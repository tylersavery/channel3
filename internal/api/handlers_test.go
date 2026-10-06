package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tylersavery/channel3/internal/schedule"
)

// update rewrites the golden files instead of comparing against them:
// go test ./internal/api -update.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// eastern is the zone the fixtures are scheduled in. It is fixed rather than
// loaded from the machine's database so the golden files are the same
// everywhere, and offset by four hours so they read like the contract's
// examples.
var eastern = time.FixedZone("EDT", -4*60*60)

// fixedNow is the instant every fixture response is taken at.
func fixedNow() time.Time {
	return time.Date(2026, 9, 22, 19, 4, 11, 0, eastern)
}

// fixtureChannels is the test station.
//
// The channels are deliberately out of order: every response sorts by number,
// and a fixture that arrived sorted would not prove it. The durations are not
// round, because a guide that only ever adds whole minutes hides arithmetic
// errors that real videos would find.
func fixtureChannels() []schedule.Channel {
	return []schedule.Channel{
		{
			ID: "docs", Number: 7, Name: "Documentaries",
			Items: []schedule.Item{
				{ID: "d1", Title: "Bridges", Path: "/lib/docs/bridges.mp4", Duration: 22*time.Minute + 3*time.Second},
				{ID: "d2", Title: "Harbours", Path: "/lib/docs/harbours.mp4", Duration: 47 * time.Minute},
			},
		},
		{
			ID: "trains", Number: 3, Name: "Train TV",
			Items: []schedule.Item{
				{ID: "abc", Title: "Steam Engines", Path: "/lib/trains/steam.mp4", Duration: 612437 * time.Millisecond},
				{ID: "def", Title: "Switching Yard", Path: "/lib/trains/yard.mp4", Duration: 22*time.Minute + 48*time.Second},
				{ID: "ghi", Title: "Level Crossings", Path: "/lib/trains/crossings.mp4", Duration: 9*time.Minute + 30*time.Second},
			},
		},
		{ID: "empty", Number: 9, Name: "Nothing At All"},
	}
}

// testDeps is the fixture station wired to a fixed clock.
func testDeps() Deps {
	return Deps{
		Channels: fixtureChannels,
		Now:      fixedNow,
		Tuned:    func() string { return "trains" },
		Clock:    schedule.NewClock(eastern),
	}
}

// get asks the handler for a path and returns the response.
func get(t *testing.T, deps Deps, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	New(deps).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// decode reads a JSON response body into v and fails if it is not JSON.
func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type %q, want JSON", got)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

// checkGolden compares a response body against testdata/<name>, ignoring
// whitespace, and rewrites the file under -update.
func checkGolden(t *testing.T, name string, body []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)

	if *update {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err != nil {
			t.Fatalf("indent %s: %v", name, err)
		}
		pretty.WriteString("\n")
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./internal/api -update to create it)", path, err)
	}
	if got, want := compact(t, body), compact(t, want); got != want {
		t.Errorf("%s does not match the golden file\n got: %s\nwant: %s", name, got, want)
	}
}

// compact normalises JSON whitespace so a golden file can be pretty printed.
func compact(t *testing.T, raw []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		t.Fatalf("compact %s: %v", raw, err)
	}
	return out.String()
}

func TestChannelsMatchesTheGoldenFile(t *testing.T) {
	rec := get(t, testDeps(), "/api/channels")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	checkGolden(t, "channels.json", rec.Body.Bytes())
}

func TestNowMatchesTheGoldenFile(t *testing.T) {
	rec := get(t, testDeps(), "/api/now")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	checkGolden(t, "now.json", rec.Body.Bytes())
}

func TestGuideMatchesTheGoldenFile(t *testing.T) {
	rec := get(t, testDeps(), "/api/guide?hours=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	checkGolden(t, "guide.json", rec.Body.Bytes())
}

// TestChannelsAreSortedByNumber is the one ordering rule the page depends on,
// and the fixtures arrive in the wrong order on purpose.
func TestChannelsAreSortedByNumber(t *testing.T) {
	for _, target := range []string{"/api/channels", "/api/now", "/api/guide"} {
		t.Run(target, func(t *testing.T) {
			rec := get(t, testDeps(), target)
			var body struct {
				Channels []struct {
					Number int `json:"number"`
				} `json:"channels"`
			}
			decode(t, rec, &body)

			var numbers []int
			for _, ch := range body.Channels {
				numbers = append(numbers, ch.Number)
			}
			want := []int{3, 7, 9}
			if len(numbers) != len(want) {
				t.Fatalf("got %d channels, want %d", len(numbers), len(want))
			}
			for i, n := range numbers {
				if n != want[i] {
					t.Fatalf("channels came back as %v, want %v", numbers, want)
				}
			}
		})
	}
}

// TestNowReportsTheOffsetTheScheduleGives ties the API to the same pure
// function the broadcast loop calls, rather than to a number typed into a test.
func TestNowReportsTheOffsetTheScheduleGives(t *testing.T) {
	deps := testDeps()
	clock := deps.Clock
	trains := fixtureChannels()[1]
	want, ok := schedule.At(trains, fixedNow(), clock)
	if !ok {
		t.Fatal("the fixture channel has nothing on")
	}

	rec := get(t, deps, "/api/now")
	var body nowResponse
	decode(t, rec, &body)

	var found *nowChannel
	for i := range body.Channels {
		if body.Channels[i].ID == "trains" {
			found = &body.Channels[i]
		}
	}
	if found == nil || found.Now == nil {
		t.Fatal("the trains channel is not airing anything")
	}
	if found.Now.ID != want.Item.ID {
		t.Errorf("now is %q, want %q", found.Now.ID, want.Item.ID)
	}
	if found.Now.Offset != want.Offset.Seconds() {
		t.Errorf("offset %v, want %v", found.Now.Offset, want.Offset.Seconds())
	}
	if found.Now.Duration != want.Item.Duration.Seconds() {
		t.Errorf("duration %v, want %v", found.Now.Duration, want.Item.Duration.Seconds())
	}
	if found.Next == nil {
		t.Fatal("nothing is on next")
	}
	if found.Next.Offset != 0 {
		t.Errorf("the next item has offset %v, want 0", found.Next.Offset)
	}
	if found.Next.Start != found.Now.End {
		t.Errorf("next starts at %s, want the end of what is on now, %s", found.Next.Start, found.Now.End)
	}
}

// TestTunedIsReported checks the field the page highlights a row with.
func TestTunedIsReported(t *testing.T) {
	var body nowResponse
	decode(t, get(t, testDeps(), "/api/now"), &body)
	if body.Tuned == nil || *body.Tuned != "trains" {
		t.Fatalf("tuned is %v, want trains", body.Tuned)
	}
}

// TestNothingTunedIsNull is the shape before the station has tuned, which is
// what the API answers while mpv is still starting.
func TestNothingTunedIsNull(t *testing.T) {
	deps := testDeps()
	deps.Tuned = func() string { return "" }

	rec := get(t, deps, "/api/now")
	var raw map[string]any
	decode(t, rec, &raw)
	if got, ok := raw["tuned"]; !ok || got != nil {
		t.Fatalf("tuned is %v, want null", got)
	}
}

// TestAChannelWithNoItemsIsEmptyNotMissing keeps a configured but empty channel
// in every response, because it is a channel that shows the Please Stand By
// card and not an error.
func TestAChannelWithNoItemsIsEmptyNotMissing(t *testing.T) {
	var now nowResponse
	decode(t, get(t, testDeps(), "/api/now"), &now)
	var empty *nowChannel
	for i := range now.Channels {
		if now.Channels[i].ID == "empty" {
			empty = &now.Channels[i]
		}
	}
	if empty == nil {
		t.Fatal("the empty channel is missing from /api/now")
	}
	if empty.Now != nil || empty.Next != nil {
		t.Errorf("the empty channel reports now=%v next=%v, want both null", empty.Now, empty.Next)
	}

	// The guide's slots must be an empty list rather than a null, so the page
	// can iterate every channel the same way.
	var guide struct {
		Channels []struct {
			ID    string      `json:"id"`
			Slots []slotJSON  `json:"slots"`
			Raw   json.Number `json:"-"`
		} `json:"channels"`
	}
	decode(t, get(t, testDeps(), "/api/guide"), &guide)
	for _, ch := range guide.Channels {
		if ch.ID != "empty" {
			continue
		}
		if ch.Slots == nil {
			t.Fatal("the empty channel has a null slots list, want []")
		}
		if len(ch.Slots) != 0 {
			t.Fatalf("the empty channel has %d slots, want none", len(ch.Slots))
		}
		return
	}
	t.Fatal("the empty channel is missing from /api/guide")
}

// TestEmptySlotsEncodeAsAnArray checks the bytes rather than the decoded value,
// because a null and an empty list decode the same into a Go slice.
func TestEmptySlotsEncodeAsAnArray(t *testing.T) {
	rec := get(t, testDeps(), "/api/guide")
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"slots":[]`)) {
		t.Errorf("the empty channel's slots are not an empty array:\n%s", rec.Body.String())
	}
}

// TestNoChannelsIsAnEmptyList is the shape the API answers with before the
// library has been read.
func TestNoChannelsIsAnEmptyList(t *testing.T) {
	deps := testDeps()
	deps.Channels = func() []schedule.Channel { return nil }
	deps.Tuned = func() string { return "" }

	rec := get(t, deps, "/api/channels")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := compact(t, rec.Body.Bytes()); got != `{"channels":[]}` {
		t.Errorf("body %s, want an empty channel list", got)
	}
}

func TestBadHoursIsRejected(t *testing.T) {
	for _, target := range []string{"/api/guide?hours=0", "/api/guide?hours=49", "/api/guide?hours=abc", "/api/guide?hours=-1", "/api/guide?hours=2.5"} {
		t.Run(target, func(t *testing.T) {
			rec := get(t, testDeps(), target)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			var body errorResponse
			decode(t, rec, &body)
			if body.Error == "" {
				t.Error("the 400 carries no error message")
			}
		})
	}
}

func TestHoursAtTheLimitsIsAccepted(t *testing.T) {
	for _, target := range []string{"/api/guide?hours=1", "/api/guide?hours=48"} {
		if rec := get(t, testDeps(), target); rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", target, rec.Code)
		}
	}
}

// TestTheDefaultHorizonIsSixHours pins the default the page relies on.
func TestTheDefaultHorizonIsSixHours(t *testing.T) {
	var body guideResponse
	decode(t, get(t, testDeps(), "/api/guide"), &body)

	from, err := time.Parse(time.RFC3339, body.From)
	if err != nil {
		t.Fatalf("from %q: %v", body.From, err)
	}
	to, err := time.Parse(time.RFC3339, body.To)
	if err != nil {
		t.Fatalf("to %q: %v", body.To, err)
	}
	if got := to.Sub(from); got != 6*time.Hour {
		t.Errorf("the default guide covers %s, want 6h", got)
	}
	if !from.Equal(fixedNow()) {
		t.Errorf("from is %s, want %s", from, fixedNow())
	}
}

// TestGuideCoversTheWholeHorizon checks that the last slot of every channel
// with items runs to or past `to`, which is what a page drawing a timeline
// needs.
func TestGuideCoversTheWholeHorizon(t *testing.T) {
	var body guideResponse
	decode(t, get(t, testDeps(), "/api/guide?hours=2"), &body)

	to, err := time.Parse(time.RFC3339, body.To)
	if err != nil {
		t.Fatalf("to %q: %v", body.To, err)
	}
	for _, ch := range body.Channels {
		if len(ch.Slots) == 0 {
			continue
		}
		last := ch.Slots[len(ch.Slots)-1]
		end, err := time.Parse(time.RFC3339, last.End)
		if err != nil {
			t.Fatalf("%s: end %q: %v", ch.ID, last.End, err)
		}
		if end.Before(to) {
			t.Errorf("%s stops at %s, before the horizon at %s", ch.ID, end, to)
		}
	}
}

// TestDurationComesFromTheItemNotTheAiring is the rollover rule. An airing that
// would run past 04:00 is cut short there, so end minus start is shorter than
// the video, and a page that measured the video that way would draw a progress
// bar that never fills.
func TestDurationComesFromTheItemNotTheAiring(t *testing.T) {
	const length = 2*time.Hour + 5*time.Minute
	deps := testDeps()
	deps.Channels = func() []schedule.Channel {
		return []schedule.Channel{{
			ID: "long", Number: 1, Name: "One Long Film",
			Items: []schedule.Item{{ID: "film", Title: "The Long Film", Path: "/lib/long.mp4", Duration: length}},
		}}
	}
	// Half an hour before the rollover, with an item that cannot finish before
	// it.
	deps.Now = func() time.Time { return time.Date(2026, 9, 22, 3, 30, 0, 0, eastern) }

	var body guideResponse
	decode(t, get(t, deps, "/api/guide?hours=1"), &body)
	if len(body.Channels) != 1 || len(body.Channels[0].Slots) == 0 {
		t.Fatalf("the guide is empty: %+v", body)
	}
	slot := body.Channels[0].Slots[0]

	if slot.Duration != length.Seconds() {
		t.Errorf("duration %v, want the item's own %v", slot.Duration, length.Seconds())
	}
	start, err := time.Parse(time.RFC3339, slot.Start)
	if err != nil {
		t.Fatalf("start %q: %v", slot.Start, err)
	}
	end, err := time.Parse(time.RFC3339, slot.End)
	if err != nil {
		t.Fatalf("end %q: %v", slot.End, err)
	}
	aired := end.Sub(start)
	if aired >= length {
		t.Fatalf("the airing was not cut at the rollover: it runs %s of a %s item", aired, length)
	}
	if got := end.In(eastern).Format("15:04:05"); got != "04:00:00" {
		t.Errorf("the airing ends at %s, want the 04:00 rollover", got)
	}

	// The same item on /api/now reports the same length.
	var now nowResponse
	decode(t, get(t, deps, "/api/now"), &now)
	if now.Channels[0].Now == nil || now.Channels[0].Now.Duration != length.Seconds() {
		t.Errorf("/api/now reports %+v, want a duration of %v", now.Channels[0].Now, length.Seconds())
	}
}

// TestWriteMethodsAreRejected is the ground rule that no HTTP request may ever
// change what is playing.
func TestWriteMethodsAreRejected(t *testing.T) {
	targets := []string{"/api/channels", "/api/now", "/api/guide", "/"}
	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, target := range targets {
		for _, method := range methods {
			t.Run(method+" "+target, func(t *testing.T) {
				rec := httptest.NewRecorder()
				New(testDeps()).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("status %d, want 405", rec.Code)
				}
				if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
					t.Errorf("Allow is %q, want \"GET, HEAD\"", got)
				}
				var body errorResponse
				decode(t, rec, &body)
				if body.Error == "" {
					t.Error("the 405 carries no error message")
				}
			})
		}
	}
}

// TestUnknownEndpointIsJSON keeps a client that mistypes a path from having to
// parse an HTML page to find out.
func TestUnknownEndpointIsJSON(t *testing.T) {
	rec := get(t, testDeps(), "/api/nonsense")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	var body errorResponse
	decode(t, rec, &body)
	if body.Error == "" {
		t.Error("the 404 carries no error message")
	}
}

// TestTimesAreInTheClocksZone checks that the page is told the Pi's local time
// and not UTC.
func TestTimesAreInTheClocksZone(t *testing.T) {
	var body nowResponse
	decode(t, get(t, testDeps(), "/api/now"), &body)
	if want := "2026-09-22T19:04:11-04:00"; body.Time != want {
		t.Errorf("time is %q, want %q", body.Time, want)
	}
}

// builtUI is a web build as Vite would leave it.
//
// The names that are not hashed output are the ones that catch a cache rule
// written too loosely: a touch icon, a web manifest and a logo all carry a dash
// and none of them changes name when its bytes change.
func builtUI() fs.FS {
	return fstest.MapFS{
		"index.html":                 &fstest.MapFile{Data: []byte("<!doctype html><title>Guide</title>")},
		"assets/index-BfG3k2Ls.js":   &fstest.MapFile{Data: []byte("console.log('guide')")},
		"assets/index-Zq81mm0p.css":  &fstest.MapFile{Data: []byte("body{margin:0}")},
		"assets/logo-a1b2c3d4.svg":   &fstest.MapFile{Data: []byte("<svg/>")},
		"apple-touchicon.png":        &fstest.MapFile{Data: []byte("icon")},
		"manifest-webmanifest.json":  &fstest.MapFile{Data: []byte("{}")},
		"logo-horizontal.svg":        &fstest.MapFile{Data: []byte("<svg/>")},
		"favicon.ico":                &fstest.MapFile{Data: []byte("icon")},
		"nested/deeper/notes.txt":    &fstest.MapFile{Data: []byte("not part of the app")},
		"index.html.bak/placeholder": &fstest.MapFile{Data: []byte("a directory that looks like a file")},
	}
}

// TestTheFallbackPageIsServedWhenNothingIsBuilt is the fresh clone case: the
// binary has no UI in it and the guide still has to answer.
func TestTheFallbackPageIsServedWhenNothingIsBuilt(t *testing.T) {
	for name, ui := range map[string]fs.FS{
		"no FS at all":   nil,
		"an empty build": fstest.MapFS{},
		"a build with no index": fstest.MapFS{
			"assets/index-DkJ2f8Qa.js": &fstest.MapFile{Data: []byte("console.log('guide')")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			deps := testDeps()
			deps.UI = ui

			rec := get(t, deps, "/")
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("content type %q, want HTML", got)
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte("has not been built")) {
				t.Errorf("the fallback page does not say the UI is missing:\n%s", rec.Body.String())
			}
			// The API is the reason the page is a 200, so it must still work.
			if api := get(t, deps, "/api/channels"); api.Code != http.StatusOK {
				t.Errorf("/api/channels answers %d while the UI is missing, want 200", api.Code)
			}
		})
	}
}

func TestIndexIsServedAtRoot(t *testing.T) {
	deps := testDeps()
	deps.UI = builtUI()

	rec := get(t, deps, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("<title>Guide</title>")) {
		t.Fatalf("root served %q, want the built index.html", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != noCache {
		t.Errorf("index.html Cache-Control is %q, want %q", got, noCache)
	}
}

// TestOnlyHashedAssetsAreCachedForALongTime is the rule that a file may only be
// held for a year when its name changes with its contents.
//
// Everything else is revalidated. A dash in a name proves nothing: a browser
// that pinned apple-touchicon.png for a year would keep serving last year's
// icon, and nothing short of a new file name would get it back.
func TestOnlyHashedAssetsAreCachedForALongTime(t *testing.T) {
	deps := testDeps()
	deps.UI = builtUI()

	longLived := []string{"/assets/index-BfG3k2Ls.js", "/assets/index-Zq81mm0p.css", "/assets/logo-a1b2c3d4.svg"}
	for _, target := range longLived {
		rec := get(t, deps, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", target, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != immutableCache {
			t.Errorf("%s: Cache-Control is %q, want %q", target, got, immutableCache)
		}
	}

	revalidated := []string{"/", "/index.html", "/apple-touchicon.png", "/manifest-webmanifest.json", "/logo-horizontal.svg", "/favicon.ico"}
	for _, target := range revalidated {
		rec := get(t, deps, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", target, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != noCache {
			t.Errorf("%s: Cache-Control is %q, want %q", target, got, noCache)
		}
	}
}

// TestDirectoriesAreNotListed keeps the build's layout to itself.
func TestDirectoriesAreNotListed(t *testing.T) {
	deps := testDeps()
	deps.UI = builtUI()

	for _, target := range []string{"/assets", "/assets/", "/nested/deeper"} {
		rec := get(t, deps, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404: %s", target, rec.Code, rec.Body.String())
		}
	}
}

// TestPathsOutsideTheBuildAreRefused is the traversal check. The cleaned path
// can never climb out of the FS, and a request that tries lands on a file that
// is not there.
func TestPathsOutsideTheBuildAreRefused(t *testing.T) {
	deps := testDeps()
	deps.UI = builtUI()

	for _, target := range []string{"/../secrets.txt", "/assets/../../secrets.txt", "//etc/passwd"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://pi.local", nil)
		req.URL.Path = target
		New(deps).ServeHTTP(rec, req)
		if rec.Code == http.StatusOK && bytes.Contains(rec.Body.Bytes(), []byte("secrets")) {
			t.Errorf("%s served something outside the build: %s", target, rec.Body.String())
		}
	}
}

// TestAMissingAssetIs404 keeps a missing script from being answered with the
// fallback page, which a browser would try to run as JavaScript.
func TestAMissingAssetIs404(t *testing.T) {
	deps := testDeps()
	deps.UI = builtUI()

	rec := get(t, deps, "/assets/index-NotBuilt.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	var body errorResponse
	decode(t, rec, &body)
	if body.Error == "" {
		t.Error("the 404 carries no error message")
	}
}

// TestSongsCarryTheirArtist is a radio station in the guide: a song's artist
// is in now, next and the guide's slots, and video has no artist key at all.
func TestSongsCarryTheirArtist(t *testing.T) {
	deps := testDeps()
	deps.Channels = func() []schedule.Channel {
		return []schedule.Channel{{
			ID: "beatles", Number: 21, Name: "The Beatles",
			Items: []schedule.Item{{ID: "help", Title: "Help!", Artist: "The Beatles", Path: "/r/help.mp3", Duration: 2*time.Minute + 18*time.Second}},
		}}
	}
	for _, target := range []string{"/api/now", "/api/guide?hours=1"} {
		body := get(t, deps, target).Body.String()
		if !strings.Contains(body, `"artist":"The Beatles"`) {
			t.Errorf("%s does not carry the artist: %s", target, body)
		}
	}
	if body := get(t, testDeps(), "/api/now").Body.String(); strings.Contains(body, `"artist"`) {
		t.Errorf("video items carry an artist key: %s", body)
	}
}
