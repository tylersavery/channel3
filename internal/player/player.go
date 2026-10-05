package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// EventKind says what happened to the player.
type EventKind int

const (
	// EndFile means mpv finished a file. Only the reasons the station acts on
	// are surfaced: eof for a file that played out, error for one mpv could not
	// play.
	EndFile EventKind = iota
	// Restarted means mpv died and has been replaced by a fresh process, which
	// is idle and showing nothing.
	Restarted
)

// String names the kind for logs.
func (k EventKind) String() string {
	switch k {
	case EndFile:
		return "end-file"
	case Restarted:
		return "restarted"
	default:
		return fmt.Sprintf("kind(%d)", int(k))
	}
}

// mpv's end-file reasons. Only the first two reach the station.
const (
	// ReasonEOF is a file that played to its end.
	ReasonEOF = "eof"
	// ReasonError is a file mpv could not play.
	ReasonError = "error"
	// reasonStop is the file our own loadfile replaced.
	reasonStop = "stop"
	// reasonRedirect is a playlist entry mpv resolved into another entry.
	reasonRedirect = "redirect"
	// reasonQuit is mpv shutting down.
	reasonQuit = "quit"
)

// Event is something the player needs the station to know about.
type Event struct {
	Kind   EventKind
	Reason string // mpv's end-file reason, for EndFile
	Path   string // the file that ended, as it was handed to Load
}

// Player is what the rest of the service does with mpv.
//
// The interface is deliberately small: broadcast mode has no pause, no seek and
// no playlist. Phase 6 drives Load on a channel change and Phase 7 reads the
// station, not the player, for what is on.
type Player interface {
	// Load plays path from offset, replacing whatever is playing.
	Load(path string, offset time.Duration) error
	// Standby shows the Please Stand By card until the next Load.
	Standby() error
	// ShowText draws text over the picture for d, replacing any text already
	// up. It survives a Load, so it can be sent at the moment of tuning.
	ShowText(text string, d time.Duration) error
	// Position reports the file mpv is playing and how far into it. An idle
	// mpv reports an empty path and a zero offset without an error.
	Position() (string, time.Duration, error)
	// Events is the stream of end-file and restart events.
	Events() <-chan Event
}

// Timings are the supervisor's deadlines and restart policy.
//
// A zero field takes its default, so callers set only what they mean to change.
// Tests shorten them; the service uses the defaults.
type Timings struct {
	Command        time.Duration // deadline for one mpv command
	Dial           time.Duration // how long to wait for mpv to create its socket
	HealthInterval time.Duration // how often to check that mpv still answers
	HealthTimeout  time.Duration // how long an answer to the health check may take
	InitialBackoff time.Duration // wait before the first restart attempt
	MaxBackoff     time.Duration // cap on the wait between restart attempts
	HealthyPeriod  time.Duration // how long a session must last to reset the backoff
	Quit           time.Duration // how long a clean quit may take before mpv is killed
}

// DefaultTimings are the values the service runs with.
func DefaultTimings() Timings {
	return Timings{
		Command:        5 * time.Second,
		Dial:           5 * time.Second,
		HealthInterval: 10 * time.Second,
		HealthTimeout:  2 * time.Second,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
		HealthyPeriod:  60 * time.Second,
		Quit:           3 * time.Second,
	}
}

// withDefaults fills every zero field from DefaultTimings.
func (t Timings) withDefaults() Timings {
	d := DefaultTimings()
	if t.Command <= 0 {
		t.Command = d.Command
	}
	if t.Dial <= 0 {
		t.Dial = d.Dial
	}
	if t.HealthInterval <= 0 {
		t.HealthInterval = d.HealthInterval
	}
	if t.HealthTimeout <= 0 {
		t.HealthTimeout = d.HealthTimeout
	}
	if t.InitialBackoff <= 0 {
		t.InitialBackoff = d.InitialBackoff
	}
	if t.MaxBackoff <= 0 {
		t.MaxBackoff = d.MaxBackoff
	}
	if t.HealthyPeriod <= 0 {
		t.HealthyPeriod = d.HealthyPeriod
	}
	if t.Quit <= 0 {
		t.Quit = d.Quit
	}
	return t
}

// Options configure a Supervisor.
type Options struct {
	// Launcher starts mpv. Required.
	Launcher Launcher
	// Socket is the IPC socket path mpv listens on. Required.
	Socket string
	// StandbyPath is the Please Stand By card on disk. Required.
	StandbyPath string
	// Logger receives the supervisor's own logging. Defaults to a discarding
	// logger so a library user is never surprised by output.
	Logger *slog.Logger
	// Timings override the defaults, field by field.
	Timings Timings
}

// errNotConnected is returned by a command issued while mpv is being restarted.
var errNotConnected = errors.New("player: mpv is not connected")

// eventBufferOut is how many events may queue for the station. One per file
// boundary is the normal rate, so this only absorbs a burst during a restart.
const eventBufferOut = 16

// Supervisor runs mpv and keeps it running.
//
// It owns the process and the socket. Every method above is safe to call from
// any goroutine, and all of them tolerate mpv being mid-restart: they fail with
// an error rather than blocking, because the station recovers on the next event
// or the next reconcile tick.
type Supervisor struct {
	launcher Launcher
	socket   string
	standby  string
	log      *slog.Logger
	timings  Timings

	events chan Event
	stop   chan struct{}
	done   chan struct{}
	cancel context.CancelFunc

	closeOnce sync.Once

	mu      sync.Mutex
	conn    *ipcConn
	entries map[int64]string // mpv playlist entry id to the path we loaded
}

// session is one mpv process and the connection to it.
type session struct {
	proc   Process
	conn   *ipcConn
	exited chan struct{}
}

// Start launches mpv, connects to it and begins supervising.
//
// It returns once the first process is up and has been checked, so a caller
// that gets no error has a usable player. An mpv older than 0.38 is refused
// here and never restarted.
func Start(ctx context.Context, opts Options) (*Supervisor, error) {
	if opts.Launcher == nil {
		return nil, errors.New("player: a launcher is required")
	}
	if opts.Socket == "" {
		return nil, errors.New("player: a socket path is required")
	}
	if opts.StandbyPath == "" {
		return nil, errors.New("player: a standby card path is required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	runCtx, cancel := context.WithCancel(ctx)
	s := &Supervisor{
		launcher: opts.Launcher,
		socket:   opts.Socket,
		standby:  opts.StandbyPath,
		log:      log,
		timings:  opts.Timings.withDefaults(),
		events:   make(chan Event, eventBufferOut),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		cancel:   cancel,
		entries:  make(map[int64]string),
	}

	first, err := s.launch(runCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	go s.supervise(runCtx, first)
	return s, nil
}

// Events returns the stream the station reads. It is closed when the supervisor
// stops for good.
func (s *Supervisor) Events() <-chan Event { return s.events }

// Load plays path from offset, replacing whatever is on.
//
// The command is mpv's four argument loadfile: the index must be present before
// the options string, which is why this service requires mpv 0.38 or newer.
func (s *Supervisor) Load(path string, offset time.Duration) error {
	if offset < 0 {
		offset = 0
	}
	start := fmt.Sprintf("start=%.3f", offset.Seconds())
	return s.loadFile(path, "loadfile", path, "replace", -1, start)
}

// Standby shows the Please Stand By card.
//
// The card is an image and mpv runs with image-display-duration set to
// infinity, so it stays up until the next Load rather than ending and leaving a
// black screen.
func (s *Supervisor) Standby() error {
	return s.loadFile(s.standby, "loadfile", s.standby, "replace")
}

// ShowText draws text over the picture for d.
//
// mpv runs with osd-level=0 so that none of its own messages ever appear. The
// last argument to show-text is the level a message needs, and 0 is what lets
// this one through.
func (s *Supervisor) ShowText(text string, d time.Duration) error {
	conn, err := s.currentConn()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timings.Command)
	defer cancel()
	_, err = conn.Command(ctx, "show-text", text, d.Milliseconds(), 0)
	return err
}

// loadFile sends a loadfile command and records which path the resulting
// playlist entry holds, so an end-file event can name the file that ended.
func (s *Supervisor) loadFile(path string, args ...any) error {
	conn, err := s.currentConn()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timings.Command)
	defer cancel()

	data, err := conn.Command(ctx, args...)
	if err != nil {
		return err
	}
	var reply struct {
		EntryID int64 `json:"playlist_entry_id"`
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		// The file is loading regardless; only the event's Path is lost, and
		// the station falls back to the item it last asked for.
		s.log.Warn("could not read the playlist entry id from mpv", "path", path, "error", err)
		return nil
	}
	s.recordEntry(reply.EntryID, path)
	return nil
}

// recordEntry remembers the path behind a playlist entry id.
//
// Only the most recent entries are kept. mpv emits end-file for an entry within
// moments of the next one starting, so anything older than the previous entry
// is never asked about again and would otherwise grow without bound.
func (s *Supervisor) recordEntry(id int64, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = path
	for known := range s.entries {
		if known < id-1 {
			delete(s.entries, known)
		}
	}
}

// takeEntry returns and forgets the path behind a playlist entry id.
func (s *Supervisor) takeEntry(id int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.entries[id]
	delete(s.entries, id)
	return path
}

// Position reports what mpv is playing and how far into it.
//
// An idle mpv, which is what a fresh process is before the first load, reports
// an empty path and a zero offset with no error. The station reads that as "not
// playing what I expect" and reloads, which is the right answer.
func (s *Supervisor) Position() (string, time.Duration, error) {
	conn, err := s.currentConn()
	if err != nil {
		return "", 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timings.Command)
	defer cancel()

	var path string
	data, err := conn.Command(ctx, "get_property", "path")
	if err != nil {
		if isUnavailable(err) {
			return "", 0, nil
		}
		return "", 0, err
	}
	if err := json.Unmarshal(data, &path); err != nil {
		return "", 0, fmt.Errorf("player: read the path property: %w", err)
	}

	var seconds float64
	data, err = conn.Command(ctx, "get_property", "time-pos")
	if err != nil {
		if isUnavailable(err) {
			return path, 0, nil
		}
		return "", 0, err
	}
	if err := json.Unmarshal(data, &seconds); err != nil {
		return "", 0, fmt.Errorf("player: read the time-pos property: %w", err)
	}
	return path, time.Duration(seconds * float64(time.Second)), nil
}

// isUnavailable reports whether an mpv error means "there is nothing playing"
// rather than a real failure.
//
// mpv answers a property that has no value right now with this text rather than
// with a null, so matching on it is the only way to tell an idle player from a
// broken one.
func isUnavailable(err error) bool {
	return strings.Contains(err.Error(), "property unavailable")
}

// Close quits mpv cleanly and stops supervising.
//
// It is safe to call more than once. After it returns, no further events are
// delivered and mpv is gone.
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.done
		s.cancel()
	})
	return nil
}

// currentConn returns the live connection, or an error while mpv is restarting.
func (s *Supervisor) currentConn() (*ipcConn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil, errNotConnected
	}
	return s.conn, nil
}

// setConn publishes the connection the public methods use.
func (s *Supervisor) setConn(conn *ipcConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn
	if conn == nil {
		// Entry ids restart with the process, so stale paths would name the
		// wrong file on the next end-file event.
		s.entries = make(map[int64]string)
	}
}

// launch starts one mpv, connects to it and checks its version.
func (s *Supervisor) launch(ctx context.Context) (*session, error) {
	proc, err := s.launcher.Launch(ctx, s.socket)
	if err != nil {
		return nil, err
	}
	sess := &session{proc: proc, exited: make(chan struct{})}
	go func() {
		if err := proc.Wait(); err != nil {
			s.log.Warn("mpv exited with an error", "error", err)
		}
		close(sess.exited)
	}()

	conn, err := s.dial(ctx, sess)
	if err != nil {
		s.killSession(sess)
		return nil, err
	}
	sess.conn = conn

	if err := s.checkVersion(conn); err != nil {
		s.killSession(sess)
		return nil, err
	}

	s.setConn(conn)
	return sess, nil
}

// dial connects to the socket, retrying until mpv has created it.
func (s *Supervisor) dial(ctx context.Context, sess *session) (*ipcConn, error) {
	deadline := time.Now().Add(s.timings.Dial)
	var lastErr error
	for {
		conn, err := dialIPC(s.socket, s.log)
		if err == nil {
			return conn, nil
		}
		lastErr = err

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("player: mpv did not create %s within %s: %w", s.socket, s.timings.Dial, lastErr)
		}
		select {
		case <-sess.exited:
			return nil, fmt.Errorf("player: mpv exited before it created %s: %w", s.socket, lastErr)
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// checkVersion refuses an mpv too old for the loadfile form this service sends.
func (s *Supervisor) checkVersion(conn *ipcConn) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timings.Command)
	defer cancel()

	data, err := conn.Command(ctx, "get_property", "mpv-version")
	if err != nil {
		return fmt.Errorf("player: read the mpv version: %w", err)
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("player: read the mpv version: %w", err)
	}
	got, err := parseVersion(raw)
	if err != nil {
		return err
	}
	if got.olderThan(minimumVersion) {
		return fmt.Errorf("player: mpv %s is too old, this service needs %s or newer because it sends the loadfile form introduced there",
			got, minimumVersion)
	}
	s.log.Info("connected to mpv", "version", raw, "socket", s.socket)
	return nil
}

// supervise keeps one mpv alive until Close or a cancelled context.
func (s *Supervisor) supervise(ctx context.Context, sess *session) {
	defer close(s.done)
	defer close(s.events)

	backoff := s.timings.InitialBackoff
	for {
		started := time.Now()
		stopping := s.runSession(ctx, sess)
		s.setConn(nil)

		if stopping {
			s.shutdownSession(sess)
			return
		}
		s.killSession(sess)

		backoff = backoffAfterSession(backoff, time.Since(started), s.timings)
		s.log.Warn("mpv stopped, restarting", "after", time.Since(started).Round(time.Millisecond), "wait", backoff)

		next, ok := s.relaunch(ctx, &backoff)
		if !ok {
			return
		}
		sess = next
		s.emit(Event{Kind: Restarted})
	}
}

// relaunch waits out the backoff and starts mpv again, retrying until it works
// or the supervisor is stopped. It reports false when it was stopped.
func (s *Supervisor) relaunch(ctx context.Context, backoff *time.Duration) (*session, bool) {
	for {
		wait := *backoff
		*backoff = nextBackoff(*backoff, s.timings.MaxBackoff)

		select {
		case <-time.After(wait):
		case <-s.stop:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}

		sess, err := s.launch(ctx)
		if err == nil {
			return sess, true
		}
		// A launch failure that repeats is either a missing binary or an mpv
		// too old. Logging every attempt is what makes that visible on a Pi
		// with no screen to read.
		s.log.Error("could not start mpv", "error", err, "next attempt in", *backoff)
	}
}

// backoffAfterSession returns the wait to carry into the next restart.
//
// A session that lasted the healthy period was a working mpv rather than one
// link in a restart loop, so the escalation it inherited is dropped and the
// next failure waits the initial backoff again.
func backoffAfterSession(current, lifetime time.Duration, t Timings) time.Duration {
	if lifetime >= t.HealthyPeriod {
		return t.InitialBackoff
	}
	return current
}

// nextBackoff doubles the wait up to the cap.
func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

// runSession pumps one session's events and health checks.
//
// It returns true when the supervisor is stopping for good and false when the
// session failed and should be replaced.
func (s *Supervisor) runSession(ctx context.Context, sess *session) bool {
	health := time.NewTicker(s.timings.HealthInterval)
	defer health.Stop()

	for {
		select {
		case <-ctx.Done():
			return true
		case <-s.stop:
			return true
		case <-sess.exited:
			s.log.Warn("mpv exited")
			return false
		case <-sess.conn.Done():
			s.log.Warn("the mpv ipc socket closed")
			return false
		case ev, ok := <-sess.conn.Events():
			if !ok {
				return false
			}
			s.handleEvent(ev)
		case <-health.C:
			if err := s.checkHealth(sess.conn); err != nil {
				s.log.Warn("mpv failed its health check", "error", err)
				return false
			}
		}
	}
}

// checkHealth asks mpv a question it can always answer.
//
// A process that does not answer within the health timeout is hung rather than
// busy: idle-active is read from the core's state and never waits on decoding.
func (s *Supervisor) checkHealth(conn *ipcConn) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timings.HealthTimeout)
	defer cancel()
	_, err := conn.Command(ctx, "get_property", "idle-active")
	return err
}

// handleEvent turns an mpv event into a station event, or swallows it.
func (s *Supervisor) handleEvent(ev mpvEvent) {
	if ev.Name != "end-file" {
		return
	}
	path := s.takeEntry(ev.EntryID)
	switch ev.Reason {
	case ReasonEOF, ReasonError:
		if ev.Reason == ReasonError {
			s.log.Warn("mpv could not play a file", "path", path, "file_error", ev.FileError)
		}
		s.emit(Event{Kind: EndFile, Reason: ev.Reason, Path: path})
	case reasonStop, reasonRedirect, reasonQuit:
		// stop is mpv reporting the file our own loadfile replaced, and
		// redirect is one playlist entry resolving into another. Passing
		// either on would make the station ask the schedule again for a load
		// it just issued, and it would do so forever.
		s.log.Debug("swallowed an end-file event", "reason", ev.Reason, "path", path)
	default:
		s.log.Warn("ignoring an unknown end-file reason", "reason", ev.Reason, "path", path)
	}
}

// emit delivers an event to the station, unless the supervisor is stopping.
func (s *Supervisor) emit(ev Event) {
	select {
	case s.events <- ev:
	case <-s.stop:
	}
}

// shutdownSession asks mpv to quit and kills it if it will not.
func (s *Supervisor) shutdownSession(sess *session) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timings.Quit)
	defer cancel()
	if _, err := sess.conn.Command(ctx, "quit"); err != nil && !errors.Is(err, errConnClosed) {
		s.log.Warn("mpv did not accept the quit command", "error", err)
	}

	select {
	case <-sess.exited:
	case <-time.After(s.timings.Quit):
		s.log.Warn("mpv did not exit after quit, killing it")
	}
	s.killSession(sess)
}

// killSession tears a session down whatever state it is in.
func (s *Supervisor) killSession(sess *session) {
	if sess.conn != nil {
		if err := sess.conn.Close(); err != nil {
			s.log.Debug("closing the mpv socket", "error", err)
		}
	}
	if err := sess.proc.Kill(); err != nil {
		s.log.Warn("could not kill mpv", "error", err)
	}
	<-sess.exited
}
