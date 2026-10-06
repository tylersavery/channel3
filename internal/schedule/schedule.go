// Package schedule answers the only question Channel Three asks about playback:
// given a channel and an instant, what is airing and how far into it are we.
//
// The answer is a pure function of the clock and the channel's items. Nothing
// here is persisted, nothing is cached, and no state survives a call. Tuning to
// a channel, restarting mpv, rebooting the Pi and opening the guide on a phone
// all run the same function and get the same answer.
//
// The broadcast day starts at 04:00 local rather than midnight, so the daily
// reshuffle happens when nobody is watching. Within a day the order is a seeded
// shuffle of the channel's items, played end to end and then looped.
//
// This package defines its own Item and Channel types and imports only the
// standard library. It never reads config, never touches the filesystem and
// never opens a network connection. cmd/channel3/load.go adapts the library
// package's types into these.
package schedule

import "time"

// Item is one playable video, as the schedule sees it.
type Item struct {
	ID    string
	Title string
	// Artist is a song's artist, which is also what marks an item as a song
	// on screen. Empty for video.
	Artist string
	// Gain is the dB to raise or lower the item by so every item plays at
	// about the same loudness. Zero leaves it at its own level.
	Gain     float64
	Path     string
	Duration time.Duration
}

// Channel is one themed channel and the items it draws on.
type Channel struct {
	ID     string
	Number int
	Name   string
	Items  []Item
}

// Slot is one airing of one item.
type Slot struct {
	Item   Item
	Start  time.Time     // wall clock start of this airing
	End    time.Time     // wall clock end of this airing; Start + Item.Duration except where Guide cuts an airing at the broadcast day rollover
	Offset time.Duration // how far into Item at the query time; zero for future slots
}

// Clock is the local view of time a channel is scheduled against.
//
// A zero Clock is not meant to be used directly. Location nil falls back to the
// system's local zone, but DayStart is taken literally, so Clock{} schedules a
// broadcast day starting at midnight. Build one with NewClock.
type Clock struct {
	Location *time.Location // channel's local zone; time.Local on the Pi
	DayStart time.Duration  // 4 * time.Hour
}

// location resolves the zone this clock schedules in.
func (c Clock) location() *time.Location {
	if c.Location == nil {
		return time.Local
	}
	return c.Location
}

// At returns what ch is airing at t and how far into it that instant is.
//
// It reports false when the channel has nothing playable, which is the caller's
// cue to show the Please Stand By card. Exactly on a boundary the next item
// starts at offset zero; one nanosecond earlier the previous item is still
// airing with one nanosecond left.
//
// Start and End are the wall clock times of this airing, in the clock's zone.
// They carry no monotonic reading even when t does, so they compare and print
// as plain wall clock times.
func At(ch Channel, t time.Time, c Clock) (Slot, bool) {
	day := c.BroadcastDay(t)
	order := Order(ch, day)
	total := totalDuration(order)
	if total <= 0 {
		return Slot{}, false
	}

	// BroadcastDay never returns an instant after t for a sane DayStart, but a
	// caller that sets DayStart outside 0 to 24 h can make elapsed negative.
	// Normalising here keeps pos in [0, total) whatever it was handed.
	elapsed := t.Sub(day)
	pos := elapsed % total
	if pos < 0 {
		pos += total
	}

	cursor := day.Add(elapsed - pos)
	for _, item := range order {
		if pos < item.Duration {
			return Slot{
				Item:   item,
				Start:  cursor,
				End:    cursor.Add(item.Duration),
				Offset: pos,
			}, true
		}
		pos -= item.Duration
		cursor = cursor.Add(item.Duration)
	}

	// Unreachable: pos is strictly less than the sum of the durations walked
	// above. Reporting false rather than panicking keeps an impossible order
	// from taking the whole service down; the caller shows Stand By.
	return Slot{}, false
}

// totalDuration is the length of one full pass through items.
func totalDuration(items []Item) time.Duration {
	var total time.Duration
	for _, item := range items {
		total += item.Duration
	}
	return total
}
