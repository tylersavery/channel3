package main

import (
	"context"
	"log/slog"

	"github.com/tylersavery/channel3/internal/schedule"
	"github.com/tylersavery/channel3/internal/shelf"
)

// shelfCovers makes the first cover for every shelf item that has none: a
// frame a quarter of the way into its video. It works one frame at a time at
// the lowest priority, whenever the shelf is saved and once the station is up.
type shelfCovers struct {
	store    *shelf.Store
	covers   shelf.Covers
	channels func() []schedule.Channel
	kick     chan struct{}
	log      *slog.Logger
}

func newShelfCovers(store *shelf.Store, covers shelf.Covers, channels func() []schedule.Channel, log *slog.Logger) *shelfCovers {
	return &shelfCovers{store: store, covers: covers, channels: channels, kick: make(chan struct{}, 1), log: log}
}

// Kick asks for a pass. A pass already asked for covers this one too.
func (w *shelfCovers) Kick() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Run makes covers until ctx ends.
func (w *shelfCovers) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
			w.fill(ctx)
		}
	}
}

// fill makes every missing cover it can.
func (w *shelfCovers) fill(ctx context.Context) {
	current, err := w.store.Read()
	if err != nil {
		w.log.Error("could not read the shelf to make covers", "error", err)
		return
	}
	lib := shelf.NewLibrary(w.channels())
	var items []shelf.Item
	for _, e := range current.Entries {
		if e.Item != nil {
			items = append(items, *e.Item)
		} else {
			items = append(items, e.Folder.Items...)
		}
	}
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		key := shelf.ItemKey(it.Ref())
		video, ok := lib[it.Ref()]
		if !ok || w.covers.Has(key) {
			continue
		}
		if err := w.covers.FromFrame(ctx, key, video.Path, shelf.DefaultFrame(video.Duration)); err != nil {
			w.log.Warn("could not make a cover", "key", key, "error", err)
			continue
		}
		w.log.Info("made a cover", "key", key)
	}
}
