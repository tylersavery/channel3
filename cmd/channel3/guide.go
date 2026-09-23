package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/schedule"
)

// timeLayout is how a slot's start is printed: local wall clock, to the minute.
const timeLayout = "15:04"

// standbyLine stands in for a channel with nothing playable, which is what the
// TV shows for it.
const standbyLine = "(nothing to play: Please Stand By)"

// runGuide prints what every channel is airing over the next few hours.
//
// It is the manual test harness for the schedule package: the same pure function
// serve plays from, printed as text. Nothing here reads or writes playback
// state, and --at makes any instant, past or future, printable.
func runGuide(g *globals, args []string) error {
	fs := g.flagSet("guide")
	hours := fs.Int("hours", 6, "hours of schedule to print")
	at := fs.String("at", "", "the instant to print the schedule for, RFC 3339 (default now)")
	channelID := fs.String("channel", "", "only print the channel with this id")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "channel3 guide: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return &exitError{code: 1}
	}
	if *hours < 1 || *hours > 48 {
		return &exitError{code: 1, err: fmt.Errorf("--hours must be between 1 and 48, got %d", *hours)}
	}

	now := time.Now()
	if *at != "" {
		parsed, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			return &exitError{code: 1, err: fmt.Errorf("--at: %w", err)}
		}
		now = parsed
	}

	station, err := loadStation(g.root)
	if err != nil {
		return &exitError{code: 1, err: err}
	}
	if *channelID != "" {
		station = slices.DeleteFunc(station, func(ch schedule.Channel) bool {
			return ch.ID != *channelID
		})
		if len(station) == 0 {
			return &exitError{code: 1, err: fmt.Errorf("no channel with id %q is configured in %s",
				*channelID, library.ChannelsDir(g.root))}
		}
	}
	if len(station) == 0 {
		return &exitError{code: 1, err: fmt.Errorf("no channels are configured in %s",
			library.ChannelsDir(g.root))}
	}

	printGuide(os.Stdout, station, now, time.Duration(*hours)*time.Hour, schedule.NewClock(time.Local))
	return nil
}

// printGuide writes one block per channel: the number and name, then a line per
// airing. Slot times come back from the schedule in the clock's own zone, so
// they print as local wall clock without any conversion here.
func printGuide(w io.Writer, station []schedule.Channel, now time.Time, horizon time.Duration, clock schedule.Clock) {
	for i, ch := range station {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%3d  %s\n", ch.Number, ch.Name)

		slots := schedule.Guide(ch, now, horizon, clock)
		if len(slots) == 0 {
			fmt.Fprintf(w, "     %s\n", standbyLine)
			continue
		}
		for j, slot := range slots {
			line := fmt.Sprintf("     %s  %s", slot.Start.Format(timeLayout), slot.Item.Title)
			if j == 0 {
				line += "  " + remainingLabel(slot.End.Sub(now))
			}
			fmt.Fprintln(w, line)
		}
	}
}

// remainingLabel says how much of the current airing is left, rounded up to the
// next whole minute so an item with seconds to run never reads as zero.
func remainingLabel(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("(remaining %dm)", int((d+time.Minute-1)/time.Minute))
}
