package main

import (
	"cmp"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/tylersavery/channel3/internal/bumper"
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

// loadCards reads every channel's bumper card under root: its name and colour
// from the channel config, its icon from <root>/icons.
//
// Nothing here stops the broadcast. Config that does not load leaves every
// channel without a card, and an icon that cannot be read leaves that channel's
// card with its name alone; both are logged. The config itself is reported by
// loadStation, which reads the same files.
func loadCards(root string) map[string]bumper.Card {
	channels, err := library.LoadChannels(library.ChannelsDir(root))
	if err != nil {
		slog.Warn("no bumper cards, the channel config did not load", "error", err)
		return nil
	}
	cards := make(map[string]bumper.Card)
	for _, ch := range channels {
		if ch.Bumper == nil {
			continue
		}
		card := bumper.Card{Name: ch.Name, Color: ch.Bumper.Color}
		if ch.Bumper.Icon != "" {
			path := filepath.Join(library.IconsDir(root), ch.Bumper.Icon)
			icon, err := os.ReadFile(path)
			if err != nil {
				slog.Warn("the channel's icon cannot be read, its card shows the name alone",
					"channel", ch.ID, "icon", path, "error", err)
			} else {
				card.Icon = icon
			}
		}
		cards[ch.ID] = card
	}
	return cards
}

// ingestLocal makes sidecars for local files and folders only, which serve runs
// after a home video upload. It never touches the network, so it is allowed
// while serve is broadcasting.
func ingestLocal(root string) error {
	channels, err := library.LoadChannels(library.ChannelsDir(root))
	if err != nil {
		return err
	}
	report, err := library.Ingest(library.IngestOptions{
		Root:      root,
		Channels:  channels,
		Prober:    library.FFProbe{Path: "ffprobe"},
		LocalOnly: true,
		Out:       io.Discard,
	})
	if err != nil {
		return err
	}
	slog.Info("local files ingested", "new", report.OK, "already in", report.Skipped, "failed", report.Failed)
	return nil
}
