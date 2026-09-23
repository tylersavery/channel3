package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/tylersavery/channel3/internal/input"
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
	// keys is the merged stream of remote and keyboard presses. A nil channel
	// is a station nobody can change, which is what --no-input asks for.
	keys <-chan input.Key
	// cec forwards power and volume to the television. Nil means --cec is off
	// and those buttons are logged and dropped.
	cec input.CEC
	// controls hands television keys to the worker that talks to cec-ctl. It
	// holds one key, the most recent, because a press that has been waiting
	// behind a television that is not answering is no longer worth sending.
	controls chan input.Key
	// digitTimeout overrides how long the tuner waits for another digit. Zero
	// takes the tuner's default; tests shorten it.
	digitTimeout time.Duration

	// channels is the whole station, replaced wholesale at every rollover.
	// The loop is its only writer and reads it directly; the API reads a copy
	// through Channels, which is why every write goes through setChannels.
	channels []schedule.Channel
	// tuner decides what the buttons mean. It is rebuilt whenever the channel
	// list or the tuned channel changes, which also throws away any half typed
	// number, the right answer when the channel has just changed underneath it.
	tuner *input.Tuner
	// mu guards tuned and channels, which the loop writes and the API reads
	// from the HTTP server's goroutines. Everything else in here belongs to the
	// loop alone.
	mu        sync.RWMutex
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
	// Keys is the merged key stream. Nil means no input at all.
	Keys <-chan input.Key
	// CEC forwards power and volume to the television. Nil drops them.
	CEC input.CEC
	// DigitTimeout overrides how long a half typed channel number waits for
	// another digit. Zero takes input.DigitTimeout.
	DigitTimeout time.Duration
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
		player:       opts.Player,
		clock:        opts.Clock,
		now:          opts.Now,
		reload:       opts.Reload,
		log:          log,
		interval:     interval,
		keys:         opts.Keys,
		cec:          opts.CEC,
		controls:     make(chan input.Key, 1),
		digitTimeout: opts.DigitTimeout,
		excluded:     make(map[string]map[string]bool),
	}

	channels, err := s.reload()
	if err != nil {
		return nil, err
	}
	s.setChannels(channels)
	s.day = s.clock.BroadcastDay(s.now())

	// A station with nothing configured still starts. The television shows the
	// card, the guide answers with an empty list, and the next rollover or
	// rescan adopts whatever has appeared since. Refusing to start would leave
	// an appliance whose first ingest has not happened yet with no service at
	// all, and no way to see that from a phone.
	if len(channels) == 0 {
		log.Warn("no channels are configured, showing stand by")
	}

	if err := s.tune(opts.StartChannel); err != nil {
		if len(channels) > 0 {
			return nil, err
		}
		// There is nothing to tune yet, so a named start channel is not wrong,
		// only early. It is dropped rather than remembered: the lowest numbered
		// channel is tuned when channels appear, exactly as a bare serve does.
		log.Warn("the start channel is not configured yet", "channel", opts.StartChannel, "error", err)
		if err := s.tune(""); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// tune selects the channel to play. An empty id takes the lowest number, which
// is what a television does when it is switched on with no memory.
func (s *station) tune(id string) error {
	s.stalled = false
	if id == "" {
		if len(s.channels) == 0 {
			// Nothing to tune. The station stays on the card and the tuner is
			// still rebuilt, so a key press is a no-op rather than a panic.
			s.setTuned("")
			return nil
		}
		// loadStation sorts by number, so the first is the lowest.
		s.setTuned(s.channels[0].ID)
		return nil
	}
	for _, ch := range s.channels {
		if ch.ID == id {
			s.setTuned(id)
			return nil
		}
	}
	return fmt.Errorf("station: no channel with id %q is configured", id)
}

// setTuned records the tuned channel and rebuilds the tuner over it.
func (s *station) setTuned(id string) {
	s.mu.Lock()
	s.tuned = id
	s.mu.Unlock()
	s.retune()
}

// setChannels replaces the station's channel list.
//
// Every assignment to s.channels goes through here, so the API's reader is
// never handed a slice header that is half written.
func (s *station) setChannels(channels []schedule.Channel) {
	s.mu.Lock()
	s.channels = channels
	s.mu.Unlock()
}

// Channels is the station's channel list, in the order it was loaded, which
// loadStation sorts by number.
//
// The API reads this from the HTTP server's goroutines while the loop may be
// replacing the list at a rollover, so the slice is copied under the lock. The
// items inside it are not copied: nothing ever mutates an item or an Items
// slice in place, the whole channel list is replaced instead.
func (s *station) Channels() []schedule.Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]schedule.Channel, len(s.channels))
	copy(out, s.channels)
	return out
}

// PlayableChannels is the channel list as the broadcast loop sees it: today's
// exclusions already removed.
//
// This is what the API answers from, so the guide and the screen agree about
// what is on. An item mpv could not open is not part of today's order on the
// television, and it must not be part of it in the guide either.
//
// Both the list and the exclusions are read under the same lock the loop writes
// them under, so a request arriving mid rollover sees one consistent station.
func (s *station) PlayableChannels() []schedule.Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]schedule.Channel, 0, len(s.channels))
	for _, ch := range s.channels {
		out = append(out, s.playable(ch))
	}
	return out
}

// Tuned is the channel id currently on air.
//
// Phase 7's API reads this from the HTTP server's goroutine, which is why the
// field behind it is guarded. It reports what the station is tuned to, never
// what mpv happens to be doing.
func (s *station) Tuned() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tuned
}

// retune rebuilds the tuner over the current channel list.
//
// It is called whenever the channels or the tuned channel change, so the tuner
// never moves through a channel that has gone from the config.
func (s *station) retune() {
	s.tuner = input.NewTuner(tunerChannels(s.channels), s.tuned)
	s.tuner.Timeout = s.digitTimeout
}

// tunerChannels is the tuner's view of the station: numbers and ids, nothing
// about what is in them.
func tunerChannels(channels []schedule.Channel) []input.Channel {
	out := make([]input.Channel, 0, len(channels))
	for _, ch := range channels {
		out = append(out, input.Channel{ID: ch.ID, Number: ch.Number})
	}
	return out
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

	// digits fires when a half typed channel number has waited long enough.
	// It is stopped whenever nothing is pending, so an idle station is not
	// woken by it.
	digits := time.NewTimer(time.Hour)
	digits.Stop()
	defer digits.Stop()

	// Television keys are sent on their own goroutine. cec-ctl can sit for its
	// full timeout against a set that is off, and an end-file event arriving
	// meanwhile must not wait behind it: the screen would hold a finished item
	// for as long as the television took to answer.
	if s.cec != nil {
		controlCtx, stopControls := context.WithCancel(ctx)
		controlsDone := make(chan struct{})
		go func() {
			defer close(controlsDone)
			s.serveControls(controlCtx)
		}()
		defer func() {
			stopControls()
			<-controlsDone
		}()
	}

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
		case key, ok := <-s.keys:
			if !ok {
				// Every source has gone. The broadcast carries on, nobody can
				// change the channel, and a nil channel blocks here forever
				// rather than spinning on a closed one.
				s.log.Warn("no input sources are left, the channel can no longer be changed")
				s.keys = nil
				continue
			}
			s.key(key)
		case <-digits.C:
			s.expireDigits()
		case <-ticker.C:
			s.reconcile()
		}
		s.armDigits(digits)
	}
}

// key acts on one press.
func (s *station) key(k input.Key) {
	switch k.Action {
	case input.Power, input.VolumeUp, input.VolumeDown, input.Mute:
		s.control(k)
		return
	}

	pending, _ := s.tuner.Pending()
	changed, target := s.tuner.Handle(k, s.now())
	if changed {
		s.tuneTo(target)
		return
	}
	if k.Action == input.Digit {
		s.reportNumber(pending+strconv.Itoa(k.Digit), target)
	}
}

// expireDigits commits a channel number that has stopped growing.
func (s *station) expireDigits() {
	pending, _ := s.tuner.Pending()
	changed, target := s.tuner.Expire()
	if changed {
		s.tuneTo(target)
		return
	}
	if pending != "" {
		s.reportNumber(pending, target)
	}
}

// reportNumber says what became of a keyed channel number that did not change
// the channel.
//
// Someone standing at the television has pressed a button and nothing has
// happened, and the only place that can be explained is the log.
func (s *station) reportNumber(number string, target input.Channel) {
	if waiting, _ := s.tuner.Pending(); waiting != "" {
		s.log.Debug("waiting for the rest of a channel number", "keyed", waiting)
		return
	}
	if target.ID != "" {
		s.log.Info("already on that channel", "number", target.Number, "channel", target.ID)
		return
	}
	s.log.Info("no channel has that number, staying put", "keyed", number, "channel", s.tuned)
}

// armDigits sets the timer from whatever the tuner is waiting for.
//
// The deadline comes from the tuner, which stamped it with the station's clock,
// so a test with a fake clock gets a wait measured in the same units it is
// moving. Nothing is armed when no number is half typed.
func (s *station) armDigits(timer *time.Timer) {
	pending, deadline := s.tuner.Pending()
	if pending == "" {
		timer.Stop()
		return
	}
	wait := deadline.Sub(s.now())
	if wait < 0 {
		wait = 0
	}
	timer.Reset(wait)
}

// tuneTo changes channel and plays whatever the new one is airing.
//
// Nothing about the old channel is remembered. The new channel is joined at
// whatever it is up to, exactly as it would be on a television.
func (s *station) tuneTo(target input.Channel) {
	if err := s.tune(target.ID); err != nil {
		s.log.Error("could not tune", "channel", target.ID, "error", err)
		return
	}
	s.log.Info("tune", "number", target.Number, "channel", target.ID)
	s.play()
}

// control queues a power or volume button for the television.
//
// These do nothing to the broadcast. With --cec off there is nowhere to send
// them, which is the normal state on a television that does not do CEC, and the
// press is logged so it is clear the button was seen.
//
// The queue holds one key. A press that arrives while the television is still
// being spoken to replaces whatever was waiting, because volume up pressed four
// times quickly means "louder now", not four commands to send once the set
// finally answers.
func (s *station) control(k input.Key) {
	if s.cec == nil {
		s.log.Info("ignoring a television key, cec is off", "key", k.String())
		return
	}

	select {
	case s.controls <- k:
		return
	default:
	}

	// The loop is the only sender, so at most one key is dropped here and the
	// one just pressed takes its place.
	select {
	case dropped := <-s.controls:
		s.log.Debug("dropping a television key that is still waiting", "key", dropped.String())
	default:
	}
	select {
	case s.controls <- k:
	default:
		s.log.Warn("could not queue a television key", "key", k.String())
	}
}

// serveControls sends queued television keys until the station stops.
//
// It runs on its own goroutine so that a cec-ctl invocation, which may take its
// full timeout, never holds up the broadcast loop.
func (s *station) serveControls(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case k := <-s.controls:
			s.sendControl(k)
		}
	}
}

// sendControl asks the television to do one thing.
func (s *station) sendControl(k input.Key) {
	var err error
	switch k.Action {
	case input.Power:
		err = s.cec.Power()
	case input.VolumeUp:
		err = s.cec.VolumeUp()
	case input.VolumeDown:
		err = s.cec.VolumeDown()
	case input.Mute:
		err = s.cec.Mute()
	}
	if err != nil {
		// The CEC implementation has already logged the command that failed.
		// A television that will not listen must never stop the broadcast.
		s.log.Debug("the television did not accept a key", "key", k.String(), "error", err)
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
	// The loop is the only writer, but the API reads the exclusions through
	// PlayableChannels from the HTTP server's goroutines, so the write is
	// guarded even though nothing competes with it.
	s.mu.Lock()
	if s.excluded[s.tuned] == nil {
		s.excluded[s.tuned] = make(map[string]bool)
	}
	s.excluded[s.tuned][item.ID] = true
	s.mu.Unlock()

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
	if len(s.channels) == 0 {
		// A station with nothing configured. This is a normal state on a box
		// whose first ingest has not happened, so it is a stand by card and a
		// log line rather than a failure.
		s.log.Info("no channels are configured, showing stand by")
		s.standbyOrLog()
		return
	}
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

	if len(s.channels) == 0 {
		// The card is already up and there is nothing to reconcile it against.
		// Channels are adopted at the next rollover, which is the one moment
		// the library is rescanned.
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
	s.mu.Lock()
	s.excluded = make(map[string]map[string]bool)
	s.mu.Unlock()

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
	s.setChannels(channels)
	s.day = day

	if _, ok := s.channel(s.tuned); !ok {
		if s.tuned == "" {
			s.log.Info("the rescan found channels, tuning the lowest", "channels", len(channels))
		} else {
			s.log.Warn("the tuned channel is no longer configured, tuning the lowest", "channel", s.tuned)
		}
		if err := s.tune(""); err != nil {
			s.log.Error("could not tune a channel after the rescan", "error", err)
		}
	} else {
		// The channel list the tuner moves through has just been replaced, so
		// it is rebuilt even though the tuned channel is unchanged.
		s.retune()
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
//
// It reads s.excluded and takes no lock of its own. The loop calls it directly,
// and PlayableChannels calls it holding the read lock; both are safe because
// every write to the map happens under the write lock.
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
