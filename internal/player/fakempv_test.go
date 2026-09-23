package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeMPV is a stand-in for mpv's JSON IPC socket.
//
// It speaks the same newline-delimited JSON, records every command it is sent,
// answers get_property from a table the test controls, and can emit events or
// drop the connection on demand. No test in this package starts a real mpv.
type fakeMPV struct {
	t      *testing.T
	socket string
	ln     net.Listener

	mu        sync.Mutex
	commands  [][]any
	conns     []net.Conn
	props     map[string]any
	propErrs  map[string]string
	stalled   bool
	nextEntry int64
	stopped   bool
}

// shortTempDir returns a temporary directory whose path is short enough for a
// Unix socket. macOS caps a socket path at just over a hundred bytes, and the
// per-test directories testing.T hands out are long enough to overflow that.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "c3")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("removing %s: %v", dir, err)
		}
	})
	return dir
}

// startFakeMPV listens on socket and serves until the test ends.
func startFakeMPV(t *testing.T, socket string) *fakeMPV {
	t.Helper()
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen on %s: %v", socket, err)
	}
	f := &fakeMPV{
		t:      t,
		socket: socket,
		ln:     ln,
		props: map[string]any{
			"mpv-version": "mpv v0.41.0",
			"idle-active": false,
		},
		propErrs: map[string]string{},
	}
	go f.accept()
	t.Cleanup(f.Stop)
	return f
}

// accept serves every connection until the listener is closed.
func (f *fakeMPV) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns = append(f.conns, conn)
		f.mu.Unlock()
		go f.serve(conn)
	}
}

// serve answers commands on one connection.
func (f *fakeMPV) serve(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req struct {
			Command   []any `json:"command"`
			RequestID int64 `json:"request_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			f.t.Errorf("fake mpv got unparsable request %q: %v", scanner.Text(), err)
			continue
		}

		f.mu.Lock()
		f.commands = append(f.commands, req.Command)
		stalled := f.stalled
		f.mu.Unlock()
		if stalled {
			continue
		}

		data, errText := f.answer(req.Command)
		reply := map[string]any{"request_id": req.RequestID, "error": errText}
		if data != nil {
			reply["data"] = data
		}
		line, err := json.Marshal(reply)
		if err != nil {
			f.t.Errorf("fake mpv could not encode a reply: %v", err)
			continue
		}
		if _, err := conn.Write(append(line, '\n')); err != nil && !errors.Is(err, net.ErrClosed) {
			return
		}
	}
}

// answer produces the reply data and error text for one command.
func (f *fakeMPV) answer(cmd []any) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(cmd) == 0 {
		return nil, "invalid parameter"
	}
	name, _ := cmd[0].(string)
	switch name {
	case "get_property":
		if len(cmd) < 2 {
			return nil, "invalid parameter"
		}
		prop, _ := cmd[1].(string)
		if msg, ok := f.propErrs[prop]; ok {
			return nil, msg
		}
		value, ok := f.props[prop]
		if !ok {
			return nil, "property not found"
		}
		return value, mpvSuccess
	case "loadfile":
		f.nextEntry++
		return map[string]any{"playlist_entry_id": f.nextEntry}, mpvSuccess
	case "quit":
		return nil, mpvSuccess
	default:
		return nil, mpvSuccess
	}
}

// Commands returns every command the fake has been sent, in order.
func (f *fakeMPV) Commands() [][]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]any, len(f.commands))
	copy(out, f.commands)
	return out
}

// CommandsNamed returns only the commands whose first argument is name.
func (f *fakeMPV) CommandsNamed(name string) [][]any {
	var out [][]any
	for _, cmd := range f.Commands() {
		if len(cmd) > 0 {
			if got, _ := cmd[0].(string); got == name {
				out = append(out, cmd)
			}
		}
	}
	return out
}

// SetProperty sets what get_property returns for name.
func (f *fakeMPV) SetProperty(name string, value any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.propErrs, name)
	f.props[name] = value
}

// SetPropertyError makes get_property for name fail with mpv's error text.
func (f *fakeMPV) SetPropertyError(name, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.props, name)
	f.propErrs[name] = message
}

// Stall stops the fake replying to commands, so callers hit their timeout. It
// keeps recording what it was sent.
func (f *fakeMPV) Stall(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stalled = on
}

// LastEntryID is the playlist entry id the fake handed out for the most recent
// loadfile, which is what its end-file events should carry.
func (f *fakeMPV) LastEntryID() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nextEntry
}

// Emit sends one event to every connected client.
//
// It waits for a client first: a test that connects and emits straight away
// would otherwise race the accept goroutine and send the event to nobody.
func (f *fakeMPV) Emit(event map[string]any) {
	line, err := json.Marshal(event)
	if err != nil {
		f.t.Fatalf("encoding fake event %v: %v", event, err)
	}
	conns := f.waitForConns()
	for _, conn := range conns {
		if _, err := conn.Write(append(line, '\n')); err != nil && !errors.Is(err, net.ErrClosed) {
			f.t.Logf("fake mpv could not emit %v: %v", event, err)
		}
	}
}

// waitForConns returns the connected clients, waiting for the first one.
func (f *fakeMPV) waitForConns() []net.Conn {
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		conns := make([]net.Conn, len(f.conns))
		copy(conns, f.conns)
		f.mu.Unlock()
		if len(conns) > 0 || time.Now().After(deadline) {
			return conns
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// EmitEndFile sends an end-file event for the most recent loadfile.
func (f *fakeMPV) EmitEndFile(reason string) {
	f.EmitEndFileFor(f.LastEntryID(), reason)
}

// EmitEndFileFor sends an end-file event for one playlist entry. mpv reports
// the entry a load replaced under that entry's own id, not the new one.
func (f *fakeMPV) EmitEndFileFor(entry int64, reason string) {
	f.Emit(map[string]any{
		"event":             "end-file",
		"reason":            reason,
		"playlist_entry_id": entry,
	})
}

// CloseConns drops every client connection, which is what a dying mpv looks
// like from the outside.
func (f *fakeMPV) CloseConns() {
	f.mu.Lock()
	conns := f.conns
	f.conns = nil
	f.mu.Unlock()
	for _, conn := range conns {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			f.t.Logf("closing a fake connection: %v", err)
		}
	}
}

// Stop shuts the fake down and releases the socket.
func (f *fakeMPV) Stop() {
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return
	}
	f.stopped = true
	f.mu.Unlock()

	if err := f.ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		f.t.Logf("closing the fake listener: %v", err)
	}
	f.CloseConns()
	if err := os.Remove(f.socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		f.t.Logf("removing %s: %v", f.socket, err)
	}
}

// fakeLauncher hands the supervisor a fresh fake mpv on every launch.
type fakeLauncher struct {
	t *testing.T

	mu       sync.Mutex
	launches int
	current  *fakeMPV
	failNext error
	version  string
}

// Launch starts a fake mpv on socket and returns a handle to it. The context
// is accepted to match the real launcher; the fake is stopped by Kill.
func (l *fakeLauncher) Launch(ctx context.Context, socket string) (Process, error) {
	l.mu.Lock()
	if err := l.failNext; err != nil {
		l.failNext = nil
		l.mu.Unlock()
		return nil, err
	}
	l.launches++
	l.mu.Unlock()

	fake := startFakeMPV(l.t, socket)
	if l.version != "" {
		fake.SetProperty("mpv-version", l.version)
	}
	l.mu.Lock()
	l.current = fake
	l.mu.Unlock()
	return &fakeProcess{fake: fake, exited: make(chan struct{})}, nil
}

// Launches is how many times the supervisor has started mpv.
func (l *fakeLauncher) Launches() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.launches
}

// Current is the fake mpv from the most recent launch.
func (l *fakeLauncher) Current() *fakeMPV {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current
}

// fakeProcess is the handle the supervisor holds on a fake mpv.
type fakeProcess struct {
	fake   *fakeMPV
	once   sync.Once
	exited chan struct{}
}

func (p *fakeProcess) Wait() error {
	<-p.exited
	return nil
}

func (p *fakeProcess) Kill() error {
	p.once.Do(func() {
		p.fake.Stop()
		close(p.exited)
	})
	return nil
}

// socketPath returns a socket path inside dir, short enough for macOS.
func socketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(shortTempDir(t), "mpv.sock")
}

// commandStrings renders a recorded command for comparison in a failure
// message, so a mismatch prints as the JSON that was actually sent.
func commandStrings(cmd []any) string {
	line, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Sprintf("%v", cmd)
	}
	return string(line)
}
