// Package input turns button presses into channel changes.
//
// A key arrives from one of three places and they are all the same to the rest
// of the service: an evdev device on the Pi, which is any USB keyboard and in
// practice the Flirc receiver translating a household remote, the terminal on a
// developer's Mac, or a test. Each of those is a Source, and Merge fans them
// into the one stream the station reads.
//
// What a key means is decided by the Tuner, a pure state machine with no
// goroutines and no clock of its own. Power and volume keys are not the tuner's
// business: the station hands those to CEC, which is the only way this service
// touches the television itself.
//
// Nothing here is written to disk. The tuned channel lives in memory and dies
// with the process, because what plays is a function of the clock and never of
// what anyone pressed yesterday.
package input

import (
	"context"
	"fmt"
	"sync"
)

// Action is what a button means.
type Action int

const (
	// ChannelUp moves to the next channel by number, wrapping at the top.
	ChannelUp Action = iota
	// ChannelDown moves to the previous channel by number, wrapping at the
	// bottom.
	ChannelDown
	// Digit is one keyed digit of a channel number, carried in Key.Digit.
	Digit
	// Power asks the television to sleep or wake.
	Power
	// VolumeUp raises the television's volume.
	VolumeUp
	// VolumeDown lowers it.
	VolumeDown
	// Mute toggles it.
	Mute
)

// String names the action for logs.
func (a Action) String() string {
	switch a {
	case ChannelUp:
		return "channel-up"
	case ChannelDown:
		return "channel-down"
	case Digit:
		return "digit"
	case Power:
		return "power"
	case VolumeUp:
		return "volume-up"
	case VolumeDown:
		return "volume-down"
	case Mute:
		return "mute"
	default:
		return fmt.Sprintf("action(%d)", int(a))
	}
}

// Key is one press.
//
// Digit is meaningful only when Action is Digit, where it holds 0 to 9.
type Key struct {
	Action Action
	Digit  int
}

// String describes the key for logs.
func (k Key) String() string {
	if k.Action == Digit {
		return fmt.Sprintf("digit %d", k.Digit)
	}
	return k.Action.String()
}

// Source is somewhere keys come from.
//
// Keys returns a channel that carries presses until ctx is cancelled or the
// source fails for good, at which point the channel is closed. A source is
// expected to survive its device being unplugged rather than closing, because
// the Flirc can be pulled out of the Pi and pushed back in.
type Source interface {
	Keys(ctx context.Context) <-chan Key
}

// Merge fans several sources into one stream.
//
// The returned channel is closed once every source has closed, so a station
// reading it can tell the difference between "no keys just now" and "no input
// at all any more". A nil source is ignored, which is what an evdev device that
// could not be found looks like.
func Merge(ctx context.Context, sources ...Source) <-chan Key {
	out := make(chan Key)
	var wg sync.WaitGroup

	for _, src := range sources {
		if src == nil {
			continue
		}
		wg.Add(1)
		go func(src Source) {
			defer wg.Done()
			for key := range src.Keys(ctx) {
				select {
				case out <- key:
				case <-ctx.Done():
					return
				}
			}
		}(src)
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
