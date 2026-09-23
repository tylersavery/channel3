package input

import (
	"reflect"
	"testing"
)

// TestDecodeTTY is the developer keyboard map, asserted as a table so the keys
// in the README and the keys in the code cannot drift apart.
func TestDecodeTTY(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Key
	}{
		{name: "plus is channel up", input: "+", want: []Key{{Action: ChannelUp}}},
		{name: "minus is channel down", input: "-", want: []Key{{Action: ChannelDown}}},
		{name: "p is power", input: "p", want: []Key{{Action: Power}}},
		{name: "bracket left is volume down", input: "[", want: []Key{{Action: VolumeDown}}},
		{name: "bracket right is volume up", input: "]", want: []Key{{Action: VolumeUp}}},
		{
			name:  "digits",
			input: "0123456789",
			want: []Key{
				{Action: Digit, Digit: 0}, {Action: Digit, Digit: 1}, {Action: Digit, Digit: 2},
				{Action: Digit, Digit: 3}, {Action: Digit, Digit: 4}, {Action: Digit, Digit: 5},
				{Action: Digit, Digit: 6}, {Action: Digit, Digit: 7}, {Action: Digit, Digit: 8},
				{Action: Digit, Digit: 9},
			},
		},
		{name: "up arrow is channel up", input: "\x1b[A", want: []Key{{Action: ChannelUp}}},
		{name: "down arrow is channel down", input: "\x1b[B", want: []Key{{Action: ChannelDown}}},
		{name: "right arrow means nothing", input: "\x1b[C", want: nil},
		{name: "letters mean nothing", input: "qwerty", want: nil},
		{
			name:  "a run of keys in order",
			input: "+1\x1b[B2-",
			want: []Key{
				{Action: ChannelUp},
				{Action: Digit, Digit: 1},
				{Action: ChannelDown},
				{Action: Digit, Digit: 2},
				{Action: ChannelDown},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, rest := decodeTTY([]byte(tc.input))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decoded %q to %v, want %v", tc.input, got, tc.want)
			}
			if len(rest) != 0 {
				t.Errorf("decoded %q and held back %q, want nothing", tc.input, rest)
			}
		})
	}
}

// TestDecodeTTYHoldsAPartialEscape covers an arrow key split across two reads,
// which a terminal is entitled to do.
func TestDecodeTTYHoldsAPartialEscape(t *testing.T) {
	keys, rest := decodeTTY([]byte("+\x1b["))
	if !reflect.DeepEqual(keys, []Key{{Action: ChannelUp}}) {
		t.Errorf("decoded %v, want the plus only", keys)
	}
	if string(rest) != "\x1b[" {
		t.Fatalf("held back %q, want the start of the escape sequence", rest)
	}

	keys, rest = decodeTTY(append(rest, 'A'))
	if !reflect.DeepEqual(keys, []Key{{Action: ChannelUp}}) {
		t.Errorf("decoded %v once the sequence completed, want a channel up", keys)
	}
	if len(rest) != 0 {
		t.Errorf("held back %q after a complete sequence, want nothing", rest)
	}
}

// TestDecodeTTYDropsALoneEscape covers the Escape key itself, which must not
// swallow the keypress that follows it while it waits for two bytes that are
// never coming.
func TestDecodeTTYDropsALoneEscape(t *testing.T) {
	keys, rest := decodeTTY([]byte("\x1b3"))
	if !reflect.DeepEqual(keys, []Key{{Action: Digit, Digit: 3}}) {
		t.Errorf("decoded escape then 3 as %v, want the digit", keys)
	}
	if len(rest) != 0 {
		t.Errorf("held back %q, want nothing", rest)
	}

	// A trailing Escape on its own is still worth waiting on: the rest of an
	// arrow key may be in the next read.
	if keys, rest = decodeTTY([]byte("+\x1b")); len(rest) != 1 || rest[0] != escape {
		t.Errorf("held back %q, want the escape", rest)
	}
	if !reflect.DeepEqual(keys, []Key{{Action: ChannelUp}}) {
		t.Errorf("decoded %v before the escape, want a channel up", keys)
	}

	// And an arrow key still arrives whole.
	if keys, _ = decodeTTY([]byte("\x1b[A")); !reflect.DeepEqual(keys, []Key{{Action: ChannelUp}}) {
		t.Errorf("decoded an up arrow as %v", keys)
	}
}

// TestWantsInterrupt is how a raw terminal gets out of serve, since the driver
// no longer turns Ctrl-C into a signal.
func TestWantsInterrupt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"ctrl-c", "\x03", true},
		{"ctrl-d", "\x04", true},
		{"ctrl-c after a digit", "5\x03", true},
		{"ordinary keys", "+-5", false},
		{"nothing", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wantsInterrupt([]byte(tc.input)); got != tc.want {
				t.Errorf("wantsInterrupt(%q) is %t, want %t", tc.input, got, tc.want)
			}
		})
	}
}
