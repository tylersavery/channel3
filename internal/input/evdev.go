package input

import (
	"encoding/binary"
	"log/slog"
	"time"
)

// The Linux input event codes this service cares about, from
// include/uapi/linux/input-event-codes.h. They are copied rather than read from
// a cgo header because the binary is pure Go and cross compiled from a Mac.
const (
	evKey = 0x01 // EV_KEY, a button went up or down

	// valuePress is the value of a press. A release is 0 and an autorepeat is
	// 2, and neither should change a channel: holding channel up on a remote
	// would otherwise run through every channel on the box.
	valuePress = 1
)

// Key codes as the kernel reports them.
const (
	KeyEsc         = 1
	Key1           = 2
	Key2           = 3
	Key3           = 4
	Key4           = 5
	Key5           = 6
	Key6           = 7
	Key7           = 8
	Key8           = 9
	Key9           = 10
	Key0           = 11
	KeyKP7         = 71
	KeyKP8         = 72
	KeyKP9         = 73
	KeyKP4         = 75
	KeyKP5         = 76
	KeyKP6         = 77
	KeyKP1         = 79
	KeyKP2         = 80
	KeyKP3         = 81
	KeyKP0         = 82
	KeyUp          = 103
	KeyPageUp      = 104
	KeyDown        = 108
	KeyPageDown    = 109
	KeyMute        = 113
	KeyVolumeDown  = 114
	KeyVolumeUp    = 115
	KeyPower       = 116
	KeyChannelUp   = 402
	KeyChannelDown = 403
)

// eventSize is the size of one struct input_event on 64-bit Linux: two 64-bit
// words of timeval, then a uint16 type, a uint16 code and an int32 value.
//
// This is the arm64 and amd64 layout. A 32-bit Pi OS would use 16 bytes, which
// is why the target in CLAUDE.md is the 64-bit image.
const eventSize = 24

// Offsets within one input_event.
const (
	offsetType  = 16
	offsetCode  = 18
	offsetValue = 20
)

// keyActions maps a kernel key code to what this service does about it.
//
// The arrow and page keys are here because a plain USB keyboard is the fallback
// remote and because a Flirc can be taught to send them. Everything not in this
// map is dropped, which is most of a keyboard.
var keyActions = map[uint16]Key{
	KeyChannelUp:   {Action: ChannelUp},
	KeyUp:          {Action: ChannelUp},
	KeyPageUp:      {Action: ChannelUp},
	KeyChannelDown: {Action: ChannelDown},
	KeyDown:        {Action: ChannelDown},
	KeyPageDown:    {Action: ChannelDown},

	Key0: {Action: Digit, Digit: 0},
	Key1: {Action: Digit, Digit: 1},
	Key2: {Action: Digit, Digit: 2},
	Key3: {Action: Digit, Digit: 3},
	Key4: {Action: Digit, Digit: 4},
	Key5: {Action: Digit, Digit: 5},
	Key6: {Action: Digit, Digit: 6},
	Key7: {Action: Digit, Digit: 7},
	Key8: {Action: Digit, Digit: 8},
	Key9: {Action: Digit, Digit: 9},

	KeyKP0: {Action: Digit, Digit: 0},
	KeyKP1: {Action: Digit, Digit: 1},
	KeyKP2: {Action: Digit, Digit: 2},
	KeyKP3: {Action: Digit, Digit: 3},
	KeyKP4: {Action: Digit, Digit: 4},
	KeyKP5: {Action: Digit, Digit: 5},
	KeyKP6: {Action: Digit, Digit: 6},
	KeyKP7: {Action: Digit, Digit: 7},
	KeyKP8: {Action: Digit, Digit: 8},
	KeyKP9: {Action: Digit, Digit: 9},

	KeyPower:      {Action: Power},
	KeyVolumeUp:   {Action: VolumeUp},
	KeyVolumeDown: {Action: VolumeDown},
	KeyMute:       {Action: Mute},
}

// decoder turns a stream of bytes from an evdev device into keys.
//
// A read from /dev/input/eventN returns whole events in practice, but nothing
// in the interface promises it, so a trailing part of a record is held back
// until the rest of it arrives. Misreading that boundary would turn a keypress
// into whatever the next few bytes happened to spell.
type decoder struct {
	buf []byte
}

// decode appends chunk to whatever was left over and returns the keys in it.
//
// Only presses of mapped codes come back. Releases, autorepeats, and the
// EV_SYN and EV_MSC events every device sends alongside a key are dropped here
// rather than further up, so the rest of the service only ever sees presses.
func (d *decoder) decode(chunk []byte) []Key {
	d.buf = append(d.buf, chunk...)

	var keys []Key
	for len(d.buf) >= eventSize {
		record := d.buf[:eventSize]
		d.buf = d.buf[eventSize:]

		kind := binary.LittleEndian.Uint16(record[offsetType:])
		code := binary.LittleEndian.Uint16(record[offsetCode:])
		value := int32(binary.LittleEndian.Uint32(record[offsetValue:]))
		if kind != evKey || value != valuePress {
			continue
		}
		if key, ok := keyActions[code]; ok {
			keys = append(keys, key)
		}
	}

	// The leftover is copied to the front rather than kept as a slice of a
	// growing buffer, so the backing array does not grow without bound over
	// the months this service runs.
	if len(d.buf) > 0 {
		rest := make([]byte, len(d.buf))
		copy(rest, d.buf)
		d.buf = rest
	} else {
		d.buf = d.buf[:0]
	}
	return keys
}

// Device reads keys from an evdev character device.
//
// On Linux it opens the device, grabs it so presses do not also reach the
// console, and reopens it with a backoff when it goes away, which is what
// unplugging the Flirc looks like. Off Linux it is a stub that says so, because
// a Mac has no /dev/input and develops against the terminal source instead.
type Device struct {
	path string
	log  *slog.Logger
}

// NewDevice returns a source reading from path.
func NewDevice(path string, log *slog.Logger) *Device {
	if log == nil {
		log = slog.Default()
	}
	return &Device{path: path, log: log}
}

// Reopen backoff for a device that has gone away. A remote receiver that is
// pulled out and pushed back in should be picked up within a second or two,
// and a device that is never coming back should not fill the journal.
const (
	reopenInitialBackoff = 250 * time.Millisecond
	reopenMaxBackoff     = 5 * time.Second
)

// nextReopenBackoff doubles the wait up to the cap.
func nextReopenBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > reopenMaxBackoff {
		return reopenMaxBackoff
	}
	return next
}
