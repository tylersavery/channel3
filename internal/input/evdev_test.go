package input

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
	"time"
)

// event builds one struct input_event as the kernel writes it on 64-bit Linux.
//
// The test builds the bytes by hand rather than reusing the decoder's own
// offsets, so a change to the layout fails here instead of passing quietly.
func event(seconds, micros int64, kind, code uint16, value int32) []byte {
	record := make([]byte, 24)
	binary.LittleEndian.PutUint64(record[0:], uint64(seconds))
	binary.LittleEndian.PutUint64(record[8:], uint64(micros))
	binary.LittleEndian.PutUint16(record[16:], kind)
	binary.LittleEndian.PutUint16(record[18:], code)
	binary.LittleEndian.PutUint32(record[20:], uint32(value))
	return record
}

// press is one key going down, which is the only event that means anything.
func keyPress(code uint16) []byte   { return event(1758600000, 123456, evKey, code, 1) }
func keyRelease(code uint16) []byte { return event(1758600000, 223456, evKey, code, 0) }
func keyRepeat(code uint16) []byte  { return event(1758600000, 323456, evKey, code, 2) }

// syn is the EV_SYN report every device sends after a key, which must be
// ignored rather than decoded as a key of its own.
func syn() []byte { return event(1758600000, 123457, 0x00, 0, 0) }

// TestEventLayoutIs24Bytes pins the struct this decoder assumes. If the target
// ever moves to a 32-bit image, this is the test that says so.
func TestEventLayoutIs24Bytes(t *testing.T) {
	record := keyPress(KeyChannelUp)
	if len(record) != eventSize {
		t.Fatalf("one event is %d bytes, want %d", len(record), eventSize)
	}
	if got := binary.LittleEndian.Uint16(record[offsetType:]); got != evKey {
		t.Errorf("type at offset %d is %d, want EV_KEY", offsetType, got)
	}
	if got := binary.LittleEndian.Uint16(record[offsetCode:]); got != KeyChannelUp {
		t.Errorf("code at offset %d is %d, want KEY_CHANNELUP", offsetCode, got)
	}
	if got := int32(binary.LittleEndian.Uint32(record[offsetValue:])); got != 1 {
		t.Errorf("value at offset %d is %d, want a press", offsetValue, got)
	}
}

// TestDecodePressReleaseAndRepeat is the real traffic from one button: the
// press, the release, and the autorepeats a held remote button produces. Only
// the press changes a channel.
func TestDecodePressReleaseAndRepeat(t *testing.T) {
	var d decoder
	stream := bytes.Join([][]byte{
		keyPress(KeyChannelUp),
		syn(),
		keyRepeat(KeyChannelUp),
		keyRepeat(KeyChannelUp),
		keyRelease(KeyChannelUp),
		syn(),
	}, nil)

	got := d.decode(stream)
	want := []Key{{Action: ChannelUp}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded %v, want exactly one channel up", got)
	}
}

// TestDecodeBuffersAPartialRecord covers a read that stops in the middle of a
// struct, which a character device is entitled to do.
func TestDecodeBuffersAPartialRecord(t *testing.T) {
	var d decoder
	record := keyPress(Key5)

	if got := d.decode(record[:10]); len(got) != 0 {
		t.Fatalf("decoded %v from half a record, want nothing yet", got)
	}
	got := d.decode(record[10:])
	want := []Key{{Action: Digit, Digit: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded %v once the record completed, want %v", got, want)
	}
}

// TestDecodeSplitsAcrossManyChunks is the same rule one byte at a time, which
// is the worst a device could do to us.
func TestDecodeSplitsAcrossManyChunks(t *testing.T) {
	var d decoder
	stream := append(keyPress(Key1), keyPress(Key2)...)

	var got []Key
	for _, b := range stream {
		got = append(got, d.decode([]byte{b})...)
	}
	want := []Key{{Action: Digit, Digit: 1}, {Action: Digit, Digit: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded %v byte by byte, want %v", got, want)
	}
}

// TestDecodeDropsUnknownCodes keeps a keyboard's other 100 keys out of the
// broadcast.
func TestDecodeDropsUnknownCodes(t *testing.T) {
	var d decoder
	stream := bytes.Join([][]byte{
		keyPress(KeyEsc),
		keyPress(30), // KEY_A
		keyPress(57), // KEY_SPACE
	}, nil)

	if got := d.decode(stream); len(got) != 0 {
		t.Errorf("decoded %v from unmapped keys, want nothing", got)
	}
}

// TestDecodeEveryMappedCode is the table the Flirc is taught against, asserted
// as a whole so a typo in one code is caught.
func TestDecodeEveryMappedCode(t *testing.T) {
	tests := []struct {
		name string
		code uint16
		want Key
	}{
		{"KEY_CHANNELUP", KeyChannelUp, Key{Action: ChannelUp}},
		{"KEY_UP", KeyUp, Key{Action: ChannelUp}},
		{"KEY_PAGEUP", KeyPageUp, Key{Action: ChannelUp}},
		{"KEY_CHANNELDOWN", KeyChannelDown, Key{Action: ChannelDown}},
		{"KEY_DOWN", KeyDown, Key{Action: ChannelDown}},
		{"KEY_PAGEDOWN", KeyPageDown, Key{Action: ChannelDown}},
		{"KEY_0", Key0, Key{Action: Digit, Digit: 0}},
		{"KEY_1", Key1, Key{Action: Digit, Digit: 1}},
		{"KEY_2", Key2, Key{Action: Digit, Digit: 2}},
		{"KEY_3", Key3, Key{Action: Digit, Digit: 3}},
		{"KEY_4", Key4, Key{Action: Digit, Digit: 4}},
		{"KEY_5", Key5, Key{Action: Digit, Digit: 5}},
		{"KEY_6", Key6, Key{Action: Digit, Digit: 6}},
		{"KEY_7", Key7, Key{Action: Digit, Digit: 7}},
		{"KEY_8", Key8, Key{Action: Digit, Digit: 8}},
		{"KEY_9", Key9, Key{Action: Digit, Digit: 9}},
		{"KEY_KP0", KeyKP0, Key{Action: Digit, Digit: 0}},
		{"KEY_KP1", KeyKP1, Key{Action: Digit, Digit: 1}},
		{"KEY_KP2", KeyKP2, Key{Action: Digit, Digit: 2}},
		{"KEY_KP3", KeyKP3, Key{Action: Digit, Digit: 3}},
		{"KEY_KP4", KeyKP4, Key{Action: Digit, Digit: 4}},
		{"KEY_KP5", KeyKP5, Key{Action: Digit, Digit: 5}},
		{"KEY_KP6", KeyKP6, Key{Action: Digit, Digit: 6}},
		{"KEY_KP7", KeyKP7, Key{Action: Digit, Digit: 7}},
		{"KEY_KP8", KeyKP8, Key{Action: Digit, Digit: 8}},
		{"KEY_KP9", KeyKP9, Key{Action: Digit, Digit: 9}},
		{"KEY_POWER", KeyPower, Key{Action: Power}},
		{"KEY_VOLUMEUP", KeyVolumeUp, Key{Action: VolumeUp}},
		{"KEY_VOLUMEDOWN", KeyVolumeDown, Key{Action: VolumeDown}},
		{"KEY_MUTE", KeyMute, Key{Action: Mute}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d decoder
			got := d.decode(keyPress(tc.code))
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("%s decoded to %v, want %v", tc.name, got, tc.want)
			}
		})
	}

	if len(keyActions) != len(tests) {
		t.Errorf("the code map has %d entries and this table has %d, so one of them is out of date",
			len(keyActions), len(tests))
	}
}

// TestDecodeIgnoresNonKeyEvents covers EV_REL and EV_ABS traffic from a device
// that is a mouse as well as a keyboard, which some remote receivers are.
func TestDecodeIgnoresNonKeyEvents(t *testing.T) {
	var d decoder
	stream := bytes.Join([][]byte{
		event(1, 0, 0x02, KeyChannelUp, 1), // EV_REL carrying the same code
		event(1, 0, 0x04, KeyChannelUp, 1), // EV_MSC scancode
		keyPress(KeyChannelUp),
	}, nil)

	if got := d.decode(stream); len(got) != 1 || got[0].Action != ChannelUp {
		t.Errorf("decoded %v, want only the EV_KEY press", got)
	}
}

// TestDecoderBufferDoesNotGrow guards the leftover copy: a stream that never
// completes a record must not let the buffer climb forever.
func TestDecoderBufferDoesNotGrow(t *testing.T) {
	var d decoder
	for range 1000 {
		d.decode(keyPress(Key1))
	}
	if len(d.buf) != 0 {
		t.Errorf("the decoder kept %d bytes after whole records, want none", len(d.buf))
	}
}

// TestNextReopenBackoff pins the wait between attempts to reopen a device that
// has gone away, which is what stops an unplugged receiver from filling the
// journal while still picking it up quickly when it comes back.
func TestNextReopenBackoff(t *testing.T) {
	if reopenInitialBackoff != 250*time.Millisecond {
		t.Errorf("the first wait is %s, want 250ms", reopenInitialBackoff)
	}
	if reopenMaxBackoff != 5*time.Second {
		t.Errorf("the cap is %s, want 5s", reopenMaxBackoff)
	}

	want := []time.Duration{
		500 * time.Millisecond,
		time.Second,
		2 * time.Second,
		4 * time.Second,
		5 * time.Second,
		5 * time.Second,
	}
	got := reopenInitialBackoff
	for i, w := range want {
		got = nextReopenBackoff(got)
		if got != w {
			t.Fatalf("wait %d is %s, want %s", i+1, got, w)
		}
	}
}
