package library

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixtureChannels are the channels the testdata library is scanned for. nature
// has no directory on disk, which is the "nothing ingested yet" case.
func fixtureChannels() []Channel {
	return []Channel{
		{ID: "trains", Number: 3, Name: "Train TV"},
		{ID: "space", Number: 12, Name: "Space Channel"},
		{ID: "nature", Number: 7, Name: "Nature Channel"},
	}
}

func TestScanExcludesUnplayableItems(t *testing.T) {
	logs := captureLogs(t)

	index, err := Scan("testdata", fixtureChannels())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	trains := index.Items("trains")
	if len(trains) != 2 {
		t.Fatalf("got %d trains items, want 2: %+v", len(trains), trains)
	}

	// Ordered by item id, not by sidecar file name: a-first.json holds zulu001
	// and z-last.json holds alpha001.
	if trains[0].ID != "alpha001" || trains[1].ID != "zulu001" {
		t.Errorf("got order %s, %s, want alpha001, zulu001", trains[0].ID, trains[1].ID)
	}

	for _, excluded := range []string{"gone002", "fail003", "zero004"} {
		for _, item := range trains {
			if item.ID == excluded {
				t.Errorf("item %s should have been excluded", excluded)
			}
		}
		if !strings.Contains(logs.String(), excluded) {
			t.Errorf("exclusion of %s was not logged:\n%s", excluded, logs.String())
		}
	}

	if got := len(index.Items("space")); got != 1 {
		t.Errorf("got %d space items, want 1", got)
	}
	if got := index.Items("nature"); len(got) != 0 {
		t.Errorf("a channel with no directory yielded %d items", len(got))
	}
	if got := index.Items("never-scanned"); got != nil {
		t.Errorf("an unscanned channel yielded %+v, want nil", got)
	}
}

func TestScanItemFields(t *testing.T) {
	captureLogs(t)

	index, err := Scan("testdata", fixtureChannels())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	absRoot, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	want := []Item{
		{
			ID:       "alpha001",
			Title:    "Switching Yard at Dusk",
			Path:     filepath.Join(absRoot, "library", "trains", "alpha001.mp4"),
			Duration: 1368 * time.Second,
			Source:   "https://www.youtube.com/watch?v=alpha001",
		},
		{
			ID:       "zulu001",
			Title:    "Steam Engines of the Rockies",
			Path:     filepath.Join(absRoot, "library", "trains", "zulu001.mp4"),
			Duration: 612437 * time.Millisecond,
			Source:   "https://www.youtube.com/watch?v=zulu001",
		},
	}
	if got := index.Items("trains"); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if !filepath.IsAbs(index.Items("space")[0].Path) {
		t.Errorf("path %q is not absolute", index.Items("space")[0].Path)
	}
}

// TestScanIsStable matters because the schedule's seeded shuffle is only
// reproducible if the item order it shuffles is.
func TestScanIsStable(t *testing.T) {
	captureLogs(t)

	first, err := Scan("testdata", fixtureChannels())
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	second, err := Scan("testdata", fixtureChannels())
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	for _, ch := range fixtureChannels() {
		if !reflect.DeepEqual(first.Items(ch.ID), second.Items(ch.ID)) {
			t.Errorf("channel %s: scans differ\n%+v\n%+v", ch.ID, first.Items(ch.ID), second.Items(ch.ID))
		}
	}
}

// TestScanAbsoluteFile covers a file:// source, whose sidecar names the video
// where it already lives instead of copying it into the library.
func TestScanAbsoluteFile(t *testing.T) {
	captureLogs(t)

	root := t.TempDir()
	video := filepath.Join(root, "local", "steam-engines.mp4")
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(video, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}

	channelDir := ChannelDir(root, "trains")
	if err := os.MkdirAll(channelDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := WriteSidecar(filepath.Join(channelDir, "steam-engines.json"), Sidecar{
		ID:         "steam-engines",
		Title:      "Steam Engines",
		Source:     "file://" + video,
		File:       video,
		Duration:   90.25,
		IngestedAt: mustTime(t, "2026-09-23T02:11:00Z"),
		Status:     StatusOK,
	})
	if err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	index, err := Scan(root, []Channel{{ID: "trains", Number: 3, Name: "Train TV"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	items := index.Items("trains")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Path != video {
		t.Errorf("path = %q, want %q", items[0].Path, video)
	}
	if items[0].Duration != 90250*time.Millisecond {
		t.Errorf("duration = %v, want 90.25s", items[0].Duration)
	}
}

func TestScanSkipsUnreadableSidecar(t *testing.T) {
	logs := captureLogs(t)

	root := t.TempDir()
	channelDir := ChannelDir(root, "trains")
	if err := os.MkdirAll(channelDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(channelDir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	index, err := Scan(root, []Channel{{ID: "trains", Number: 3, Name: "Train TV"}})
	if err != nil {
		t.Fatalf("Scan returned an error for one malformed sidecar: %v", err)
	}
	if got := len(index.Items("trains")); got != 0 {
		t.Errorf("got %d items, want 0", got)
	}
	if !strings.Contains(logs.String(), "unreadable") {
		t.Errorf("malformed sidecar was not logged:\n%s", logs.String())
	}
}

func TestChannelDir(t *testing.T) {
	if got, want := ChannelDir("/srv/channel3", "trains"), "/srv/channel3/library/trains"; got != want {
		t.Errorf("ChannelDir = %q, want %q", got, want)
	}
	if got, want := ChannelsDir("/srv/channel3"), "/srv/channel3/channels"; got != want {
		t.Errorf("ChannelsDir = %q, want %q", got, want)
	}
}
