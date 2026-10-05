package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/input"
	"github.com/tylersavery/channel3/internal/player"
	"github.com/tylersavery/channel3/internal/schedule"
)

// loadCall is one Load the station asked the player for.
type loadCall struct {
	Path   string
	Offset time.Duration
}

// fakePlayer stands in for mpv. No test in this package starts a real one.
type fakePlayer struct {
	mu       sync.Mutex
	loads    []loadCall
	standbys int
	position loadCall
	posErr   error
	loadErr  error
	texts    []textCall
	events   chan player.Event
}

// textCall is one ShowText the station made.
type textCall struct {
	Text     string
	Duration time.Duration
}

// newFakePlayer returns a player that records what it is asked to do.
func newFakePlayer() *fakePlayer {
	return &fakePlayer{events: make(chan player.Event, 8)}
}

func (p *fakePlayer) Load(path string, offset time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loadErr != nil {
		return p.loadErr
	}
	p.loads = append(p.loads, loadCall{Path: path, Offset: offset})
	// A real mpv is then playing that file at that offset, which is what the
	// reconcile tick reads back.
	p.position = loadCall{Path: path, Offset: offset}
	return nil
}

func (p *fakePlayer) Standby() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.standbys++
	p.position = loadCall{}
	return nil
}

func (p *fakePlayer) ShowText(text string, d time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = append(p.texts, textCall{Text: text, Duration: d})
	return nil
}

// Texts returns every ShowText so far.
func (p *fakePlayer) Texts() []textCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]textCall(nil), p.texts...)
}

func (p *fakePlayer) Position() (string, time.Duration, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.position.Path, p.position.Offset, p.posErr
}

func (p *fakePlayer) Events() <-chan player.Event { return p.events }

// Loads returns every load, in order.
func (p *fakePlayer) Loads() []loadCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]loadCall, len(p.loads))
	copy(out, p.loads)
	return out
}

// LastLoad returns the most recent load.
func (p *fakePlayer) LastLoad(t *testing.T) loadCall {
	t.Helper()
	loads := p.Loads()
	if len(loads) == 0 {
		t.Fatal("the station never loaded anything")
	}
	return loads[len(loads)-1]
}

// Standbys is how many times the card has been shown.
func (p *fakePlayer) Standbys() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.standbys
}

// SetPosition makes the player report something other than what it last loaded,
// which is how a clock jump or a stuck mpv is simulated.
func (p *fakePlayer) SetPosition(path string, offset time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.position = loadCall{Path: path, Offset: offset}
}

// fakeClock is a wall clock the test moves by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testChannels is a station with two clips on the tuned channel and one empty
// channel, which is enough to cover every case the station has.
//
// The two durations add up to 31 minutes on purpose. A cycle that divided three
// hours evenly would put the clock jump test back at the same offset in the
// same item, and the station would be right not to reload.
func testChannels() []schedule.Channel {
	return []schedule.Channel{
		{
			ID: "clips", Number: 5, Name: "Test Clips",
			Items: []schedule.Item{
				{ID: "a", Title: "Clip A", Path: "/lib/a.mp4", Duration: 10 * time.Minute},
				{ID: "b", Title: "Clip B", Path: "/lib/b.mp4", Duration: 21 * time.Minute},
			},
		},
		{ID: "empty", Number: 9, Name: "Nothing At All"},
	}
}

// newTestStation builds a station over the fake player and clock.
func newTestStation(t *testing.T, p player.Player, clock *fakeClock, start string, channels []schedule.Channel) *station {
	t.Helper()
	s, err := newStation(stationOptions{
		Player:       p,
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return channels, nil },
		StartChannel: start,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	return s
}

// noon is a fixed instant well inside a broadcast day.
func noon() time.Time {
	return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
}

// wantSlot returns what the schedule says is on, so tests assert against the
// same pure function the station calls rather than against a hard coded answer.
func wantSlot(t *testing.T, ch schedule.Channel, at time.Time) schedule.Slot {
	t.Helper()
	slot, ok := schedule.At(ch, at, schedule.NewClock(time.UTC))
	if !ok {
		t.Fatalf("the schedule has nothing on channel %s at %s", ch.ID, at)
	}
	return slot
}

// TestStartupShowsStandbyThenLoadsTheSchedule is the boot sequence: the card
// goes up first so the television is never black, then the current item plays
// from the offset the schedule computed.
func TestStartupShowsStandbyThenLoadsTheSchedule(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()

	waitFor(t, func() bool { return len(p.Loads()) > 0 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}

	if p.Standbys() != 1 {
		t.Errorf("the card was shown %d times at startup, want once", p.Standbys())
	}
	want := wantSlot(t, channels[0], noon())
	got := p.LastLoad(t)
	if got.Path != want.Item.Path || got.Offset != want.Offset {
		t.Errorf("loaded %s at %s, want %s at %s", got.Path, got.Offset, want.Item.Path, want.Offset)
	}
}

// TestStartChannelSelectsByID checks the flag, and the default that a bare
// serve tunes the lowest numbered channel.
func TestStartChannelSelectsByID(t *testing.T) {
	channels := testChannels()
	clock := &fakeClock{now: noon()}

	s := newTestStation(t, newFakePlayer(), clock, "empty", channels)
	if s.tuned != "empty" {
		t.Errorf("tuned %q, want empty", s.tuned)
	}

	s = newTestStation(t, newFakePlayer(), clock, "", channels)
	if s.tuned != "clips" {
		t.Errorf("tuned %q with no start channel, want the lowest numbered channel", s.tuned)
	}

	_, err := newStation(stationOptions{
		Player:       newFakePlayer(),
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return channels, nil },
		StartChannel: "nosuch",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err == nil {
		t.Error("expected an unknown start channel to be refused")
	}
}

// TestEOFAsksTheScheduleAgain is the rule that keeps playback honest: the next
// item comes from the schedule at the current time, never from a playlist.
func TestEOFAsksTheScheduleAgain(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	first := p.LastLoad(t)

	// The clock moves to the end of that airing, which is when mpv would
	// report eof.
	slot := wantSlot(t, channels[0], noon())
	clock.Advance(slot.End.Sub(noon()))
	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonEOF, Path: first.Path})

	want := wantSlot(t, channels[0], clock.Now())
	got := p.LastLoad(t)
	if got.Path != want.Item.Path {
		t.Errorf("after eof the station loaded %s, want the schedule's answer %s", got.Path, want.Item.Path)
	}
	if got.Offset != want.Offset {
		t.Errorf("after eof the offset is %s, want %s", got.Offset, want.Offset)
	}
	if got.Offset != 0 {
		t.Errorf("a boundary load starts at %s, want the start of the item", got.Offset)
	}
}

// TestErrorExcludesTheItemForTheDay covers a corrupt or missing file: it is
// dropped from today's order and the station plays something else.
func TestErrorExcludesTheItemForTheDay(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	broken := p.LastLoad(t).Path

	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: broken})

	if got := p.LastLoad(t).Path; got == broken {
		t.Fatalf("the station reloaded %s after it failed", got)
	}
	if !s.excluded["clips"][itemIDForPath(t, channels[0], broken)] {
		t.Errorf("%s was not excluded", broken)
	}

	// The exclusion has to hold for the rest of the day, not just for one
	// lookup, so a later boundary must not bring it back.
	clock.Advance(90 * time.Minute)
	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonEOF, Path: p.LastLoad(t).Path})
	if got := p.LastLoad(t).Path; got == broken {
		t.Errorf("the excluded item %s came back later in the day", got)
	}
}

// failLast reports the item the station last loaded as unplayable, the way mpv
// does: an error end-file, after which mpv sits idle on nothing.
func failLast(t *testing.T, s *station, p *fakePlayer) {
	t.Helper()
	path := p.LastLoad(t).Path
	p.SetPosition("", 0)
	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: path})
}

// TestEveryItemExcludedShowsStandby is the end of the road for a channel whose
// files are all unplayable: the card, and a service that stays up. The second
// failure is first blamed on the player, so the files are only believed bad
// once they still fail at the next reconcile tick.
func TestEveryItemExcludedShowsStandby(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	failLast(t, s, p)
	failLast(t, s, p)
	s.reconcile()

	for range len(channels[0].Items) {
		if p.Standbys() > 0 {
			break
		}
		failLast(t, s, p)
	}

	if p.Standbys() == 0 {
		t.Fatal("a channel of unplayable files never reached the card")
	}
	if got, want := len(s.excluded["clips"]), len(channels[0].Items); got != want {
		t.Errorf("%d items excluded, want all %d", got, want)
	}
}

// TestFailuresBeforeAnythingPlaysAreBlamedOnThePlayer is a cold boot where mpv
// came up before the sound card and refused every file. Believing it would
// exclude the whole channel for the day and leave the television dark.
func TestFailuresBeforeAnythingPlaysAreBlamedOnThePlayer(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	first := p.LastLoad(t).Path
	failLast(t, s, p)
	if !s.excluded["clips"][itemIDForPath(t, channels[0], first)] {
		t.Fatal("a single failure did not exclude the item")
	}

	failLast(t, s, p)
	loads := len(p.Loads())
	for range 5 {
		s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: p.LastLoad(t).Path})
	}
	if got := len(s.excluded["clips"]); got != 0 {
		t.Errorf("%d items still excluded after the streak was blamed on the player", got)
	}
	if got := len(p.Loads()); got != loads {
		t.Errorf("the station loaded %d more times while waiting, want none", got-loads)
	}

	// The sound card is ready by the next tick and the scheduled item plays,
	// the one that failed first included.
	s.reconcile()
	want := wantSlot(t, channels[0], clock.Now())
	if got := p.LastLoad(t); got.Path != want.Item.Path {
		t.Errorf("the retry loaded %s, want the scheduled %s", got.Path, want.Item.Path)
	}

	// The item plays out, which proves the player works, so the next lone
	// failure is a file's fault again and is excluded at once.
	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonEOF, Path: p.LastLoad(t).Path})
	broken := p.LastLoad(t).Path
	failLast(t, s, p)
	if !s.excluded["clips"][itemIDForPath(t, channels[0], broken)] {
		t.Error("a failure after healthy playback was not excluded")
	}
	if got := p.LastLoad(t).Path; got == broken {
		t.Errorf("reloaded the broken item %s", got)
	}
}

// TestStandbyFailureDoesNotSpin is mpv refusing the card itself. Each refusal
// used to show the card again at once, fifty times a second, until the day
// rolled over.
func TestStandbyFailureDoesNotSpin(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "empty", testChannels())

	s.play()
	shown := p.Standbys()
	for range 20 {
		s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: "/run/channel3/standby.png"})
	}
	if got := p.Standbys(); got != shown {
		t.Fatalf("the card was shown %d more times after failing, want none until the next tick", got-shown)
	}

	s.reconcile()
	if got := p.Standbys(); got != shown+1 {
		t.Errorf("the next tick showed the card %d more times, want exactly one retry", got-shown)
	}
}

// TestChannelWithNoItemsShowsStandby is a configured but empty channel, which
// is a normal state before the first ingest.
func TestChannelWithNoItemsShowsStandby(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "empty", testChannels())

	s.play()

	if len(p.Loads()) != 0 {
		t.Errorf("the station loaded %v from an empty channel", p.Loads())
	}
	if p.Standbys() == 0 {
		t.Error("an empty channel did not show the card")
	}
}

// TestRestartedReloadsTheCurrentItem checks the station re-seeks after mpv is
// replaced, rather than waiting for the next boundary.
func TestRestartedReloadsTheCurrentItem(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	clock.Advance(2 * time.Minute)
	s.handle(player.Event{Kind: player.Restarted})

	want := wantSlot(t, channels[0], clock.Now())
	got := p.LastLoad(t)
	if got.Path != want.Item.Path || got.Offset != want.Offset {
		t.Errorf("after a restart the station loaded %s at %s, want %s at %s",
			got.Path, got.Offset, want.Item.Path, want.Offset)
	}
}

// TestReconcileCorrectsAClockJump is the NTP step: the clock moves three hours
// between ticks and the next tick puts the schedule back in charge.
func TestReconcileCorrectsAClockJump(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	loadsBefore := len(p.Loads())

	clock.Advance(3 * time.Hour)
	s.reconcile()

	if len(p.Loads()) != loadsBefore+1 {
		t.Fatalf("the station loaded %d times, want one reload after the jump", len(p.Loads())-loadsBefore)
	}
	want := wantSlot(t, channels[0], clock.Now())
	got := p.LastLoad(t)
	if got.Path != want.Item.Path || got.Offset != want.Offset {
		t.Errorf("after the jump the station loaded %s at %s, want %s at %s",
			got.Path, got.Offset, want.Item.Path, want.Offset)
	}
}

// TestReconcileLeavesHealthyPlaybackAlone checks the tolerance: a load takes a
// moment and mpv rounds its position, and neither is a reason to seek.
func TestReconcileLeavesHealthyPlaybackAlone(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", testChannels())

	s.play()
	loads := len(p.Loads())

	// Two seconds of drift, inside the tolerance.
	clock.Advance(2 * time.Second)
	s.reconcile()

	if len(p.Loads()) != loads {
		t.Errorf("the station reloaded on %s of drift", 2*time.Second)
	}
}

// TestReconcileReloadsWhenMPVIsOnTheWrongItem covers a dropped end-file event.
func TestReconcileReloadsWhenMPVIsOnTheWrongItem(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	loads := len(p.Loads())
	p.SetPosition("/lib/something-else.mp4", 0)

	s.reconcile()

	if len(p.Loads()) != loads+1 {
		t.Fatal("the station did not reload when mpv was on the wrong item")
	}
	want := wantSlot(t, channels[0], clock.Now())
	if got := p.LastLoad(t); got.Path != want.Item.Path {
		t.Errorf("reloaded %s, want %s", got.Path, want.Item.Path)
	}
}

// TestRolloverRescansAndForgivesExclusions is the 04:00 boundary: a new day's
// order, a fresh library scan, and yesterday's failures forgotten.
func TestRolloverRescansAndForgivesExclusions(t *testing.T) {
	p := newFakePlayer()
	// Ten minutes before the rollover.
	clock := &fakeClock{now: time.Date(2026, 9, 23, 3, 50, 0, 0, time.UTC)}
	channels := testChannels()

	var scans int
	s, err := newStation(stationOptions{
		Player: p,
		Clock:  schedule.NewClock(time.UTC),
		Now:    clock.Now,
		Reload: func() ([]schedule.Channel, error) {
			scans++
			return channels, nil
		},
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	if scans != 1 {
		t.Fatalf("the library was scanned %d times at startup, want once", scans)
	}

	s.play()
	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: p.LastLoad(t).Path})
	if len(s.excluded["clips"]) != 1 {
		t.Fatalf("expected one exclusion before the rollover, got %d", len(s.excluded["clips"]))
	}

	clock.Advance(20 * time.Minute)
	loads := len(p.Loads())
	s.reconcile()

	if scans != 2 {
		t.Errorf("the library was scanned %d times, want a rescan at the rollover", scans)
	}
	if len(s.excluded["clips"]) != 0 {
		t.Errorf("%d exclusions survived the rollover, want none", len(s.excluded["clips"]))
	}
	if len(p.Loads()) != loads+1 {
		t.Error("the rollover did not reload")
	}
	want := wantSlot(t, channels[0], clock.Now())
	if got := p.LastLoad(t); got.Path != want.Item.Path || got.Offset != want.Offset {
		t.Errorf("after the rollover the station loaded %s at %s, want %s at %s",
			got.Path, got.Offset, want.Item.Path, want.Offset)
	}
}

// TestRolloverKeepsPlayingWhenTheRescanFails checks the failure the Pi will
// actually see, a library on a disk that is briefly unreadable. The broadcast
// continues on the channels already in memory.
func TestRolloverKeepsPlayingWhenTheRescanFails(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: time.Date(2026, 9, 23, 3, 50, 0, 0, time.UTC)}
	channels := testChannels()

	var scans int
	s, err := newStation(stationOptions{
		Player: p,
		Clock:  schedule.NewClock(time.UTC),
		Now:    clock.Now,
		Reload: func() ([]schedule.Channel, error) {
			scans++
			if scans > 1 {
				return nil, errRescanFailed
			}
			return channels, nil
		},
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}

	clock.Advance(20 * time.Minute)
	s.reconcile()

	if len(p.Loads()) == 0 {
		t.Fatal("the station stopped playing when the rescan failed")
	}
	want := wantSlot(t, channels[0], clock.Now())
	if got := p.LastLoad(t); got.Path != want.Item.Path {
		t.Errorf("loaded %s, want %s from the channels already in memory", got.Path, want.Item.Path)
	}
}

// TestPlayerStoppingEndsTheRun checks the station does not spin on a closed
// event channel.
func TestPlayerStoppingEndsTheRun(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", testChannels())

	close(p.events)
	err := s.run(t.Context())
	if err == nil {
		t.Fatal("expected an error when the player stopped")
	}
}

// keyChannels is a station with three numbered channels, all playable. The
// numbers matter: 1 is a channel in its own right and also the start of 12, so
// keying 1 is the case the tuner has to wait on.
func keyChannels() []schedule.Channel {
	return []schedule.Channel{
		{
			ID: "kids", Number: 1, Name: "Kids",
			Items: []schedule.Item{{ID: "k1", Title: "Kids One", Path: "/lib/k1.mp4", Duration: 12 * time.Minute}},
		},
		{
			ID: "clips", Number: 5, Name: "Test Clips",
			Items: []schedule.Item{
				{ID: "a", Title: "Clip A", Path: "/lib/a.mp4", Duration: 10 * time.Minute},
				{ID: "b", Title: "Clip B", Path: "/lib/b.mp4", Duration: 21 * time.Minute},
			},
		},
		{
			ID: "docs", Number: 12, Name: "Documentaries",
			Items: []schedule.Item{{ID: "d1", Title: "Doc One", Path: "/lib/d1.mp4", Duration: 25 * time.Minute}},
		},
	}
}

// runKeyStation starts a station over the given key stream and returns a stop
// function. It is the whole loop, so the timer that commits a half typed
// channel number is the real one.
func runKeyStation(t *testing.T, p player.Player, clock *fakeClock, start string, channels []schedule.Channel, keys <-chan input.Key, log *slog.Logger) (*station, func()) {
	t.Helper()
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s, err := newStation(stationOptions{
		Player:       p,
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return channels, nil },
		StartChannel: start,
		Logger:       log,
		Interval:     time.Hour,
		Keys:         keys,
		DigitTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()

	return s, func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}
}

// TestChannelUpTunesTheNextChannel is the remote's main button: one press, one
// load, at the offset the new channel is already up to.
func TestChannelUpTunesTheNextChannel(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	keys := make(chan input.Key, 1)

	_, stop := runKeyStation(t, p, clock, "clips", channels, keys, nil)
	defer stop()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })

	keys <- input.Key{Action: input.ChannelUp}
	waitFor(t, func() bool { return len(p.Loads()) == 2 })

	// Nothing else may load: a channel change is one load, not a load per
	// channel it passed on the way.
	if got := len(p.Loads()); got != 2 {
		t.Fatalf("the station loaded %d times, want the startup load and one more", got)
	}
	want := wantSlot(t, channels[2], clock.Now())
	got := p.LastLoad(t)
	if got.Path != want.Item.Path || got.Offset != want.Offset {
		t.Errorf("after channel up the station loaded %s at %s, want %s at %s",
			got.Path, got.Offset, want.Item.Path, want.Offset)
	}
}

// TestChannelDownWraps checks the other direction and the wrap, from the air
// rather than from the tuner's unit tests.
func TestChannelDownWraps(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	keys := make(chan input.Key, 1)

	_, stop := runKeyStation(t, p, clock, "kids", channels, keys, nil)
	defer stop()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })

	keys <- input.Key{Action: input.ChannelDown}
	waitFor(t, func() bool { return len(p.Loads()) == 2 })

	want := wantSlot(t, channels[2], clock.Now())
	if got := p.LastLoad(t); got.Path != want.Item.Path {
		t.Errorf("channel down from the lowest number loaded %s, want the highest channel's %s",
			got.Path, want.Item.Path)
	}
}

// TestDigitsTuneAfterTheTimeout is keying a channel number that could still
// grow: nothing happens until the tuner's deadline passes, and then the typed
// channel comes on.
func TestDigitsTuneAfterTheTimeout(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	keys := make(chan input.Key, 1)

	_, stop := runKeyStation(t, p, clock, "clips", channels, keys, nil)
	defer stop()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })

	keys <- input.Key{Action: input.Digit, Digit: 1}
	waitFor(t, func() bool { return len(p.Loads()) == 2 })

	want := wantSlot(t, channels[0], clock.Now())
	got := p.LastLoad(t)
	if got.Path != want.Item.Path || got.Offset != want.Offset {
		t.Errorf("after keying 1 the station loaded %s at %s, want channel 1's %s at %s",
			got.Path, got.Offset, want.Item.Path, want.Offset)
	}
}

// TestTwoDigitsTuneAtOnce is the other half of the same rule: a number that
// cannot grow any further does not wait for the timer.
func TestTwoDigitsTuneAtOnce(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	keys := make(chan input.Key, 2)

	station, stop := runKeyStation(t, p, clock, "clips", channels, keys, nil)
	defer stop()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })

	keys <- input.Key{Action: input.Digit, Digit: 1}
	keys <- input.Key{Action: input.Digit, Digit: 2}
	waitFor(t, func() bool { return len(p.Loads()) == 2 })

	want := wantSlot(t, channels[2], clock.Now())
	if got := p.LastLoad(t); got.Path != want.Item.Path {
		t.Errorf("keying 12 loaded %s, want channel 12's %s", got.Path, want.Item.Path)
	}
	if got := station.Tuned(); got != "docs" {
		t.Errorf("the station is tuned to %q, want docs", got)
	}
}

// TestUnknownChannelNumberChangesNothing is keying a number nobody broadcasts
// on: the picture does not change, nothing is loaded, and the log says why,
// because that is the only place a press that did nothing can be explained.
func TestUnknownChannelNumberChangesNothing(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	keys := make(chan input.Key, 1)
	logged := &syncBuffer{}

	_, stop := runKeyStation(t, p, clock, "clips", keyChannels(), keys,
		slog.New(slog.NewTextHandler(logged, nil)))

	waitFor(t, func() bool { return len(p.Loads()) == 1 })
	before := p.LastLoad(t)

	keys <- input.Key{Action: input.Digit, Digit: 9}
	// There is nothing to wait for, so the test waits out the digit timeout
	// and checks nothing happened.
	time.Sleep(60 * time.Millisecond)

	if got := len(p.Loads()); got != 1 {
		t.Errorf("keying an unused channel number loaded %d times, want none", got-1)
	}
	if got := p.LastLoad(t); got != before {
		t.Errorf("playback changed to %v, want it left alone", got)
	}

	// The station is stopped before the log is read, so the buffer is not
	// being written while the test looks at it.
	stop()
	if got := logged.String(); !strings.Contains(got, "no channel has that number") || !strings.Contains(got, "keyed=9") {
		t.Errorf("the log does not say the number was ignored:\n%s", got)
	}
}

// syncBuffer is a log sink a test can read once the station has stopped.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestTelevisionKeysReachCEC checks power and volume go to the television and
// never to the schedule.
func TestTelevisionKeysReachCEC(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	keys := make(chan input.Key, 4)
	tv := &fakeCEC{}

	s, err := newStation(stationOptions{
		Player:       p,
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return channels, nil },
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
		Keys:         keys,
		CEC:          tv,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })
	// One at a time: the queue holds a single key on purpose, so a test that
	// pushed all four at once would be asserting a race rather than the order.
	for i, k := range []input.Key{
		{Action: input.Power},
		{Action: input.VolumeUp},
		{Action: input.VolumeDown},
		{Action: input.Mute},
	} {
		keys <- k
		waitFor(t, func() bool { return len(tv.Calls()) == i+1 })
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}

	want := []string{"power", "volume-up", "volume-down", "mute"}
	got := tv.Calls()
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d is %q, want %q", i, got[i], want[i])
		}
	}
	if len(p.Loads()) != 1 {
		t.Errorf("a television key loaded %d items, want none", len(p.Loads())-1)
	}
}

// TestTunedReportsTheChannelOnAir is what the Phase 7 API reads, and it is
// read from another goroutine while the loop is running, which is the race the
// API would otherwise hit.
func TestTunedReportsTheChannelOnAir(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	keys := make(chan input.Key, 1)

	station, stop := runKeyStation(t, p, clock, "clips", keyChannels(), keys, nil)
	defer stop()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })
	if got := station.Tuned(); got != "clips" {
		t.Fatalf("tuned to %q at startup, want clips", got)
	}

	keys <- input.Key{Action: input.ChannelUp}
	waitFor(t, func() bool { return station.Tuned() == "docs" })
}

// fakeCEC records what the television was asked to do.
//
// A gate holds every call until the test opens it, which is how a set that
// takes seconds to answer, or never answers at all, is simulated.
type fakeCEC struct {
	mu      sync.Mutex
	calls   []string
	gate    chan struct{}
	started chan string
}

func (c *fakeCEC) record(name string) error {
	c.mu.Lock()
	c.calls = append(c.calls, name)
	gate, started := c.gate, c.started
	c.mu.Unlock()

	if started != nil {
		select {
		case started <- name:
		default:
		}
	}
	if gate != nil {
		<-gate
	}
	return nil
}

func (c *fakeCEC) Power() error      { return c.record("power") }
func (c *fakeCEC) VolumeUp() error   { return c.record("volume-up") }
func (c *fakeCEC) VolumeDown() error { return c.record("volume-down") }
func (c *fakeCEC) Mute() error       { return c.record("mute") }

func (c *fakeCEC) Calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.calls))
	copy(out, c.calls)
	return out
}

// TestASlowTelevisionDoesNotStallTheBroadcast is why the television is spoken
// to from its own goroutine: cec-ctl can sit for seconds against a set that is
// off, and the next item must not wait behind it.
func TestASlowTelevisionDoesNotStallTheBroadcast(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	keys := make(chan input.Key, 1)
	tv := &fakeCEC{gate: make(chan struct{}), started: make(chan string, 4)}

	s, err := newStation(stationOptions{
		Player:       p,
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return channels, nil },
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
		Keys:         keys,
		CEC:          tv,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })

	// The power command is now stuck against a television that never answers.
	keys <- input.Key{Action: input.Power}
	select {
	case <-tv.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the power key never reached the television")
	}

	// The broadcast carries on regardless.
	first := p.LastLoad(t)
	clock.Advance(11 * time.Minute)
	p.events <- player.Event{Kind: player.EndFile, Reason: player.ReasonEOF, Path: first.Path}
	waitFor(t, func() bool { return len(p.Loads()) == 2 })

	close(tv.gate)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestRepeatedTelevisionKeysCollapse is the one slot queue: holding volume up
// while the set is slow must not build a backlog of commands to send later.
func TestRepeatedTelevisionKeysCollapse(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	keys := make(chan input.Key, 3)
	tv := &fakeCEC{gate: make(chan struct{}), started: make(chan string, 4)}

	s, err := newStation(stationOptions{
		Player:       p,
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return channels, nil },
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
		Keys:         keys,
		CEC:          tv,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()

	waitFor(t, func() bool { return len(p.Loads()) == 1 })

	for range 3 {
		keys <- input.Key{Action: input.VolumeUp}
	}
	// The first press is stuck at the television and the other two have been
	// taken off the key stream by the loop.
	select {
	case <-tv.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first volume key never reached the television")
	}
	waitFor(t, func() bool { return len(keys) == 0 })

	close(tv.gate)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := len(tv.Calls()); got > 2 {
		t.Errorf("three presses became %d commands, want the queue to collapse them to at most two", got)
	} else if got < 1 {
		t.Error("three presses reached the television as nothing at all")
	}
}

// itemIDForPath finds the item id behind a path in a channel.
func itemIDForPath(t *testing.T, ch schedule.Channel, path string) string {
	t.Helper()
	for _, item := range ch.Items {
		if item.Path == path {
			return item.ID
		}
	}
	t.Fatalf("channel %s has no item at %s", ch.ID, path)
	return ""
}

// waitFor polls until cond is true or the test's patience runs out.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the station")
}

// errRescanFailed stands in for a library that cannot be read.
var errRescanFailed = errors.New("library is unreadable")

// TestUnresolvedErrorDoesNotSpin is the throttle. An error event whose path
// names nothing on the channel excludes nothing, so reloading would hand mpv
// the same failing file again and again as fast as it could report the failure.
// The station waits for the next reconcile tick instead.
func TestUnresolvedErrorDoesNotSpin(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", testChannels())
	s.interval = 30 * time.Second

	s.play()
	if len(p.Loads()) != 1 {
		t.Fatalf("the station loaded %d times at startup, want once", len(p.Loads()))
	}

	// mpv could not resolve the path behind the entry, so the event carries
	// none. Several in a row must not each cause a load.
	for range 5 {
		s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: ""})
	}
	if got := len(p.Loads()); got > 2 {
		t.Fatalf("the station loaded %d times while stalled, want at most one extra", got)
	}
	stalledLoads := len(p.Loads())

	// The next reconcile tick is allowed to try once more.
	clock.Advance(s.interval + time.Second)
	s.reconcile()
	if got := len(p.Loads()); got != stalledLoads+1 {
		t.Fatalf("the reconcile tick loaded %d times, want exactly one retry", got-stalledLoads)
	}

	// And the stall applies again after that retry fails.
	retried := len(p.Loads())
	for range 5 {
		s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: ""})
	}
	if got := len(p.Loads()); got > retried+1 {
		t.Errorf("the station loaded %d more times after the retry, want at most one", got-retried)
	}
}

// TestResolvedErrorStillReloadsAtOnce guards the throttle from being too eager:
// when the failing item was excluded there is something else to play, and the
// station must not wait half a minute to play it.
func TestResolvedErrorStillReloadsAtOnce(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	broken := p.LastLoad(t).Path
	loads := len(p.Loads())

	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: broken})

	if len(p.Loads()) != loads+1 {
		t.Fatal("an excluded item did not lead to an immediate reload")
	}
	if got := p.LastLoad(t).Path; got == broken {
		t.Errorf("reloaded the broken item %s", got)
	}
}

// TestRolloverRetriesAfterAFailedRescan checks that a library which is briefly
// unreadable at 04:00 is rescanned on the next tick rather than skipped until
// the following day.
func TestRolloverRetriesAfterAFailedRescan(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: time.Date(2026, 9, 23, 3, 50, 0, 0, time.UTC)}
	before := testChannels()
	after := []schedule.Channel{
		{
			ID: "clips", Number: 5, Name: "Test Clips",
			Items: []schedule.Item{
				{ID: "c", Title: "Clip C", Path: "/lib/c.mp4", Duration: 17 * time.Minute},
			},
		},
	}

	var scans int
	s, err := newStation(stationOptions{
		Player: p,
		Clock:  schedule.NewClock(time.UTC),
		Now:    clock.Now,
		Reload: func() ([]schedule.Channel, error) {
			scans++
			switch scans {
			case 1:
				return before, nil
			case 2:
				return nil, errRescanFailed
			default:
				return after, nil
			}
		},
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	startedOn := s.day

	// First tick after 04:00: the rescan fails and the day is not advanced.
	clock.Advance(20 * time.Minute)
	s.reconcile()
	if scans != 2 {
		t.Fatalf("the library was scanned %d times, want a first attempt at the rollover", scans)
	}
	if !s.day.Equal(startedOn) {
		t.Error("the broadcast day advanced despite the rescan failing")
	}
	if got := p.LastLoad(t).Path; got != "/lib/a.mp4" && got != "/lib/b.mp4" {
		t.Errorf("the station loaded %s, want one of the channels already in memory", got)
	}

	// Second tick: the library is readable and the new channels take effect.
	clock.Advance(time.Minute)
	s.reconcile()
	if scans != 3 {
		t.Fatalf("the library was scanned %d times, want a retry on the next tick", scans)
	}
	if s.day.Equal(startedOn) {
		t.Error("the broadcast day did not advance after a successful rescan")
	}
	if got := p.LastLoad(t).Path; got != "/lib/c.mp4" {
		t.Errorf("the station loaded %s, want the rescanned channel's item", got)
	}

	// A third tick must not rescan again now that the day has advanced.
	clock.Advance(time.Minute)
	s.reconcile()
	if scans != 3 {
		t.Errorf("the library was scanned %d times, want no further rescan within the day", scans)
	}
}

// TestChannelsIsSafeWhileTheListIsReplaced is the Phase 7 API reading the
// station from the HTTP server's goroutine while the broadcast loop swaps the
// whole channel list at the 04:00 rollover.
//
// It is a race detector test: run it with -race, which is what make test does.
func TestChannelsIsSafeWhileTheListIsReplaced(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: time.Date(2026, 9, 23, 3, 50, 0, 0, time.UTC)}

	// Every rescan returns a fresh slice, as loadStation does, so a reader
	// holding the old one cannot be saved by the two sharing memory.
	var scans atomic.Int64
	s, err := newStation(stationOptions{
		Player: p,
		Clock:  schedule.NewClock(time.UTC),
		Now:    clock.Now,
		Reload: func() ([]schedule.Channel, error) {
			n := scans.Add(1)
			channels := testChannels()
			channels[0].Name = fmt.Sprintf("Test Clips %d", n)
			return channels, nil
		},
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}

	stop := make(chan struct{})
	readers := sync.WaitGroup{}
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, ch := range s.Channels() {
					// Read every field the API reads, so the detector sees the
					// reader touch what the rollover replaces.
					_ = ch.ID + ch.Name + strconv.Itoa(ch.Number) + strconv.Itoa(len(ch.Items))
				}
				_ = s.Tuned()
			}
		}()
	}

	// Roll the day over repeatedly. Each one replaces the list under the lock
	// while the readers above are walking it.
	for i := range 20 {
		clock.Advance(24 * time.Hour)
		s.reconcile()
		if i == 0 && scans.Load() < 2 {
			t.Error("the first advance did not roll the day over")
		}
	}

	close(stop)
	readers.Wait()

	if got := len(s.Channels()); got != len(testChannels()) {
		t.Errorf("the station reports %d channels after the rollovers, want %d", got, len(testChannels()))
	}
}

// TestZeroChannelsStartsOnStandby is a box whose first ingest has not happened,
// or whose channel directory is empty. The service comes up, the television
// shows the card, and nothing is loaded.
func TestZeroChannelsStartsOnStandby(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	logs := &syncBuffer{}

	s, err := newStation(stationOptions{
		Player:   p,
		Clock:    schedule.NewClock(time.UTC),
		Now:      clock.Now,
		Reload:   func() ([]schedule.Channel, error) { return nil, nil },
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
		Interval: time.Hour,
	})
	if err != nil {
		t.Fatalf("a station with no channels refused to start: %v", err)
	}
	if s.Tuned() != "" {
		t.Errorf("tuned %q, want nothing tuned", s.Tuned())
	}
	if got := s.Channels(); len(got) != 0 {
		t.Errorf("the station reports %d channels, want none", len(got))
	}
	if !strings.Contains(logs.String(), "no channels are configured") {
		t.Errorf("the empty station was not logged:\n%s", logs.String())
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()

	waitFor(t, func() bool { return p.Standbys() > 0 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := p.Loads(); len(got) != 0 {
		t.Errorf("a station with no channels loaded %v", got)
	}
}

// TestZeroChannelsIgnoresTheStartChannel keeps --start-channel from turning a
// box that has not been ingested into a box that will not boot.
func TestZeroChannelsIgnoresTheStartChannel(t *testing.T) {
	clock := &fakeClock{now: noon()}
	logs := &syncBuffer{}

	s, err := newStation(stationOptions{
		Player:       newFakePlayer(),
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return nil, nil },
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(logs, nil)),
		Interval:     time.Hour,
	})
	if err != nil {
		t.Fatalf("a named start channel on an empty station refused to start: %v", err)
	}
	if s.Tuned() != "" {
		t.Errorf("tuned %q, want nothing tuned", s.Tuned())
	}
	if !strings.Contains(logs.String(), "the start channel is not configured yet") {
		t.Errorf("the dropped start channel was not logged:\n%s", logs.String())
	}
}

// TestKeysOnAnEmptyStationDoNothing is somebody pressing buttons at a
// television that has nothing to show. It must not panic and it must not load.
func TestKeysOnAnEmptyStationDoNothing(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}

	s, err := newStation(stationOptions{
		Player:   p,
		Clock:    schedule.NewClock(time.UTC),
		Now:      clock.Now,
		Reload:   func() ([]schedule.Channel, error) { return nil, nil },
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}

	for _, k := range []input.Key{
		{Action: input.ChannelUp},
		{Action: input.ChannelDown},
		{Action: input.Digit, Digit: 5},
	} {
		s.key(k)
	}
	s.expireDigits()

	if got := p.Loads(); len(got) != 0 {
		t.Errorf("keys on an empty station loaded %v", got)
	}
	if s.Tuned() != "" {
		t.Errorf("keys on an empty station tuned %q", s.Tuned())
	}
}

// TestRolloverAdoptsTheFirstChannels is the box that was ingested while it was
// broadcasting nothing: the 04:00 rescan finds channels and the lowest numbered
// one goes on air.
func TestRolloverAdoptsTheFirstChannels(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: time.Date(2026, 9, 23, 3, 50, 0, 0, time.UTC)}
	channels := testChannels()

	var scans int
	s, err := newStation(stationOptions{
		Player: p,
		Clock:  schedule.NewClock(time.UTC),
		Now:    clock.Now,
		Reload: func() ([]schedule.Channel, error) {
			scans++
			if scans == 1 {
				return nil, nil
			}
			return channels, nil
		},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	if s.Tuned() != "" {
		t.Fatalf("tuned %q before the rescan, want nothing tuned", s.Tuned())
	}

	clock.Advance(20 * time.Minute)
	s.reconcile()

	if s.Tuned() != "clips" {
		t.Errorf("tuned %q after the rollover, want the lowest numbered channel", s.Tuned())
	}
	want := wantSlot(t, channels[0], clock.Now())
	got := p.LastLoad(t)
	if got.Path != want.Item.Path || got.Offset != want.Offset {
		t.Errorf("loaded %s at %s, want %s at %s", got.Path, got.Offset, want.Item.Path, want.Offset)
	}
}

// TestPlayableChannelsAppliesTheDaysExclusions is the guide agreeing with the
// screen. Channels reports the library as it was loaded; the API reads
// PlayableChannels, which is the order the television is really playing.
func TestPlayableChannelsAppliesTheDaysExclusions(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	s.play()
	broken := p.LastLoad(t).Path
	brokenID := itemIDForPath(t, channels[0], broken)
	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: broken})

	playable := s.PlayableChannels()
	if len(playable) != len(channels) {
		t.Fatalf("PlayableChannels returned %d channels, want %d", len(playable), len(channels))
	}
	for _, item := range playable[0].Items {
		if item.ID == brokenID {
			t.Errorf("PlayableChannels still lists the excluded item %s", brokenID)
		}
	}
	if got, want := len(playable[0].Items), len(channels[0].Items)-1; got != want {
		t.Errorf("PlayableChannels kept %d items, want %d", got, want)
	}

	// Channels is the library as it was loaded and is not filtered, so the two
	// views stay distinguishable.
	loaded := s.Channels()
	if len(loaded[0].Items) != len(channels[0].Items) {
		t.Errorf("Channels reports %d items, want the whole channel's %d",
			len(loaded[0].Items), len(channels[0].Items))
	}

	// The channel the exclusion belongs to is the only one that loses an item.
	if len(playable[1].Items) != len(channels[1].Items) {
		t.Errorf("an exclusion on one channel changed another: %+v", playable[1].Items)
	}
}

// TestPlayableChannelsIsSafeWhileItemsAreExcluded is the API reading the
// station from the HTTP server's goroutines while the broadcast loop drops an
// unplayable item.
//
// It is a race detector test: run it with -race, which is what make test does.
func TestPlayableChannelsIsSafeWhileItemsAreExcluded(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	stop := make(chan struct{})
	readers := sync.WaitGroup{}
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, ch := range s.PlayableChannels() {
					for _, item := range ch.Items {
						_ = item.ID + item.Path
					}
				}
			}
		}()
	}

	// Exclude and forgive repeatedly, which is an error event and a rollover
	// writing the same map the readers above are walking.
	for range 50 {
		s.play()
		s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: p.LastLoad(t).Path})
		s.rollover(s.clock.BroadcastDay(clock.Now()))
	}

	close(stop)
	readers.Wait()

	if got := len(s.PlayableChannels()); got != len(channels) {
		t.Errorf("the station reports %d channels, want %d", got, len(channels))
	}
}

// lastText returns the most recent ShowText, failing the test if there was none.
func lastText(t *testing.T, p *fakePlayer) textCall {
	t.Helper()
	texts := p.Texts()
	if len(texts) == 0 {
		t.Fatal("nothing was shown on screen")
	}
	return texts[len(texts)-1]
}

// TestChannelChangeShowsTheNumber is the television's own channel number, up
// for a moment after every change.
func TestChannelChangeShowsTheNumber(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.play()

	s.key(input.Key{Action: input.ChannelUp})

	if got := lastText(t, p); got.Text != "12" || got.Duration != numberDuration {
		t.Errorf("showed %q for %s, want 12 for %s", got.Text, got.Duration, numberDuration)
	}
}

// TestTypedDigitsShowWhileWaiting is the first digit of a two digit number. The
// tuner waits to see whether a second digit follows, and the screen says so
// rather than doing nothing.
func TestTypedDigitsShowWhileWaiting(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.play()

	s.key(input.Key{Action: input.Digit, Digit: 1})
	waiting := lastText(t, p)
	if waiting.Text != "1-" {
		t.Errorf("showed %q after keying 1, want 1-", waiting.Text)
	}
	if waiting.Duration <= 0 {
		t.Errorf("the half typed number is up for %s, want until the tuner stops waiting", waiting.Duration)
	}

	s.key(input.Key{Action: input.Digit, Digit: 2})
	if got := lastText(t, p); got.Text != "12" {
		t.Errorf("showed %q after keying 12, want 12", got.Text)
	}
}

// TestUnknownNumberIsShownThenDropped is a number nobody broadcasts on. It
// shows, so the press was seen, and the channel stays where it was.
func TestUnknownNumberIsShownThenDropped(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.play()
	loads := len(p.Loads())

	s.key(input.Key{Action: input.Digit, Digit: 9})

	if got := lastText(t, p); got.Text != "9" || got.Duration != numberDuration {
		t.Errorf("showed %q for %s, want 9 for %s", got.Text, got.Duration, numberDuration)
	}
	if got := len(p.Loads()); got != loads {
		t.Errorf("keying an unknown number loaded %d times, want none", got-loads)
	}
}

// TestKeyingTheTunedChannelShowsItsNumber is keying the channel already on,
// which changes nothing but still answers the press.
func TestKeyingTheTunedChannelShowsItsNumber(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.play()

	s.key(input.Key{Action: input.Digit, Digit: 5})

	if got := lastText(t, p); got.Text != "5" {
		t.Errorf("showed %q after keying the tuned channel, want 5", got.Text)
	}
}
