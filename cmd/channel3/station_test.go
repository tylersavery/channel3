package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/bumper"
	"github.com/tylersavery/channel3/internal/input"
	"github.com/tylersavery/channel3/internal/player"
	"github.com/tylersavery/channel3/internal/schedule"
	"github.com/tylersavery/channel3/internal/settings"
)

// loadCall is one Load the station asked the player for.
type loadCall struct {
	Path   string
	Offset time.Duration
}

// fakePlayer stands in for mpv. No test in this package starts a real one.
type fakePlayer struct {
	mu        sync.Mutex
	loads     []loadCall
	standbys  int
	position  loadCall
	posErr    error
	loadErr   error
	texts     []textCall
	volumes   []int
	mutes     []bool
	gains     []float64
	overlays  []overlayCall
	removed   []int
	screen    image.Point
	screenErr error
	events    chan player.Event

	movieLoads []movieLoad
	paused     bool
	seeks      []time.Duration
	tracks     []trackCall
}

// overlayCall is one ShowOverlay the station made.
type overlayCall struct {
	ID   int
	Size image.Point
	X, Y int
}

// textCall is one ShowText the station made.
type textCall struct {
	Text     string
	Duration time.Duration
	Scale    int
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

func (p *fakePlayer) ShowText(text string, d time.Duration, scale int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = append(p.texts, textCall{Text: text, Duration: d, Scale: scale})
	return nil
}

func (p *fakePlayer) ShowOverlay(id int, img *image.RGBA, x, y int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.overlays = append(p.overlays, overlayCall{ID: id, Size: img.Bounds().Size(), X: x, Y: y})
	return nil
}

func (p *fakePlayer) RemoveOverlay(id int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removed = append(p.removed, id)
	return nil
}

func (p *fakePlayer) ScreenSize() (int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.screenErr != nil {
		return 0, 0, p.screenErr
	}
	if p.screen == (image.Point{}) {
		return 1920, 1080, nil
	}
	return p.screen.X, p.screen.Y, nil
}

func (p *fakePlayer) SetVolume(percent int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volumes = append(p.volumes, percent)
	return nil
}

func (p *fakePlayer) SetMute(muted bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mutes = append(p.mutes, muted)
	return nil
}

func (p *fakePlayer) SetGain(db float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gains = append(p.gains, db)
	return nil
}

// Volumes returns every SetVolume so far.
func (p *fakePlayer) Volumes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.volumes...)
}

// Mutes returns every SetMute so far.
func (p *fakePlayer) Mutes() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.mutes...)
}

// Overlays returns every ShowOverlay so far.
func (p *fakePlayer) Overlays() []overlayCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]overlayCall(nil), p.overlays...)
}

// Removed returns every RemoveOverlay so far.
func (p *fakePlayer) Removed() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.removed...)
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

// movieLoad is one LoadMovie the station made.
type movieLoad struct {
	Path   string
	Offset time.Duration
	Opts   player.MovieOptions
}

// trackCall is one SetTrack the station made.
type trackCall struct {
	Kind player.TrackKind
	ID   int
}

func (p *fakePlayer) LoadMovie(path string, offset time.Duration, o player.MovieOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loadErr != nil {
		return p.loadErr
	}
	p.movieLoads = append(p.movieLoads, movieLoad{Path: path, Offset: offset, Opts: o})
	p.position = loadCall{Path: path, Offset: offset}
	p.paused = false
	return nil
}

func (p *fakePlayer) SetPause(paused bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = paused
	return nil
}

func (p *fakePlayer) Seek(delta time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seeks = append(p.seeks, delta)
	p.position.Offset = max(p.position.Offset+delta, 0)
	return nil
}

func (p *fakePlayer) SetTrack(kind player.TrackKind, id int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tracks = append(p.tracks, trackCall{Kind: kind, ID: id})
	return nil
}

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
		Settings:     tvVolume(),
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
		Settings:     tvVolume(),
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
		Settings:     tvVolume(),
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

	if got := lastText(t, p); got.Text != "12" || got.Duration != settings.Default().ChannelNumber.Duration {
		t.Errorf("showed %q for %s, want 12 for %s", got.Text, got.Duration, settings.Default().ChannelNumber.Duration)
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

	if got := lastText(t, p); got.Text != "9" || got.Duration != settings.Default().ChannelNumber.Duration {
		t.Errorf("showed %q for %s, want 9 for %s", got.Text, got.Duration, settings.Default().ChannelNumber.Duration)
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

// TestChannelNumberCanBeTurnedOff is settings.yaml saying no number: channel
// changes and typed digits both leave the screen alone.
func TestChannelNumberCanBeTurnedOff(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.settings.ChannelNumber.Enabled = false
	s.play()

	s.key(input.Key{Action: input.ChannelUp})
	s.key(input.Key{Action: input.Digit, Digit: 1})
	s.key(input.Key{Action: input.Digit, Digit: 9})

	if texts := p.Texts(); len(texts) != 0 {
		t.Errorf("showed %v with the channel number turned off", texts)
	}
}

// TestChannelNumberDurationComesFromSettings is a longer number set in
// settings.yaml reaching the player.
func TestChannelNumberDurationComesFromSettings(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.settings.ChannelNumber.Duration = 3 * time.Second
	s.play()

	s.key(input.Key{Action: input.ChannelUp})

	if got := lastText(t, p); got.Duration != 3*time.Second {
		t.Errorf("the number was up for %s, want the configured 3s", got.Duration)
	}
}

// cardStation is a key station whose channel 12 has a card and whose other
// channels do not.
func cardStation(t *testing.T, p *fakePlayer, clock *fakeClock) *station {
	t.Helper()
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.loadCards = func() map[string]bumper.Card {
		return map[string]bumper.Card{
			"docs": {Name: "Documentaries", Color: color.RGBA{R: 0x36, G: 0x7C, B: 0x2B, A: 0xff}},
		}
	}
	s.refreshCards()
	s.play()
	return s
}

// TestTuneShowsTheChannelsCard is the bumper going up on a channel change, in
// the lower left of the screen mpv reports, and coming down when its time is up.
func TestTuneShowsTheChannelsCard(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := cardStation(t, p, clock)

	s.key(input.Key{Action: input.ChannelUp})

	overlays := p.Overlays()
	if len(overlays) != 1 {
		t.Fatalf("showed %d overlays tuning to a channel with a card, want 1", len(overlays))
	}
	if got := overlays[0]; got.ID != bumperOverlay || got.X <= 0 || got.Y < 1080/2 {
		t.Errorf("card shown as %+v, want overlay %d in the lower left", got, bumperOverlay)
	}
	if want := clock.Now().Add(settings.Default().Bumper.Duration); !s.bumperUntil.Equal(want) {
		t.Errorf("the card comes down at %s, want %s", s.bumperUntil, want)
	}

	s.hideBumper()
	if removed := p.Removed(); len(removed) != 1 || removed[0] != bumperOverlay {
		t.Errorf("removed overlays %v when the card's time was up, want [%d]", removed, bumperOverlay)
	}
	s.hideBumper()
	if removed := p.Removed(); len(removed) != 1 {
		t.Errorf("a second hide removed again, %v, want nothing with no card up", removed)
	}
}

// TestChannelWithoutACardClearsTheLastOne is tuning away from a card's channel
// to one with none. The old channel's card must not sit over the new channel.
func TestChannelWithoutACardClearsTheLastOne(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := cardStation(t, p, clock)

	s.key(input.Key{Action: input.ChannelUp})
	s.key(input.Key{Action: input.ChannelUp})

	if got := s.Tuned(); got != "kids" {
		t.Fatalf("tuned %q, want kids", got)
	}
	if removed := p.Removed(); len(removed) != 1 {
		t.Errorf("removed overlays %v tuning to a channel with no card, want the old card taken down", removed)
	}
	if len(p.Overlays()) != 1 {
		t.Errorf("showed %d overlays, want only the first channel's card", len(p.Overlays()))
	}
}

func TestBumpersCanBeTurnedOff(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := cardStation(t, p, clock)
	s.settings.Bumper.Enabled = false

	s.key(input.Key{Action: input.ChannelUp})

	if overlays := p.Overlays(); len(overlays) != 0 {
		t.Errorf("showed %v with bumpers turned off", overlays)
	}
}

// TestUnreadableIconShowsTheNameAlone is a broken SVG in the icons directory.
// The card still goes up, without its picture.
func TestUnreadableIconShowsTheNameAlone(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.loadCards = func() map[string]bumper.Card {
		return map[string]bumper.Card{"docs": {Name: "Documentaries", Color: color.RGBA{A: 0xff}, Icon: []byte("<svg><path d=\"M 0 0 Q\"")}}
	}
	s.refreshCards()
	s.play()

	s.key(input.Key{Action: input.ChannelUp})

	if overlays := p.Overlays(); len(overlays) != 1 {
		t.Errorf("showed %d overlays for a card with a broken icon, want the card with its name alone", len(overlays))
	}
}

// TestCardRedrawsForANewScreenSize is the Pi moved from a 1080p monitor to the
// 720p Samsung without a restart: the card must be drawn again at the new size.
func TestCardRedrawsForANewScreenSize(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := cardStation(t, p, clock)

	s.key(input.Key{Action: input.ChannelUp})
	p.mu.Lock()
	p.screen = image.Pt(1366, 768)
	p.mu.Unlock()
	s.key(input.Key{Action: input.ChannelDown})
	s.key(input.Key{Action: input.ChannelUp})

	overlays := p.Overlays()
	if len(overlays) != 2 {
		t.Fatalf("showed %d overlays, want 2", len(overlays))
	}
	if overlays[0].Size == overlays[1].Size {
		t.Errorf("the card is %v on both screens, want it redrawn for 1366x768", overlays[1].Size)
	}
}

// TestNoScreenSizeShowsNoCard is mpv unable to say how big the screen is. The
// tune still happens; only the card is skipped.
func TestNoScreenSizeShowsNoCard(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := cardStation(t, p, clock)
	p.mu.Lock()
	p.screenErr = errors.New("no video output")
	p.mu.Unlock()

	s.key(input.Key{Action: input.ChannelUp})

	if got := s.Tuned(); got != "docs" {
		t.Errorf("tuned %q, want docs even without a card", got)
	}
	if overlays := p.Overlays(); len(overlays) != 0 {
		t.Errorf("showed %v with no screen size", overlays)
	}
}

// tvVolume is settings that send the volume buttons to the television, which
// is what the CEC tests are about.
func tvVolume() *settings.Settings {
	look := settings.Default()
	look.Volume.Control = settings.VolumeOnTV
	return &look
}

// lastVolume returns the most recent SetVolume, failing the test if none.
func lastVolume(t *testing.T, p *fakePlayer) int {
	t.Helper()
	volumes := p.Volumes()
	if len(volumes) == 0 {
		t.Fatal("the volume was never set")
	}
	return volumes[len(volumes)-1]
}

// TestVolumeStopsAtTheCap is a child holding volume up. The level climbs by
// the step to the cap and no further, and the bar still shows on every press.
func TestVolumeStopsAtTheCap(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	look := settings.Default().Volume

	s.key(input.Key{Action: input.VolumeUp})
	if got, want := lastVolume(t, p), look.Start+look.Step; got != want {
		t.Errorf("one press set %d, want %d", got, want)
	}
	for range 20 {
		s.key(input.Key{Action: input.VolumeUp})
	}
	if got := lastVolume(t, p); got != look.Max {
		t.Errorf("twenty more presses set %d, want the cap %d", got, look.Max)
	}
	if got := lastText(t, p); got.Text != "VOL ██████████" {
		t.Errorf("at the cap the bar reads %q, want it full", got.Text)
	}
	if got := len(p.Texts()); got != 21 {
		t.Errorf("the bar showed %d times for 21 presses, want every press answered", got)
	}
}

func TestVolumeStopsAtZero(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())

	for range 30 {
		s.key(input.Key{Action: input.VolumeDown})
	}
	if got := lastVolume(t, p); got != 0 {
		t.Errorf("thirty presses down set %d, want 0", got)
	}
	if got := lastText(t, p); got.Text != "VOL ░░░░░░░░░░" {
		t.Errorf("at zero the bar reads %q, want it empty", got.Text)
	}
}

// TestMuteTogglesAndVolumeUnmutes is mute, then volume up, which unmutes the
// way a television does.
func TestMuteTogglesAndVolumeUnmutes(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())

	s.key(input.Key{Action: input.Mute})
	if mutes := p.Mutes(); len(mutes) == 0 || !mutes[len(mutes)-1] {
		t.Fatalf("mute sent %v, want mute on", mutes)
	}
	if got := lastText(t, p); got.Text != "MUTE" {
		t.Errorf("muting showed %q, want MUTE", got.Text)
	}

	s.key(input.Key{Action: input.VolumeUp})
	if mutes := p.Mutes(); mutes[len(mutes)-1] {
		t.Error("volume up left it muted")
	}
}

// TestRestartReappliesTheVolume is mpv dying and coming back at its own full
// volume. The station must put its level back, or the cap is gone.
func TestRestartReappliesTheVolume(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.key(input.Key{Action: input.VolumeDown})
	before := len(p.Volumes())

	s.handle(player.Event{Kind: player.Restarted})

	volumes := p.Volumes()
	if len(volumes) != before+1 || volumes[len(volumes)-1] != s.volume {
		t.Errorf("after a restart the volumes set were %v, want the station's %d reapplied", volumes[before:], s.volume)
	}
}

func TestStartupSetsTheStartVolume(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	keys := make(chan input.Key)
	_, stop := runKeyStation(t, p, clock, "clips", keyChannels(), keys, nil)
	defer stop()

	waitFor(t, func() bool { return len(p.Volumes()) > 0 })
	if got, want := p.Volumes()[0], settings.Default().Volume.Start; got != want {
		t.Errorf("startup set the volume to %d, want the start level %d", got, want)
	}
}

// TestVolumeOnTheTelevisionLeavesMPVAlone is control: tv. The buttons go to
// CEC and mpv's volume is never touched, not even at startup.
func TestVolumeOnTheTelevisionLeavesMPVAlone(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.settings = *tvVolume()

	s.applyVolume()
	s.key(input.Key{Action: input.VolumeUp})
	s.key(input.Key{Action: input.Mute})

	if volumes, mutes := p.Volumes(), p.Mutes(); len(volumes) != 0 || len(mutes) != 0 {
		t.Errorf("with control: tv the station set volume %v and mute %v, want neither", volumes, mutes)
	}
}

func TestVolumeBar(t *testing.T) {
	cases := []struct {
		level, ceiling int
		muted          bool
		want           string
	}{
		{0, 70, false, "VOL ░░░░░░░░░░"},
		{35, 70, false, "VOL █████░░░░░"},
		{70, 70, false, "VOL ██████████"},
		{5, 70, false, "VOL █░░░░░░░░░"},
		{50, 70, true, "MUTE"},
	}
	for _, tc := range cases {
		if got := volumeBar(tc.level, tc.ceiling, tc.muted); got != tc.want {
			t.Errorf("volumeBar(%d, %d, %v) = %q, want %q", tc.level, tc.ceiling, tc.muted, got, tc.want)
		}
	}
}

// TestRescanAddsItemsWithoutDisturbingTheScreen is a home video uploaded while
// another channel is on. The new item appears in the station's channels, and
// the channel on screen, whose schedule did not change, is not reloaded.
func TestRescanAddsItemsWithoutDisturbingTheScreen(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := keyChannels()
	s := newTestStation(t, p, clock, "clips", channels)
	s.play()
	loads := len(p.Loads())

	home := schedule.Channel{ID: "home", Number: 14, Name: "Home Movies",
		Items: []schedule.Item{{ID: "first", Title: "2025-06-14 14.03", Path: "/home/first.mp4", Duration: time.Minute}}}
	s.reload = func() ([]schedule.Channel, error) { return append(keyChannels(), home), nil }
	s.RequestRescan()
	s.RequestRescan()
	<-s.rescans
	s.rescan()

	if _, ok := s.channel("home"); !ok {
		t.Fatal("the rescan did not pick up the new channel")
	}
	if got := len(p.Loads()); got != loads {
		t.Errorf("the rescan reloaded the screen %d times, want none for an unchanged channel", got-loads)
	}
	select {
	case <-s.rescans:
		t.Error("two requests left two rescans queued, want them folded into one")
	default:
	}
}

// TestRescanStartsAnEmptyChannelPlaying is the first upload landing on an
// empty Home Movies channel that is on screen showing the card.
func TestRescanStartsAnEmptyChannelPlaying(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	empty := schedule.Channel{ID: "home", Number: 14, Name: "Home Movies"}
	s := newTestStation(t, p, clock, "home", append(keyChannels(), empty))
	s.play()
	if len(p.Loads()) != 0 {
		t.Fatalf("an empty channel loaded %v", p.Loads())
	}

	full := empty
	full.Items = []schedule.Item{{ID: "first", Title: "2025-06-14 14.03", Path: "/home/first.mp4", Duration: time.Minute}}
	s.reload = func() ([]schedule.Channel, error) { return append(keyChannels(), full), nil }
	s.rescan()

	if got := p.LastLoad(t).Path; got != "/home/first.mp4" {
		t.Errorf("after the rescan the screen loaded %q, want the new clip", got)
	}
}

// TestVolumeBarIsHalfTheNumbersSize keeps the two pieces of text their own
// sizes: the channel number full, the volume bar half.
func TestVolumeBarIsHalfTheNumbersSize(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", keyChannels())
	s.play()

	s.key(input.Key{Action: input.ChannelUp})
	if got := lastText(t, p).Scale; got != 100 {
		t.Errorf("the channel number is at %d%%, want 100", got)
	}
	s.key(input.Key{Action: input.VolumeUp})
	if got := lastText(t, p).Scale; got != 50 {
		t.Errorf("the volume bar is at %d%%, want 50", got)
	}
}

// radioChannels is a station of one channel with one long song and one
// short one, and a video channel to tune from.
func radioChannels() []schedule.Channel {
	return []schedule.Channel{
		{ID: "clips", Number: 5, Name: "Test Clips", Items: []schedule.Item{
			{ID: "a", Title: "Clip A", Path: "/lib/a.mp4", Duration: 10 * time.Minute},
		}},
		{ID: "beatles", Number: 21, Name: "The Beatles", Items: []schedule.Item{
			{ID: "help", Title: "Help!", Artist: "The Beatles", Path: "/r/help.mp3", Duration: 3 * time.Minute},
		}},
	}
}

// TestSongTitleShowsAtTheStartAndTheEnd is a song playing from the top: its
// title and artist go up as it starts and again before it ends, at half size.
func TestSongTitleShowsAtTheStartAndTheEnd(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "beatles", radioChannels())
	s.play()
	start := clock.Now()
	offset := p.LastLoad(t).Offset

	s.showTrackDue()
	got := lastText(t, p)
	if got.Text != "Help!\nThe Beatles" || got.Scale != 30 || got.Duration != 8*time.Second {
		t.Errorf("opening title = %+v, want Help! over The Beatles at 30%% size for 8s", got)
	}

	wantEnd := start.Add(3*time.Minute - offset - 8*time.Second)
	if !s.trackEndAt.Equal(wantEnd) {
		t.Fatalf("closing title due at %s, want %s", s.trackEndAt, wantEnd)
	}
	before := len(p.Texts())
	clock.Advance(wantEnd.Sub(clock.Now()))
	s.showTrackDue()
	if len(p.Texts()) != before+1 || lastText(t, p).Text != "Help!\nThe Beatles" {
		t.Error("the closing title did not go up")
	}
}

// TestTuningToAStationHoldsTheTitleForTheNumber is a channel change onto a
// radio station: the number shows first and the title waits for it.
func TestTuningToAStationHoldsTheTitleForTheNumber(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", radioChannels())
	s.play()

	s.key(input.Key{Action: input.ChannelUp})
	if got := lastText(t, p).Text; got != "21" {
		t.Fatalf("tuning showed %q, want the channel number", got)
	}
	if want := clock.Now().Add(settings.Default().ChannelNumber.Duration); !s.trackStartAt.Equal(want) {
		t.Errorf("title due at %s, want once the number is down at %s", s.trackStartAt, want)
	}
	s.showTrackDue()
	if got := lastText(t, p).Text; got != "21" {
		t.Errorf("the title replaced the number early: %q", got)
	}
}

func TestVideoShowsNoTitle(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", radioChannels())
	s.play()
	if !s.trackStartAt.IsZero() || !s.trackEndAt.IsZero() {
		t.Error("a video scheduled a title")
	}
}

// TestShortSongShowsItsTitleOnce is a song too short for two titles: the
// closing one would follow straight on from the opening one.
func TestShortSongShowsItsTitleOnce(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := radioChannels()
	channels[1].Items[0].Duration = 12 * time.Second
	s := newTestStation(t, p, clock, "beatles", channels)
	s.play()
	if !s.trackEndAt.IsZero() {
		t.Errorf("a %s song scheduled a closing title at %s", channels[1].Items[0].Duration, s.trackEndAt)
	}
}

func TestTrackInfoCanBeTurnedOff(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "beatles", radioChannels())
	s.settings.TrackInfo.Enabled = false
	s.play()
	s.showTrackDue()
	if texts := p.Texts(); len(texts) != 0 {
		t.Errorf("showed %v with track info off", texts)
	}
}

// TestEachItemPlaysAtItsOwnGain is the levelling: the correction for the item
// about to play is set before it loads.
func TestEachItemPlaysAtItsOwnGain(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	channels := radioChannels()
	channels[1].Items[0].Gain = -7.8
	s := newTestStation(t, p, clock, "beatles", channels)
	s.play()
	p.mu.Lock()
	gains := append([]float64(nil), p.gains...)
	p.mu.Unlock()
	if len(gains) != 1 || gains[0] != -7.8 {
		t.Errorf("gains set %v, want the song's -7.8 before it loaded", gains)
	}
}

// guideChannels is the radio fixture with a guide on 1 whose music is the
// Beatles station.
func guideChannels() []schedule.Channel {
	channels := radioChannels()
	beatles := channels[1]
	guide := schedule.Channel{ID: "guide", Number: 1, Name: "Guide", Items: beatles.Items, Guide: true, GuideMusic: beatles.ID}
	return append([]schedule.Channel{guide}, channels...)
}

// overlaysWithID counts the ShowOverlay calls for one overlay id.
func overlaysWithID(p *fakePlayer, id int) []overlayCall {
	var out []overlayCall
	for _, o := range p.Overlays() {
		if o.ID == id {
			out = append(out, o)
		}
	}
	return out
}

// TestGuideGoesUpAndComesDown is tuning to the guide and away again: its page
// covers the whole screen while it is tuned and is taken down after.
func TestGuideGoesUpAndComesDown(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", guideChannels())
	s.play()

	s.key(input.Key{Action: input.Digit, Digit: 1})
	clock.Advance(2 * time.Second)
	s.expireDigits()
	if s.Tuned() != "guide" {
		t.Fatalf("tuned %q, want the guide", s.Tuned())
	}
	pages := overlaysWithID(p, guideOverlay)
	if len(pages) != 1 || pages[0].Size != image.Pt(1920, 1080) || pages[0].X != 0 || pages[0].Y != 0 {
		t.Fatalf("guide overlays %+v, want one full screen page", pages)
	}

	s.key(input.Key{Action: input.ChannelUp})
	if s.Tuned() == "guide" {
		t.Fatal("channel up stayed on the guide")
	}
	removed := false
	for _, id := range p.Removed() {
		removed = removed || id == guideOverlay
	}
	if !removed {
		t.Error("leaving the guide did not take it down")
	}
}

// TestGuidePagesTurnAndListTheOtherChannels checks the rows: every channel but
// the guide, the channel the viewer came from highlighted, and a page flip
// drawing the guide again.
func TestGuidePagesTurnAndListTheOtherChannels(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "clips", guideChannels())
	s.play()
	s.key(input.Key{Action: input.ChannelDown}) // clips 5 -> guide 1
	if s.Tuned() != "guide" {
		t.Fatalf("tuned %q, want the guide", s.Tuned())
	}

	rows := s.guideRows()
	if len(rows) != 2 || rows[0].Number != 5 || rows[1].Number != 21 {
		t.Fatalf("rows %+v, want channels 5 and 21 and not the guide", rows)
	}
	if !rows[0].Highlight || rows[1].Highlight {
		t.Errorf("highlights %v %v, want the channel the viewer came from", rows[0].Highlight, rows[1].Highlight)
	}
	if rows[1].Now != "Help! · The Beatles" {
		t.Errorf("the radio row reads %q, want the song and artist", rows[1].Now)
	}

	before := len(overlaysWithID(p, guideOverlay))
	clock.Advance(guideFlip)
	s.flipGuide()
	if got := len(overlaysWithID(p, guideOverlay)); got != before+1 {
		t.Errorf("a flip drew the guide %d more times, want once", got-before)
	}
}

func TestGuideShowsNoSongTitles(t *testing.T) {
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s := newTestStation(t, p, clock, "guide", guideChannels())
	s.play()
	if !s.trackStartAt.IsZero() || !s.trackEndAt.IsZero() {
		t.Error("the guide's background song scheduled a title over the listings")
	}
	if got := s.guideMusic(); got != "Help! · The Beatles" {
		t.Errorf("guide music %q, want the song playing behind it", got)
	}
}
