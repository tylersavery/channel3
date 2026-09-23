// Package player runs mpv and keeps it running.
//
// The service never renders video itself. It starts one mpv process, talks to
// it over mpv's JSON IPC socket, and restarts it when it dies. Everything above
// this package sees the small Player interface: load a file at an offset, show
// the Please Stand By card, ask where playback is, and receive the handful of
// events that matter.
//
// Nothing here is persisted and nothing here opens a network connection. The
// only file this package writes is the Stand By card, extracted from the binary
// into a runtime directory because mpv needs a path to load.
package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
)

// eventBuffer is how many mpv events the reader goroutine may queue.
//
// mpv is chatty: a single file load produces a start-file, a file-loaded, a
// seek and several reconfig events. The buffer is deep enough that a consumer
// busy handling one end-file never makes the reader block, because a blocked
// reader would also stop routing command replies and deadlock the caller that
// is waiting for one. Events are dropped rather than blocking if it ever fills.
const eventBuffer = 256

// mpvError is the text mpv sends in a reply's "error" field when a command
// succeeded. Anything else is a failure.
const mpvSuccess = "success"

// errConnClosed is returned to callers waiting on a reply when the connection
// dies underneath them.
var errConnClosed = errors.New("player: mpv ipc connection closed")

// mpvEvent is one asynchronous event from mpv.
//
// Only the fields this service acts on are decoded. Everything else mpv emits
// is carried in Name alone and ignored by the supervisor.
type mpvEvent struct {
	Name      string `json:"event"`
	Reason    string `json:"reason"`
	EntryID   int64  `json:"playlist_entry_id"`
	FileError string `json:"file_error"`
}

// mpvReply is one response to a command, matched to its caller by RequestID.
type mpvReply struct {
	RequestID int64           `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
}

// ipcConn is a client for one mpv JSON IPC socket.
//
// The protocol is newline-delimited JSON in both directions on a single Unix
// socket. Commands carry a monotonically increasing request_id and mpv echoes
// it back, so several commands may be in flight at once. A single reader
// goroutine owns the socket for reading: it routes replies to the callers
// waiting on them and pushes events onto a channel.
type ipcConn struct {
	conn   net.Conn
	log    *slog.Logger
	events chan mpvEvent

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan mpvReply
	closed  bool

	done    chan struct{} // closed when the reader goroutine stops
	readErr error         // why the reader stopped; guarded by mu
}

// dialIPC connects to an mpv IPC socket and starts the reader goroutine.
//
// mpv creates the socket a moment after it starts, so callers retry; this
// function makes exactly one attempt.
func dialIPC(socket string, log *slog.Logger) (*ipcConn, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("player: dial mpv socket %s: %w", socket, err)
	}
	ipc := &ipcConn{
		conn:    c,
		log:     log,
		events:  make(chan mpvEvent, eventBuffer),
		pending: make(map[int64]chan mpvReply),
		done:    make(chan struct{}),
	}
	go ipc.read()
	return ipc, nil
}

// Events returns the stream of mpv events. It is closed when the connection is.
func (c *ipcConn) Events() <-chan mpvEvent { return c.events }

// Done is closed when the reader goroutine has stopped, which is how the
// supervisor learns that mpv went away.
func (c *ipcConn) Done() <-chan struct{} { return c.done }

// Command sends one mpv command and waits for its reply.
//
// The arguments are the mpv command array, for example "loadfile", path,
// "replace". An mpv error reply becomes a Go error carrying mpv's own text. The
// context bounds the wait, so a hung mpv fails the caller instead of blocking
// the service.
func (c *ipcConn) Command(ctx context.Context, args ...any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	replies := make(chan mpvReply, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errConnClosed
	}
	c.pending[id] = replies
	c.mu.Unlock()
	defer c.forget(id)

	payload, err := json.Marshal(map[string]any{"command": args, "request_id": id})
	if err != nil {
		return nil, fmt.Errorf("player: encode command %v: %w", args, err)
	}
	if _, err := c.conn.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("player: write command %v: %w", args, err)
	}

	select {
	case reply, ok := <-replies:
		if !ok {
			// The reader closed this channel on its way out rather than
			// answering. A zero value reply here would read as an mpv failure
			// with an empty message, which says nothing about what happened.
			return nil, errConnClosed
		}
		if reply.Error != mpvSuccess {
			return nil, fmt.Errorf("player: mpv rejected %v: %s", args, reply.Error)
		}
		return reply.Data, nil
	case <-c.done:
		return nil, errConnClosed
	case <-ctx.Done():
		return nil, fmt.Errorf("player: command %v: %w", args, ctx.Err())
	}
}

// forget drops a pending request, whether it was answered, timed out or lost.
func (c *ipcConn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// read owns the socket for reading until it fails or is closed.
func (c *ipcConn) read() {
	defer close(c.done)
	defer close(c.events)

	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		c.route(scanner.Bytes())
	}
	err := scanner.Err()
	if err == nil {
		err = errConnClosed
	}

	c.mu.Lock()
	c.closed = true
	c.readErr = err
	pending := c.pending
	c.pending = make(map[int64]chan mpvReply)
	c.mu.Unlock()

	// Waiting callers are woken by done as well, but clearing the map here
	// makes the failure explicit rather than leaving goroutines to time out.
	for _, ch := range pending {
		close(ch)
	}
}

// route decides whether a line is an event or a reply and delivers it.
func (c *ipcConn) route(line []byte) {
	// A line with an "event" field is an event; everything else is a reply to
	// a command. mpv never mixes the two in one message.
	var probe struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		c.log.Warn("ignoring unparsable mpv ipc line", "line", string(line), "error", err)
		return
	}

	if probe.Event != "" {
		var ev mpvEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			c.log.Warn("ignoring unparsable mpv event", "line", string(line), "error", err)
			return
		}
		select {
		case c.events <- ev:
		default:
			// Dropping is the lesser evil: blocking here would stop replies
			// being routed and hang whoever is waiting for one. The station's
			// periodic reconcile recovers from a dropped end-file.
			c.log.Warn("dropped an mpv event, consumer is behind", "event", ev.Name)
		}
		return
	}

	var reply mpvReply
	if err := json.Unmarshal(line, &reply); err != nil {
		c.log.Warn("ignoring unparsable mpv reply", "line", string(line), "error", err)
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[reply.RequestID]
	if ok {
		delete(c.pending, reply.RequestID)
	}
	c.mu.Unlock()
	if !ok {
		// A reply whose caller already gave up, or mpv's unsolicited reply to
		// request_id 0. Neither is an error worth failing on.
		c.log.Debug("ignoring an unmatched mpv reply", "request_id", reply.RequestID)
		return
	}
	ch <- reply
}

// Close shuts the socket down. The reader goroutine stops as a result.
func (c *ipcConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return c.conn.Close()
}
