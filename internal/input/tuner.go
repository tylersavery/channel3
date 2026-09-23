package input

import (
	"strconv"
	"time"
)

// DigitTimeout is how long the tuner waits for another digit before it acts on
// the number already keyed.
//
// It only matters when the digits so far could still grow into a real channel
// number. Anything unambiguous commits at once, so this is the wait for typing
// "1" on a station that has both channel 1 and channel 12.
const DigitTimeout = 1500 * time.Millisecond

// Channel is the tuner's view of a channel.
//
// It is deliberately not schedule.Channel: nothing about tuning depends on what
// is in a channel, so this package stays free of the schedule and the library.
type Channel struct {
	ID     string
	Number int
}

// Tuner decides which channel the buttons mean.
//
// It is a pure state machine. It starts no goroutines, reads no clock and
// writes nothing: every method is given the time it should use, and the caller
// arms its own timer from Pending. That is what makes it table testable and
// what keeps the station's loop the only thing in the service that waits.
//
// A Tuner is not safe for concurrent use. The station owns one and touches it
// only from its own loop.
type Tuner struct {
	// Timeout is how long to wait for another digit. Zero takes DigitTimeout.
	// The station sets it only in tests.
	Timeout time.Duration

	channels []Channel
	current  string

	// pending holds the digits keyed so far, as typed, so "03" and "3" stay
	// distinguishable while the number is being entered.
	pending string
	// lastDigit is when the most recent digit arrived, which is what the
	// deadline is measured from.
	lastDigit time.Time
}

// NewTuner returns a tuner over channels, sorted by number, tuned to current.
//
// The channel list is copied, so the caller may rebuild its own. An id that is
// not in the list leaves the tuner with no current channel, which is harmless:
// up and down then start from the lowest number.
func NewTuner(channels []Channel, current string) *Tuner {
	copied := make([]Channel, len(channels))
	copy(copied, channels)
	return &Tuner{channels: copied, current: current}
}

// Current returns the channel the tuner is on.
func (t *Tuner) Current() (Channel, bool) {
	return t.find(t.current)
}

// Handle acts on one key and reports whether the channel changed.
//
// now is only used to stamp a digit, so a caller with a fake clock gets a fake
// deadline out of Pending and nothing in here reads the wall clock behind its
// back.
//
// Power, volume and mute are not the tuner's business and are reported as no
// change, pending digits untouched: pressing volume up midway through a channel
// number should not throw the number away.
func (t *Tuner) Handle(key Key, now time.Time) (bool, Channel) {
	switch key.Action {
	case ChannelUp:
		return t.step(+1)
	case ChannelDown:
		return t.step(-1)
	case Digit:
		return t.digit(key.Digit, now)
	default:
		return false, Channel{}
	}
}

// Pending returns the digits keyed so far and when they expire.
//
// The station arms a timer on the deadline and calls Expire when it fires. An
// empty string means nothing is pending and the deadline is the zero time.
func (t *Tuner) Pending() (string, time.Time) {
	if t.pending == "" {
		return "", time.Time{}
	}
	return t.pending, t.lastDigit.Add(t.timeout())
}

// Expire commits the digits keyed so far, if any.
//
// It is the caller's timer that decides when this happens, which is why it
// takes no time: the station calls it when the deadline it read from Pending
// has passed, and a digit arriving first re-arms that timer instead.
func (t *Tuner) Expire() (bool, Channel) {
	if t.pending == "" {
		return false, Channel{}
	}
	return t.commit()
}

// step moves by one channel in the given direction, wrapping at both ends.
//
// Half typed digits are dropped: someone who keys "1" and then reaches for
// channel up has changed their mind, and carrying the 1 into the next number
// would tune somewhere nobody asked for.
func (t *Tuner) step(delta int) (bool, Channel) {
	t.clear()
	if len(t.channels) == 0 {
		return false, Channel{}
	}

	index, ok := t.indexOf(t.current)
	if !ok {
		// Nothing is tuned, which happens when the current channel has just
		// been removed from the config. Up starts at the lowest number and
		// down at the highest.
		if delta > 0 {
			index = -1
		} else {
			index = 0
		}
	}

	next := (index + delta + len(t.channels)) % len(t.channels)
	return t.selectIndex(next)
}

// digit adds one digit to the pending number and commits it when it can.
//
// The number commits at once unless a longer channel number starts with the
// digits so far. With channels 3, 5 and 12, keying 3 tunes immediately because
// no channel number begins with 3 and continues; keying 1 waits, because 12
// might still be coming.
func (t *Tuner) digit(d int, now time.Time) (bool, Channel) {
	if d < 0 || d > 9 {
		return false, Channel{}
	}
	t.pending += strconv.Itoa(d)
	t.lastDigit = now

	if t.hasLongerMatch() {
		return false, Channel{}
	}
	return t.commit()
}

// hasLongerMatch reports whether some channel number starts with the pending
// digits and is longer than them, so another digit is worth waiting for.
func (t *Tuner) hasLongerMatch() bool {
	for _, ch := range t.channels {
		number := strconv.Itoa(ch.Number)
		if len(number) > len(t.pending) && number[:len(t.pending)] == t.pending {
			return true
		}
	}
	return false
}

// commit turns the pending digits into a channel change, or throws them away.
//
// A number nobody broadcasts on is discarded and the current channel keeps
// playing, which is what a television does: keying 47 on a box with three
// channels changes nothing.
func (t *Tuner) commit() (bool, Channel) {
	digits := t.pending
	t.clear()

	number, err := strconv.Atoi(digits)
	if err != nil {
		return false, Channel{}
	}
	for i, ch := range t.channels {
		if ch.Number == number {
			return t.selectIndex(i)
		}
	}
	return false, Channel{}
}

// selectIndex tunes the channel at index and reports whether that is a change.
//
// Tuning to the channel already playing is not a change. Reloading it would
// restart the item mid scene for no reason, which on a television looks like a
// glitch rather than an answer to the button.
func (t *Tuner) selectIndex(index int) (bool, Channel) {
	ch := t.channels[index]
	if ch.ID == t.current {
		return false, ch
	}
	t.current = ch.ID
	return true, ch
}

// clear drops any half typed number.
func (t *Tuner) clear() {
	t.pending = ""
	t.lastDigit = time.Time{}
}

// timeout is the configured digit wait, or the default.
func (t *Tuner) timeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return DigitTimeout
}

// indexOf returns the position of a channel id in the list.
func (t *Tuner) indexOf(id string) (int, bool) {
	for i, ch := range t.channels {
		if ch.ID == id {
			return i, true
		}
	}
	return 0, false
}

// find returns a channel by id.
func (t *Tuner) find(id string) (Channel, bool) {
	i, ok := t.indexOf(id)
	if !ok {
		return Channel{}, false
	}
	return t.channels[i], true
}
