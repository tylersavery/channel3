package main

import (
	"cmp"
	"slices"

	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/schedule"
)

// loadStation reads the channel config and the library under root and returns
// the schedule's view of the station, sorted by channel number.
//
// This is the one place the library package's types become the schedule
// package's, which is what keeps internal/schedule free of any dependency on
// how content is stored. guide uses it now and serve uses it from Phase 5, so
// it holds nothing either command does with the result.
//
// A channel with nothing playable is kept, with no items. It is a configured
// channel that shows the Please Stand By card, not an error.
func loadStation(root string) ([]schedule.Channel, error) {
	channels, err := library.LoadChannels(library.ChannelsDir(root))
	if err != nil {
		return nil, err
	}
	index, err := library.Scan(root, channels)
	if err != nil {
		return nil, err
	}

	station := make([]schedule.Channel, 0, len(channels))
	for _, ch := range channels {
		items := index.Items(ch.ID)
		scheduled := make([]schedule.Item, 0, len(items))
		for _, item := range items {
			scheduled = append(scheduled, schedule.Item{
				ID:       item.ID,
				Title:    item.Title,
				Path:     item.Path,
				Duration: item.Duration,
			})
		}
		station = append(station, schedule.Channel{
			ID:     ch.ID,
			Number: ch.Number,
			Name:   ch.Name,
			Items:  scheduled,
		})
	}

	slices.SortStableFunc(station, func(a, b schedule.Channel) int {
		return cmp.Compare(a.Number, b.Number)
	})
	return station, nil
}
