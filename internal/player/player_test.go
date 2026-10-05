package player

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testTimings make the supervisor react inside a test's patience without
// changing any of the logic being tested.
func testTimings() Timings {
	return Timings{
		Command:        2 * time.Second,
		Dial:           2 * time.Second,
		HealthInterval: 50 * time.Millisecond,
		HealthTimeout:  200 * time.Millisecond,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		HealthyPeriod:  time.Second,
		Quit:           500 * time.Millisecond,
	}
}

// startSupervisor starts a supervisor over a fake mpv and stops it afterwards.
func startSupervisor(t *testing.T, launcher *fakeLauncher, timings Timings) *Supervisor {
	t.Helper()
	dir := shortTempDir(t)
	sup, err := Start(t.Context(), Options{
		Launcher:    launcher,
		Socket:      filepath.Join(dir, "mpv.sock"),
		StandbyPath: filepath.Join(dir, "standby.png"),
		Logger:      testLogger(),
		Timings:     timings,
	})
	if err != nil {
		t.Fatalf("start the supervisor: %v", err)
	}
	t.Cleanup(func() {
		if err := sup.Close(); err != nil {
			t.Errorf("closing the supervisor: %v", err)
		}
	})
	return sup
}

// waitForCommand waits for a command whose first argument is name and returns
// the most recent one.
func waitForCommand(t *testing.T, fake *fakeMPV, name string) []any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := fake.CommandsNamed(name); len(got) > 0 {
			return got[len(got)-1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s command reached mpv; it saw %v", name, fake.Commands())
	return nil
}

// TestLoadSendsTheFourArgumentLoadfile pins the exact command, because the
// argument order changed in mpv 0.38 and an index in the wrong place silently
// starts every item from the beginning.
func TestLoadSendsTheFourArgumentLoadfile(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	if err := sup.Load("/lib/a.mp4", 83500*time.Millisecond); err != nil {
		t.Fatalf("load: %v", err)
	}

	got := waitForCommand(t, launcher.Current(), "loadfile")
	want := `["loadfile","/lib/a.mp4","replace",-1,"start=83.500"]`
	if commandStrings(got) != want {
		t.Errorf("mpv got %s, want %s", commandStrings(got), want)
	}
}

// TestStandbyLoadsTheCard checks the card is loaded by path and replaces
// whatever was playing.
func TestStandbyLoadsTheCard(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	if err := sup.Standby(); err != nil {
		t.Fatalf("standby: %v", err)
	}

	got := waitForCommand(t, launcher.Current(), "loadfile")
	if len(got) < 3 {
		t.Fatalf("standby sent %s, want a loadfile with a replace flag", commandStrings(got))
	}
	path, _ := got[1].(string)
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("standby loaded %q, want the png card", path)
	}
	if flag, _ := got[2].(string); flag != "replace" {
		t.Errorf("standby used the %q flag, want replace", flag)
	}
}

// TestShowTextSendsLevelZero checks the one message mpv is allowed to draw. mpv
// runs with osd-level=0, so a show-text at the default level would never
// appear.
func TestShowTextSendsLevelZero(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	if err := sup.ShowText("12", 1500*time.Millisecond); err != nil {
		t.Fatalf("show text: %v", err)
	}

	got := waitForCommand(t, launcher.Current(), "show-text")
	if len(got) != 4 {
		t.Fatalf("show text sent %s, want text, duration and level", commandStrings(got))
	}
	if text, _ := got[1].(string); text != "12" {
		t.Errorf("show text drew %q, want 12", text)
	}
	if ms, _ := got[2].(float64); ms != 1500 {
		t.Errorf("show text lasts %vms, want 1500", got[2])
	}
	if level, _ := got[3].(float64); level != 0 {
		t.Errorf("show text asked for level %v, want 0 so osd-level=0 lets it through", got[3])
	}
}

// TestEndFileReasonsAreFiltered is the rule that keeps the station from
// looping: mpv reports the file our own load replaced, and that must not look
// like a file that finished.
func TestEndFileReasonsAreFiltered(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	// Two loads, so the swallowed reasons name the entry the second load
	// replaced, which is exactly what mpv does.
	if err := sup.Load("/lib/old.mp4", 0); err != nil {
		t.Fatalf("first load: %v", err)
	}
	fake := launcher.Current()
	waitForCommand(t, fake, "loadfile")
	replaced := fake.LastEntryID()

	if err := sup.Load("/lib/a.mp4", 0); err != nil {
		t.Fatalf("second load: %v", err)
	}
	current := fake.LastEntryID()

	fake.EmitEndFileFor(replaced, reasonStop)
	fake.EmitEndFileFor(replaced, reasonRedirect)
	fake.EmitEndFileFor(current, ReasonEOF)

	select {
	case ev := <-sup.Events():
		if ev.Kind != EndFile || ev.Reason != ReasonEOF {
			t.Fatalf("got %+v, want the eof event and nothing before it", ev)
		}
		if ev.Path != "/lib/a.mp4" {
			t.Errorf("event names %q, want the loaded path", ev.Path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the eof event never arrived")
	}
}

// TestEndFileErrorIsSurfaced checks the other reason the station acts on.
func TestEndFileErrorIsSurfaced(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	if err := sup.Load("/lib/broken.mp4", 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	fake := launcher.Current()
	waitForCommand(t, fake, "loadfile")
	fake.EmitEndFile(ReasonError)

	select {
	case ev := <-sup.Events():
		if ev.Kind != EndFile || ev.Reason != ReasonError || ev.Path != "/lib/broken.mp4" {
			t.Fatalf("got %+v, want an error event for the loaded path", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the error event never arrived")
	}
}

// TestDroppedConnectionRestartsMPV is the recovery the whole appliance depends
// on: nobody is there to restart anything by hand.
func TestDroppedConnectionRestartsMPV(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())
	waitForCommand(t, launcher.Current(), "get_property")

	launcher.Current().CloseConns()

	select {
	case ev := <-sup.Events():
		if ev.Kind != Restarted {
			t.Fatalf("got %+v, want a Restarted event", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mpv was never restarted")
	}
	if launches := launcher.Launches(); launches < 2 {
		t.Errorf("mpv was launched %d times, want at least 2", launches)
	}

	// The player has to be usable again straight away, because the station
	// reloads the moment it sees the event.
	if err := sup.Load("/lib/a.mp4", time.Second); err != nil {
		t.Fatalf("load after a restart: %v", err)
	}
}

// TestHungMPVIsRestarted covers the case the process is alive but not
// answering, which is what a TV power cycle can leave behind.
func TestHungMPVIsRestarted(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())
	launcher.Current().Stall(true)

	select {
	case ev := <-sup.Events():
		if ev.Kind != Restarted {
			t.Fatalf("got %+v, want a Restarted event", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a hung mpv was never restarted")
	}
}

// TestStartRefusesOldMPV checks the version gate, because on an older mpv every
// item would silently play from its start.
func TestStartRefusesOldMPV(t *testing.T) {
	launcher := &fakeLauncher{t: t, version: "mpv v0.37.0"}
	dir := shortTempDir(t)

	_, err := Start(t.Context(), Options{
		Launcher:    launcher,
		Socket:      filepath.Join(dir, "mpv.sock"),
		StandbyPath: filepath.Join(dir, "standby.png"),
		Logger:      testLogger(),
		Timings:     testTimings(),
	})
	if err == nil {
		t.Fatal("expected mpv 0.37 to be refused")
	}
	for _, want := range []string{"0.37", "0.38"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

// TestStartAcceptsTheMinimumVersion guards the boundary from the other side.
func TestStartAcceptsTheMinimumVersion(t *testing.T) {
	launcher := &fakeLauncher{t: t, version: "mpv v0.38.0-dirty"}
	startSupervisor(t, launcher, testTimings())
}

// TestPositionReportsPathAndOffset covers the reconcile tick's input.
func TestPositionReportsPathAndOffset(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())
	fake := launcher.Current()
	fake.SetProperty("path", "/lib/a.mp4")
	fake.SetProperty("time-pos", 12.25)

	path, pos, err := sup.Position()
	if err != nil {
		t.Fatalf("position: %v", err)
	}
	if path != "/lib/a.mp4" {
		t.Errorf("path is %q, want /lib/a.mp4", path)
	}
	if want := 12250 * time.Millisecond; pos != want {
		t.Errorf("offset is %s, want %s", pos, want)
	}
}

// TestPositionOnIdleMPVIsNotAnError checks the state a freshly restarted mpv is
// in: the station must read it as "reload", not as a failure.
func TestPositionOnIdleMPVIsNotAnError(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())
	fake := launcher.Current()
	fake.SetPropertyError("path", "property unavailable")
	fake.SetPropertyError("time-pos", "property unavailable")

	path, pos, err := sup.Position()
	if err != nil {
		t.Fatalf("position on an idle mpv: %v", err)
	}
	if path != "" || pos != 0 {
		t.Errorf("got %q at %s, want an empty position", path, pos)
	}
}

// TestParseVersion covers the shapes mpv reports.
func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    version
		wantErr bool
	}{
		{in: "mpv v0.41.0", want: version{0, 41}},
		{in: "mpv v0.38.0-dirty", want: version{0, 38}},
		{in: "mpv 1.0.0", want: version{1, 0}},
		{in: "v0.37.0", want: version{0, 37}},
		{in: "", wantErr: true},
		{in: "mpv unknown", wantErr: true},
	}
	for _, c := range cases {
		got, err := parseVersion(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseVersion(%q) returned %v, want an error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseVersion(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestBaseArgsCarryTheSocket checks the flag the IPC client depends on and the
// two flags that keep the television free of chrome.
func TestBaseArgsCarryTheSocket(t *testing.T) {
	args := BaseArgs("/run/mpv.sock")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--input-ipc-server=/run/mpv.sock", "--no-osc", "--osd-level=0", "--idle=yes", "--image-display-duration=inf"} {
		if !strings.Contains(joined, want) {
			t.Errorf("base args %v are missing %s", args, want)
		}
	}
}

// TestWriteStandbyCard checks the card reaches disk as a PNG.
func TestWriteStandbyCard(t *testing.T) {
	dir := shortTempDir(t)
	path, err := WriteStandbyCard(dir)
	if err != nil {
		t.Fatalf("write the standby card: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the standby card: %v", err)
	}
	if len(data) < 1024 {
		t.Errorf("the card is %d bytes, which is too small to be the image", len(data))
	}
	if !strings.HasPrefix(string(data[:8]), "\x89PNG") {
		t.Error("the card is not a PNG")
	}
}

// TestEntryIdsAreForgotten checks the entry table cannot grow without bound
// over a broadcast day of loads.
func TestEntryIdsAreForgotten(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	for i := range 50 {
		if err := sup.Load("/lib/a.mp4", time.Duration(i)*time.Second); err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
	}
	sup.mu.Lock()
	entries := len(sup.entries)
	sup.mu.Unlock()
	if entries > 2 {
		t.Errorf("the supervisor is holding %d playlist entries, want at most 2", entries)
	}
}

// TestLoadRejectsANegativeOffset checks the schedule can never ask mpv to seek
// before the start of a file.
func TestLoadRejectsANegativeOffset(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	if err := sup.Load("/lib/a.mp4", -5*time.Second); err != nil {
		t.Fatalf("load: %v", err)
	}
	got := waitForCommand(t, launcher.Current(), "loadfile")
	var start string
	if len(got) == 5 {
		start, _ = got[4].(string)
	}
	if start != "start=0.000" {
		t.Errorf("mpv got %s, want a zero start", commandStrings(got))
	}
}

// TestNextBackoff walks the restart schedule the plan pins: half a second, then
// doubling, then held at the cap so a Pi that cannot start mpv retries for ever
// without hammering.
func TestNextBackoff(t *testing.T) {
	d := DefaultTimings()
	cases := []struct {
		current time.Duration
		want    time.Duration
	}{
		{current: 500 * time.Millisecond, want: time.Second},
		{current: time.Second, want: 2 * time.Second},
		{current: 2 * time.Second, want: 4 * time.Second},
		{current: 4 * time.Second, want: 5 * time.Second},
		{current: 5 * time.Second, want: 5 * time.Second},
	}
	for _, c := range cases {
		if got := nextBackoff(c.current, d.MaxBackoff); got != c.want {
			t.Errorf("nextBackoff(%s) = %s, want %s", c.current, got, c.want)
		}
	}
}

// TestBackoffAfterSession covers the reset: an mpv that stayed up for the
// healthy period starts the escalation again from the bottom.
func TestBackoffAfterSession(t *testing.T) {
	d := DefaultTimings()
	cases := []struct {
		name     string
		current  time.Duration
		lifetime time.Duration
		want     time.Duration
	}{
		{name: "short session keeps escalating", current: 4 * time.Second, lifetime: time.Second, want: 4 * time.Second},
		{name: "just under the healthy period keeps escalating", current: 5 * time.Second, lifetime: 59 * time.Second, want: 5 * time.Second},
		{name: "healthy session resets", current: 5 * time.Second, lifetime: 60 * time.Second, want: 500 * time.Millisecond},
		{name: "long healthy session resets", current: 2 * time.Second, lifetime: 6 * time.Hour, want: 500 * time.Millisecond},
	}
	for _, c := range cases {
		if got := backoffAfterSession(c.current, c.lifetime, d); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// TestDefaultTimings pins the numbers the plan specifies, so a change to them
// is a deliberate edit to this test rather than a silent drift.
func TestDefaultTimings(t *testing.T) {
	d := DefaultTimings()
	cases := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "initial backoff", got: d.InitialBackoff, want: 500 * time.Millisecond},
		{name: "backoff cap", got: d.MaxBackoff, want: 5 * time.Second},
		{name: "healthy reset", got: d.HealthyPeriod, want: 60 * time.Second},
		{name: "health interval", got: d.HealthInterval, want: 10 * time.Second},
		{name: "health deadline", got: d.HealthTimeout, want: 2 * time.Second},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s is %s, want %s", c.name, c.got, c.want)
		}
	}
}

// TestShowOverlayHandsMPVTheRightBytes checks the overlay end to end on the
// player side: the file holds the pixels in bgra order, and overlay-add names
// it with the geometry mpv needs to map it.
func TestShowOverlayHandsMPVTheRightBytes(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 4, G: 5, B: 6, A: 255})
	if err := sup.ShowOverlay(7, img, 10, 20); err != nil {
		t.Fatalf("show overlay: %v", err)
	}

	got := waitForCommand(t, launcher.Current(), "overlay-add")
	if len(got) != 10 {
		t.Fatalf("overlay-add sent %s, want id, x, y, file, offset, format, w, h, stride", commandStrings(got))
	}
	want := []any{float64(7), float64(10), float64(20)}
	for i, w := range want {
		if got[i+1] != w {
			t.Errorf("overlay-add argument %d is %v, want %v", i+1, got[i+1], w)
		}
	}
	if got[5] != float64(0) || got[6] != "bgra" || got[7] != float64(2) || got[8] != float64(1) || got[9] != float64(8) {
		t.Errorf("overlay-add geometry is %s, want offset 0, bgra, 2x1, stride 8", commandStrings(got[5:]))
	}
	path, _ := got[4].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the overlay file mpv was given: %v", err)
	}
	if wantBytes := []byte{3, 2, 1, 255, 6, 5, 4, 255}; string(data) != string(wantBytes) {
		t.Errorf("overlay file holds %v, want %v", data, wantBytes)
	}
}

// TestShowOverlayPacksASubImage is an image whose stride is wider than its
// width. mpv is told the stride is width times four, so the file must be packed.
func TestShowOverlayPacksASubImage(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	wide := image.NewRGBA(image.Rect(0, 0, 4, 2))
	sub := wide.SubImage(image.Rect(1, 0, 3, 2)).(*image.RGBA)
	if err := sup.ShowOverlay(1, sub, 0, 0); err != nil {
		t.Fatalf("show overlay: %v", err)
	}
	got := waitForCommand(t, launcher.Current(), "overlay-add")
	path, _ := got[4].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the overlay file: %v", err)
	}
	if len(data) != 2*2*4 {
		t.Errorf("overlay file is %d bytes, want %d for a packed 2x2 image", len(data), 2*2*4)
	}
}

func TestRemoveOverlaySendsTheID(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())

	if err := sup.RemoveOverlay(7); err != nil {
		t.Fatalf("remove overlay: %v", err)
	}
	got := waitForCommand(t, launcher.Current(), "overlay-remove")
	if len(got) != 2 || got[1] != float64(7) {
		t.Errorf("overlay-remove sent %s, want id 7", commandStrings(got))
	}
}

func TestScreenSizeReadsOSDDimensions(t *testing.T) {
	launcher := &fakeLauncher{t: t}
	sup := startSupervisor(t, launcher, testTimings())
	fake := launcher.Current()

	fake.SetProperty("osd-dimensions", map[string]any{"w": 1366, "h": 768})
	w, h, err := sup.ScreenSize()
	if err != nil || w != 1366 || h != 768 {
		t.Errorf("ScreenSize = %d, %d, %v, want 1366, 768", w, h, err)
	}

	// mpv with no video output reports zeros, and nothing should be drawn for
	// a screen that is not there.
	fake.SetProperty("osd-dimensions", map[string]any{"w": 0, "h": 0})
	if _, _, err := sup.ScreenSize(); err == nil {
		t.Error("a 0x0 screen was not an error")
	}
}
