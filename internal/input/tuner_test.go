package input

import (
	"testing"
	"time"
)

// testChannels is a station whose numbers cover every case the tuner has: a
// single digit number that is also the start of a two digit one, a single digit
// number that is not, and a two digit number.
func testChannels() []Channel {
	return []Channel{
		{ID: "kids", Number: 1},
		{ID: "trains", Number: 3},
		{ID: "clips", Number: 5},
		{ID: "docs", Number: 12},
	}
}

// start is a fixed instant. The tuner never reads a clock, so any value does.
var start = time.Date(2026, 9, 23, 19, 0, 0, 0, time.UTC)

// step is one thing that happens to the tuner.
//
// Either a key is pressed or the station's digit timer fires, after the clock
// has moved on by wait.
type step struct {
	key    Key
	expire bool
	wait   time.Duration

	// wantChanged is whether this step should retune.
	wantChanged bool
	// wantChannel is the channel the step reports when it changes.
	wantChannel string
	// wantPending is the digits held after the step.
	wantPending string
}

func press(a Action) Key { return Key{Action: a} }
func digitKey(d int) Key { return Key{Action: Digit, Digit: d} }

func TestTunerHandle(t *testing.T) {
	tests := []struct {
		name     string
		channels []Channel
		current  string
		steps    []step
	}{
		{
			name:     "channel up moves to the next number",
			channels: testChannels(),
			current:  "kids",
			steps: []step{
				{key: press(ChannelUp), wantChanged: true, wantChannel: "trains"},
				{key: press(ChannelUp), wantChanged: true, wantChannel: "clips"},
			},
		},
		{
			name:     "channel up wraps at the top",
			channels: testChannels(),
			current:  "docs",
			steps: []step{
				{key: press(ChannelUp), wantChanged: true, wantChannel: "kids"},
			},
		},
		{
			name:     "channel down wraps at the bottom",
			channels: testChannels(),
			current:  "kids",
			steps: []step{
				{key: press(ChannelDown), wantChanged: true, wantChannel: "docs"},
				{key: press(ChannelDown), wantChanged: true, wantChannel: "clips"},
			},
		},
		{
			name:     "a single channel does not change on up or down",
			channels: []Channel{{ID: "only", Number: 4}},
			current:  "only",
			steps: []step{
				{key: press(ChannelUp)},
				{key: press(ChannelDown)},
			},
		},
		{
			name:     "an unambiguous digit commits at once",
			channels: testChannels(),
			current:  "kids",
			steps: []step{
				{key: digitKey(3), wantChanged: true, wantChannel: "trains"},
			},
		},
		{
			name:     "a digit that could grow waits for the next one",
			channels: testChannels(),
			current:  "clips",
			steps: []step{
				{key: digitKey(1), wantPending: "1"},
				{key: digitKey(2), wantChanged: true, wantChannel: "docs"},
			},
		},
		{
			name:     "a digit that could grow commits on the timeout",
			channels: testChannels(),
			current:  "clips",
			steps: []step{
				{key: digitKey(1), wantPending: "1"},
				{expire: true, wait: DigitTimeout, wantChanged: true, wantChannel: "kids"},
			},
		},
		{
			name:     "a number nobody broadcasts on is discarded",
			channels: testChannels(),
			current:  "clips",
			steps: []step{
				{key: digitKey(9)},
			},
		},
		{
			name:     "a number that matches nothing on the timeout leaves the channel alone",
			channels: []Channel{{ID: "docs", Number: 12}, {ID: "news", Number: 13}},
			current:  "docs",
			steps: []step{
				// Both numbers start with 1, so the tuner waits, and when
				// nothing else is keyed the 1 on its own matches no channel.
				{key: digitKey(1), wantPending: "1"},
				{expire: true, wait: DigitTimeout},
			},
		},
		{
			name:     "a two digit number nobody broadcasts on is discarded at once",
			channels: []Channel{{ID: "kids", Number: 1}, {ID: "docs", Number: 12}},
			current:  "docs",
			steps: []step{
				{key: digitKey(1), wantPending: "1"},
				{key: digitKey(4)},
			},
		},
		{
			name:     "channel up during a pending number clears it and moves",
			channels: testChannels(),
			current:  "kids",
			steps: []step{
				{key: digitKey(1), wantPending: "1"},
				{key: press(ChannelUp), wantChanged: true, wantChannel: "trains"},
				// The dropped 1 must not join the next number: a 2 on its own
				// matches nothing rather than tuning channel 12.
				{key: digitKey(2)},
			},
		},
		{
			name:     "keying the current channel number is not a change",
			channels: testChannels(),
			current:  "trains",
			steps: []step{
				{key: digitKey(3), wantChannel: "trains"},
			},
		},
		{
			name:     "digits do nothing with no channels configured",
			channels: nil,
			current:  "",
			steps: []step{
				{key: digitKey(1)},
				{key: digitKey(2)},
				{expire: true, wait: DigitTimeout},
			},
		},
		{
			name:     "up does nothing with no channels configured",
			channels: nil,
			current:  "",
			steps: []step{
				{key: press(ChannelUp)},
				{key: press(ChannelDown)},
			},
		},
		{
			name:     "power and volume keys leave the tuner alone",
			channels: testChannels(),
			current:  "kids",
			steps: []step{
				{key: digitKey(1), wantPending: "1"},
				{key: press(VolumeUp), wantPending: "1"},
				{key: press(Power), wantPending: "1"},
				{key: press(Mute), wantPending: "1"},
				{key: digitKey(2), wantChanged: true, wantChannel: "docs"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tuner := NewTuner(tc.channels, tc.current)
			now := start

			for i, s := range tc.steps {
				now = now.Add(s.wait)

				var changed bool
				var target Channel
				if s.expire {
					changed, target = tuner.Expire()
				} else {
					changed, target = tuner.Handle(s.key, now)
				}

				if changed != s.wantChanged {
					t.Fatalf("step %d: changed is %t, want %t", i, changed, s.wantChanged)
				}
				if s.wantChannel != "" && target.ID != s.wantChannel {
					t.Fatalf("step %d: reported channel %q, want %q", i, target.ID, s.wantChannel)
				}
				if pending, _ := tuner.Pending(); pending != s.wantPending {
					t.Fatalf("step %d: pending is %q, want %q", i, pending, s.wantPending)
				}
			}
		})
	}
}

// TestTunerCurrentFollowsTheKeys checks the tuner's own idea of where it is,
// which is what the next up or down moves from.
func TestTunerCurrentFollowsTheKeys(t *testing.T) {
	tuner := NewTuner(testChannels(), "kids")

	if changed, _ := tuner.Handle(digitKey(5), start); !changed {
		t.Fatal("keying 5 did not tune")
	}
	current, ok := tuner.Current()
	if !ok || current.ID != "clips" {
		t.Fatalf("current is %+v, want clips", current)
	}
	if _, target := tuner.Handle(press(ChannelUp), start); target.ID != "docs" {
		t.Errorf("up from the keyed channel went to %q, want docs", target.ID)
	}
}

// TestPendingDeadlineIsTheLastDigitPlusTheTimeout is what the station arms its
// timer on, so the deadline has to move with every digit rather than being set
// once when the number started.
func TestPendingDeadlineIsTheLastDigitPlusTheTimeout(t *testing.T) {
	tuner := NewTuner(testChannels(), "clips")

	if pending, deadline := tuner.Pending(); pending != "" || !deadline.IsZero() {
		t.Errorf("a fresh tuner reports pending %q with deadline %s, want neither", pending, deadline)
	}

	tuner.Handle(digitKey(1), start)
	pending, deadline := tuner.Pending()
	if pending != "1" {
		t.Fatalf("pending is %q, want 1", pending)
	}
	if want := start.Add(DigitTimeout); !deadline.Equal(want) {
		t.Errorf("deadline is %s, want %s", deadline, want)
	}
	if DigitTimeout != 1500*time.Millisecond {
		t.Errorf("the digit timeout is %s, want the 1.5s the plan specifies", DigitTimeout)
	}
}

// TestTunerTimeoutOverride covers the field the station uses to keep its own
// tests quick.
func TestTunerTimeoutOverride(t *testing.T) {
	tuner := NewTuner(testChannels(), "clips")
	tuner.Timeout = 20 * time.Millisecond

	tuner.Handle(digitKey(1), start)
	_, deadline := tuner.Pending()
	if want := start.Add(20 * time.Millisecond); !deadline.Equal(want) {
		t.Errorf("deadline is %s, want %s", deadline, want)
	}
}

// TestExpireWithNothingPendingDoesNothing guards the station's timer racing a
// digit that already committed.
func TestExpireWithNothingPendingDoesNothing(t *testing.T) {
	tuner := NewTuner(testChannels(), "clips")
	if changed, _ := tuner.Expire(); changed {
		t.Error("expiring an empty number tuned somewhere")
	}
}

// TestTunerCopiesItsChannels checks the caller can rebuild its own list without
// the tuner changing underneath it.
func TestTunerCopiesItsChannels(t *testing.T) {
	channels := testChannels()
	tuner := NewTuner(channels, "kids")
	channels[1] = Channel{ID: "gone", Number: 99}

	if _, target := tuner.Handle(press(ChannelUp), start); target.ID != "trains" {
		t.Errorf("up went to %q, want the channel the tuner was built with", target.ID)
	}
}

// TestUnknownChannelStartsFromTheEnds covers a tuned channel that vanished from
// the config at the 04:00 rescan.
func TestUnknownChannelStartsFromTheEnds(t *testing.T) {
	tuner := NewTuner(testChannels(), "gone")
	if _, target := tuner.Handle(press(ChannelUp), start); target.ID != "kids" {
		t.Errorf("up from an unknown channel went to %q, want the lowest number", target.ID)
	}

	tuner = NewTuner(testChannels(), "gone")
	if _, target := tuner.Handle(press(ChannelDown), start); target.ID != "docs" {
		t.Errorf("down from an unknown channel went to %q, want the highest number", target.ID)
	}
}
