package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

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
	events   chan player.Event
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

// TestEveryItemExcludedShowsStandby is the end of the road for a channel whose
// files are all unplayable: the card, and a service that stays up.
func TestEveryItemExcludedShowsStandby(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := testChannels()
	s := newTestStation(t, p, clock, "clips", channels)

	for range len(channels[0].Items) {
		s.play()
		s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonError, Path: p.LastLoad(t).Path})
	}

	before := p.Standbys()
	s.play()
	if p.Standbys() != before+1 {
		t.Errorf("the card was shown %d times, want one more than %d", p.Standbys(), before)
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
