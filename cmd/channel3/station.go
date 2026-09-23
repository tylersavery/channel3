package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tylersavery/channel3/internal/player"
	"github.com/tylersavery/channel3/internal/schedule"
)

// reconcileInterval is how often the station checks that mpv is playing what
// the schedule says it should be.
//
// It is the correction for everything the station cannot be told about: an NTP
// step after the Pi finds the network, a sidecar duration that disagrees with
// the file, an event that was dropped while the player was restarting.
const reconcileInterval = 30 * time.Second

// reconcileTolerance is how far playback may drift from the schedule before the
// station reloads.
//
// Seeking is visible, so this has to be loose enough to ignore the second or so
// a load takes and the rounding in mpv's reported position, and tight enough
// that a real jump is corrected.
const reconcileTolerance = 5 * time.Second

// station is the broadcast loop.
//
// Everything it knows is held in memory and thrown away on exit: the channels
// it loaded at startup, which channel is tuned, which items failed today, and
// which broadcast day it is on. Nothing about playback is written anywhere, so
// a restart plays whatever the clock says is on rather than resuming.
type station struct {
	player   player.Player
	clock    schedule.Clock
	now      func() time.Time
	reload   func() ([]schedule.Channel, error)
	log      *slog.Logger
	interval time.Duration

	channels  []schedule.Channel
	tuned     string
	excluded  map[string]map[string]bool
	day       time.Time
	lastKnown bool
	// lastLoad is the path last handed to the player, which is how the station
	// recognises an error event for the item it is already trying to play.
	lastLoad string
	// stalled says the station has declined to reload a file that keeps
	// failing and is waiting for the next reconcile tick.
	stalled bool
}

// stationOptions are what serve hands the station.
type stationOptions struct {
	Player player.Player
	Clock  schedule.Clock
	// Now reads the wall clock. Tests substitute a fake.
	Now func() time.Time
	// Reload rescans the library and rebuilds the channels, which the station
	// does once at startup and again at every broadcast day rollover.
	Reload func() ([]schedule.Channel, error)
	// StartChannel is the channel id to tune at startup. Empty tunes the
	// lowest numbered channel.
	StartChannel string
	Logger       *slog.Logger
	// Interval overrides the reconcile period. Zero takes the default.
	Interval time.Duration
}

// newStation builds a station and tunes it, without starting the loop.
func newStation(opts stationOptions) (*station, error) {
	if opts.Player == nil {
		return nil, fmt.Errorf("station: a player is required")
	}
	if opts.Now == nil {
		return nil, fmt.Errorf("station: a clock reader is required")
	}
	if opts.Reload == nil {
		return nil, fmt.Errorf("station: a reload function is required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = reconcileInterval
	}

	s := &station{
		player:   opts.Player,
		clock:    opts.Clock,
		now:      opts.Now,
		reload:   opts.Reload,
		log:      log,
		interval: interval,
		excluded: make(map[string]map[string]bool),
	}

	channels, err := s.reload()
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("station: no channels are configured")
	}
	s.channels = channels
	s.day = s.clock.BroadcastDay(s.now())

	if err := s.tune(opts.StartChannel); err != nil {
		return nil, err
	}
	return s, nil
}

// tune selects the channel to play. An empty id takes the lowest number, which
// is what a television does when it is switched on with no memory.
func (s *station) tune(id string) error {
	s.stalled = false
	if id == "" {
		// loadStation sorts by number, so the first is the lowest.
		s.tuned = s.channels[0].ID
		return nil
	}
	for _, ch := range s.channels {
		if ch.ID == id {
			s.tuned = id
			return nil
		}
	}
	return fmt.Errorf("station: no channel with id %q is configured", id)
}

// run drives the station until the context is cancelled.
//
// The Stand By card goes up before anything else, so the television shows the
// card rather than a black screen or a terminal while the first item is found
// and loaded.
func (s *station) run(ctx context.Context) error {
	if err := s.standby(); err != nil {
		s.log.Error("could not show the stand by card", "error", err)
	}
	s.play()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	events := s.player.Events()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return fmt.Errorf("station: the player stopped")
			}
			s.handle(ev)
		case <-ticker.C:
			s.reconcile()
		}
	}
}

// handle acts on one player event.
func (s *station) handle(ev player.Event) {
	switch ev.Kind {
	case player.EndFile:
		if ev.Reason == player.ReasonError && !s.exclude(ev.Path) && s.repeatingFailure() {
			return
		}
		// Both reasons end the same way: ask the schedule what is on now
		// rather than playing whatever came next in the list. The schedule is
		// the only thing that decides what airs.
		s.play()
	case player.Restarted:
		s.log.Warn("mpv restarted, reloading the current item")
		s.play()
	default:
		s.log.Warn("ignoring an unknown player event", "kind", ev.Kind)
	}
}

// exclude drops the item at path from the tuned channel for the rest of the
// broadcast day and reports whether it found one to drop.
//
// A file mpv cannot open would otherwise come round again on every pass, so the
// day's order is rebuilt without it. The exclusion is in memory only and is
// cleared at the 04:00 rollover, which is when a re-ingested file gets another
// chance.
func (s *station) exclude(path string) bool {
	channel, ok := s.channel(s.tuned)
	if !ok {
		return false
	}
	item, ok := findByPath(channel.Items, path)
	if !ok {
		// The event names a file the tuned channel does not have, which is
		// what an error arriving after a channel change looks like, and what
		// an event whose path the player could not resolve looks like.
		// Excluding the last item this station loaded would be a guess.
		s.log.Warn("an unplayable file is not on the tuned channel", "path", path, "channel", s.tuned)
		return false
	}
	if s.excluded[s.tuned] == nil {
		s.excluded[s.tuned] = make(map[string]bool)
	}
	s.excluded[s.tuned][item.ID] = true
	s.log.Warn("dropping an unplayable item for the rest of the day",
		"channel", s.tuned, "item", item.ID, "title", item.Title, "path", item.Path)
	return true
}

// repeatingFailure reports whether reloading now would hand mpv the same file
// that just failed.
//
// That happens when an error event excluded nothing, either because its path
// was empty or because it names a file the tuned channel does not have, while
// the schedule still wants the item the station last loaded. Reloading would
// fail again at once and spin. The station waits for the next reconcile tick
// instead, which retries at most once per interval.
func (s *station) repeatingFailure() bool {
	if s.lastLoad == "" {
		return false
	}
	channel, ok := s.channel(s.tuned)
	if !ok {
		return false
	}
	slot, ok := schedule.At(s.playable(channel), s.now(), s.clock)
	if !ok || slot.Item.Path != s.lastLoad {
		return false
	}
	if !s.stalled {
		s.log.Error("the player cannot play the scheduled item and it could not be excluded, waiting for the next reconcile",
			"path", s.lastLoad, "channel", s.tuned, "retry in", s.interval)
		s.stalled = true
	}
	return true
}

// play asks the schedule what the tuned channel is airing and loads it.
func (s *station) play() {
	now := s.now()
	channel, ok := s.channel(s.tuned)
	if !ok {
		s.log.Error("the tuned channel is gone", "channel", s.tuned)
		s.standbyOrLog()
		return
	}

	playable := s.playable(channel)
	slot, ok := schedule.At(playable, now, s.clock)
	if !ok {
		s.log.Info("nothing to play, showing stand by", "channel", channel.ID, "at", now.Format(time.RFC3339))
		s.lastKnown = false
		s.standbyOrLog()
		return
	}

	s.log.Info("loading",
		"channel", channel.ID,
		"number", channel.Number,
		"item", slot.Item.ID,
		"title", slot.Item.Title,
		"offset", slot.Offset.Round(time.Millisecond),
		"now", now.Format(time.RFC3339))

	if err := s.player.Load(slot.Item.Path, slot.Offset); err != nil {
		// A failed load is almost always mpv restarting underneath us. The
		// Restarted event or the next reconcile tick loads again.
		s.log.Error("could not load an item", "item", slot.Item.ID, "path", slot.Item.Path, "error", err)
		return
	}
	s.lastLoad = slot.Item.Path
	s.lastKnown = true
	s.stalled = false
}

// standby shows the card.
func (s *station) standby() error {
	s.lastKnown = false
	s.lastLoad = ""
	return s.player.Standby()
}

// standbyOrLog shows the card and reports a failure rather than swallowing it.
func (s *station) standbyOrLog() {
	if err := s.standby(); err != nil {
		s.log.Error("could not show the stand by card", "error", err)
	}
}

// reconcile checks that what mpv is playing is still what the schedule says.
//
// This is the periodic self-correction. It also notices the broadcast day
// rolling over, which is the one moment the library is rescanned and the day's
// exclusions are forgiven.
func (s *station) reconcile() {
	now := s.now()

	if day := s.clock.BroadcastDay(now); !day.Equal(s.day) {
		s.rollover(day)
		return
	}

	channel, ok := s.channel(s.tuned)
	if !ok {
		s.play()
		return
	}
	slot, ok := schedule.At(s.playable(channel), now, s.clock)
	if !ok {
		if s.lastKnown {
			s.log.Info("nothing left to play, showing stand by", "channel", channel.ID)
			s.standbyOrLog()
		}
		return
	}

	path, pos, err := s.player.Position()
	if err != nil {
		s.log.Warn("could not read the player position, reloading", "error", err)
		s.play()
		return
	}
	if path != slot.Item.Path {
		s.log.Info("the player is on the wrong item, reloading",
			"playing", path, "want", slot.Item.Path)
		s.play()
		return
	}
	if drift := pos - slot.Offset; drift > reconcileTolerance || drift < -reconcileTolerance {
		s.log.Info("the player has drifted from the schedule, reloading",
			"item", slot.Item.ID, "drift", drift.Round(time.Millisecond))
		s.play()
	}
}

// rollover starts a new broadcast day: rescan the library, rebuild the
// channels, forgive every item that failed yesterday and play the new order.
func (s *station) rollover(day time.Time) {
	s.log.Info("broadcast day rollover, rescanning the library", "day", day.Format(time.RFC3339))
	s.excluded = make(map[string]map[string]bool)

	// The day is advanced only once the rescan has worked. A library that is
	// briefly unreadable at 04:00 is then retried on the next reconcile tick
	// rather than skipped until tomorrow.
	channels, err := s.reload()
	if err != nil {
		// Yesterday's channels are still in memory and still playable, so the
		// broadcast continues on them rather than going to the card.
		s.log.Error("could not rescan the library, keeping the channels already loaded", "error", err)
		s.play()
		return
	}
	if len(channels) == 0 {
		s.log.Error("the rescan found no channels, keeping the channels already loaded")
		s.play()
		return
	}
	s.channels = channels
	s.day = day

	if _, ok := s.channel(s.tuned); !ok {
		s.log.Warn("the tuned channel is no longer configured, tuning the lowest", "channel", s.tuned)
		if err := s.tune(""); err != nil {
			s.log.Error("could not tune a channel after the rescan", "error", err)
		}
	}
	s.play()
}

// channel returns the tuned channel by id.
func (s *station) channel(id string) (schedule.Channel, bool) {
	for _, ch := range s.channels {
		if ch.ID == id {
			return ch, true
		}
	}
	return schedule.Channel{}, false
}

// playable returns ch without the items that failed today.
//
// Removing an item changes the day's order for the whole channel, which is what
// the plan asks for: a file that cannot be played is not part of today's
// broadcast at all.
func (s *station) playable(ch schedule.Channel) schedule.Channel {
	excluded := s.excluded[ch.ID]
	if len(excluded) == 0 {
		return ch
	}
	items := make([]schedule.Item, 0, len(ch.Items))
	for _, item := range ch.Items {
		if !excluded[item.ID] {
			items = append(items, item)
		}
	}
	ch.Items = items
	return ch
}

// findByPath returns the item with the given path.
func findByPath(items []schedule.Item, path string) (schedule.Item, bool) {
	if path == "" {
		return schedule.Item{}, false
	}
	for _, item := range items {
		if item.Path == path {
			return item, true
		}
	}
	return schedule.Item{}, false
}
