package input

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/term"
)

// The terminal keys a developer drives the station with on a Mac.
//
// They are chosen to be reachable without a modifier and to sit nowhere near
// each other, so a slip does not change channel. The arrow keys work too, which
// is what most people reach for first.
const (
	ttyChannelUp   = '+'
	ttyChannelDown = '-'
	ttyPower       = 'p'
	ttyVolumeDown  = '['
	ttyVolumeUp    = ']'

	escape = 0x1b
	// interrupt is the Ctrl-C byte. A raw terminal does not turn it into a
	// signal, so the source raises one itself and the service shuts down the
	// way it does on the Pi.
	interrupt = 0x03
	// endOfTransmission is Ctrl-D, treated the same way.
	endOfTransmission = 0x04
)

// TTY reads keys from a terminal.
//
// It exists for development on a Mac, where there is no /dev/input and no
// remote. It puts the terminal in raw mode so a press acts at once instead of
// at the end of a line, and puts it back on the way out. Nothing about the
// broadcast depends on it: a Pi runs with no terminal at all.
type TTY struct {
	in  *os.File
	log *slog.Logger
	// interrupt is raised when Ctrl-C is typed. Tests substitute it; the
	// service lets it default to signalling this process.
	interrupt func()

	// mu guards the saved terminal state, which the read loop writes and the
	// shutdown path reads from another goroutine.
	mu       sync.Mutex
	state    *term.State
	restored bool
}

// NewTTY returns a source reading from in, which must be a terminal.
func NewTTY(in *os.File, log *slog.Logger) *TTY {
	if log == nil {
		log = slog.Default()
	}
	return &TTY{in: in, log: log, interrupt: raiseInterrupt}
}

// IsTerminal reports whether f is a terminal, which is what decides that the
// keyboard source is worth starting at all.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// raiseInterrupt asks this process to shut down the way Ctrl-C normally would.
//
// In raw mode the terminal driver no longer turns Ctrl-C into SIGINT, so
// without this a developer's only way out of serve would be to kill it from
// another window, leaving the terminal in raw mode and mpv running.
func raiseInterrupt() {
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		return
	}
	// The error is ignored deliberately: a process that cannot signal itself
	// is being torn down already.
	_ = proc.Signal(os.Interrupt)
}

// Keys reads presses until ctx is cancelled or stdin ends.
//
// The terminal is restored before the channel closes, so a Ctrl-C leaves the
// shell usable even though the read goroutine below may still be parked in the
// kernel waiting for a byte that will never come.
func (t *TTY) Keys(ctx context.Context) <-chan Key {
	out := make(chan Key)

	state, err := term.MakeRaw(int(t.in.Fd()))
	if err != nil {
		t.log.Warn("could not put the terminal in raw mode, keyboard control is off", "error", err)
		close(out)
		return out
	}
	t.mu.Lock()
	t.state = state
	t.restored = false
	t.mu.Unlock()

	// Reads are handed over to their own goroutine because a read from a
	// terminal cannot be cancelled: closing stdin on a shared terminal is
	// rude, so the goroutine is left parked and the process exits out from
	// under it.
	chunks := make(chan []byte)
	go func() {
		defer close(chunks)
		buf := make([]byte, 64)
		for {
			n, err := t.in.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case chunks <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					t.log.Warn("could not read the terminal", "error", err)
				}
				return
			}
		}
	}()

	go func() {
		defer close(out)
		defer t.Restore()

		var rest []byte
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-chunks:
				if !ok {
					return
				}
				if wantsInterrupt(chunk) {
					t.log.Info("stopping on a keyboard interrupt")
					t.interrupt()
					return
				}
				var keys []Key
				keys, rest = decodeTTY(append(rest, chunk...))
				for _, key := range keys {
					select {
					case out <- key:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return out
}

// Restore puts the terminal back the way it was found.
//
// It is safe to call more than once and from anywhere, because both the read
// loop and the shutdown path want to be sure of it: a terminal left in raw mode
// is a shell that no longer echoes what is typed into it.
func (t *TTY) Restore() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state == nil || t.restored {
		return
	}
	t.restored = true
	if err := term.Restore(int(t.in.Fd()), t.state); err != nil {
		t.log.Warn("could not restore the terminal", "error", err)
	}
}

// wantsInterrupt reports whether the bytes contain a Ctrl-C or a Ctrl-D.
func wantsInterrupt(chunk []byte) bool {
	for _, b := range chunk {
		if b == interrupt || b == endOfTransmission {
			return true
		}
	}
	return false
}

// decodeTTY maps terminal bytes to keys and returns the bytes it cannot read
// yet.
//
// The only multi byte sequence that matters is an arrow key, which arrives as
// escape, '[', then a letter, and can be split across two reads. Anything else
// unrecognised is dropped: a terminal sends far more than this service means.
func decodeTTY(buf []byte) ([]Key, []byte) {
	var keys []Key

	i := 0
	for i < len(buf) {
		b := buf[i]
		if b == escape {
			// A bare Escape followed by an ordinary key is not an arrow, so
			// the Escape is thrown away at once rather than holding the key
			// behind it until two more bytes happen to arrive.
			if len(buf)-i == 2 && buf[i+1] != '[' {
				i++
				continue
			}
			if len(buf)-i < 3 {
				// The rest of an arrow key may still be on its way.
				break
			}
			if buf[i+1] == '[' {
				switch buf[i+2] {
				case 'A':
					keys = append(keys, Key{Action: ChannelUp})
				case 'B':
					keys = append(keys, Key{Action: ChannelDown})
				}
				i += 3
				continue
			}
			i++
			continue
		}
		if key, ok := ttyKey(b); ok {
			keys = append(keys, key)
		}
		i++
	}

	rest := make([]byte, len(buf)-i)
	copy(rest, buf[i:])
	return keys, rest
}

// ttyKey maps one plain byte to a key.
func ttyKey(b byte) (Key, bool) {
	switch {
	case b >= '0' && b <= '9':
		return Key{Action: Digit, Digit: int(b - '0')}, true
	case b == ttyChannelUp:
		return Key{Action: ChannelUp}, true
	case b == ttyChannelDown:
		return Key{Action: ChannelDown}, true
	case b == ttyPower:
		return Key{Action: Power}, true
	case b == ttyVolumeUp:
		return Key{Action: VolumeUp}, true
	case b == ttyVolumeDown:
		return Key{Action: VolumeDown}, true
	default:
		return Key{}, false
	}
}
