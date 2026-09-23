package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/schedule"
)

// writeLibraryItem writes one ok sidecar and the media file it names, so a test
// root has something the index will accept.
func writeLibraryItem(t *testing.T, root, channelID, id string, seconds float64) {
	t.Helper()
	dir := filepath.Join(root, "library", channelID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	media := id + ".mp4"
	if err := os.WriteFile(filepath.Join(dir, media), []byte("not really a video"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	sidecar := map[string]any{
		"id":          id,
		"title":       strings.ToUpper(id),
		"source":      "https://example.invalid/" + id,
		"file":        media,
		"duration":    seconds,
		"ingested_at": "2026-09-22T00:00:00Z",
		"status":      "ok",
	}
	data, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("encode sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// stationRoot builds a root with two channels: one with two playable items and
// one with nothing ingested.
func stationRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeChannelConfig(t, root, "space.yaml",
		"id: space\nnumber: 5\nname: Space TV\nsources:\n  - https://example.invalid/space\n")
	writeChannelConfig(t, root, "trains.yaml",
		"id: trains\nnumber: 3\nname: Train TV\nsources:\n  - https://example.invalid/trains\n")
	writeLibraryItem(t, root, "trains", "aaa", 600)
	writeLibraryItem(t, root, "trains", "bbb", 1200)
	return root
}

func TestLoadStation(t *testing.T) {
	root := stationRoot(t)

	station, err := loadStation(root)
	if err != nil {
		t.Fatalf("loadStation: %v", err)
	}
	if len(station) != 2 {
		t.Fatalf("got %d channels, want 2", len(station))
	}
	if station[0].Number != 3 || station[1].Number != 5 {
		t.Fatalf("channels are not sorted by number: %d then %d", station[0].Number, station[1].Number)
	}
	if station[0].ID != "trains" || station[0].Name != "Train TV" {
		t.Errorf("first channel = %s %q, want trains \"Train TV\"", station[0].ID, station[0].Name)
	}
	if len(station[0].Items) != 2 {
		t.Fatalf("trains has %d items, want 2", len(station[0].Items))
	}
	if len(station[1].Items) != 0 {
		t.Errorf("space has %d items, want none until something is ingested", len(station[1].Items))
	}

	for _, item := range station[0].Items {
		if !filepath.IsAbs(item.Path) {
			t.Errorf("item %s has a relative path %q", item.ID, item.Path)
		}
		if item.Duration <= 0 {
			t.Errorf("item %s has duration %s", item.ID, item.Duration)
		}
		if item.Title == "" {
			t.Errorf("item %s has no title", item.ID)
		}
	}
}

func TestLoadStationMissingConfigIsAnError(t *testing.T) {
	if _, err := loadStation(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("loadStation of a root with no channels directory returned no error")
	}
}

func TestPrintGuide(t *testing.T) {
	clock := schedule.Clock{Location: time.UTC, DayStart: schedule.DefaultDayStart}
	now := time.Date(2026, time.September, 22, 4, 10, 0, 0, time.UTC)
	station := []schedule.Channel{
		{
			ID: "trains", Number: 3, Name: "Train TV",
			Items: []schedule.Item{{ID: "aaa", Title: "Steam Engines", Path: "/library/trains/aaa.mp4", Duration: 30 * time.Minute}},
		},
		{ID: "space", Number: 5, Name: "Space TV"},
	}

	var out strings.Builder
	printGuide(&out, station, now, time.Hour, clock)

	want := strings.Join([]string{
		"  3  Train TV",
		"     04:00  Steam Engines  (remaining 20m)",
		"     04:30  Steam Engines",
		"     05:00  Steam Engines",
		"",
		"  5  Space TV",
		"     (nothing to play: Please Stand By)",
		"",
	}, "\n")

	if got := out.String(); got != want {
		t.Fatalf("guide output:\n%s\nwant:\n%s", got, want)
	}
}

func TestRemainingLabel(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"whole minutes", 20 * time.Minute, "(remaining 20m)"},
		{"rounds up to the next minute", 90 * time.Second, "(remaining 2m)"},
		{"seconds left still read as a minute", 3 * time.Second, "(remaining 1m)"},
		{"nothing left", 0, "(remaining 0m)"},
		{"already past", -time.Minute, "(remaining 0m)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := remainingLabel(tc.in); got != tc.want {
				t.Errorf("remainingLabel(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestGuideExitCodes pins what the command does with bad input: 1 for anything
// it cannot act on, 0 once it has printed.
func TestGuideExitCodes(t *testing.T) {
	root := stationRoot(t)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"prints the whole station", []string{"guide", "--root", root}, 0},
		{"prints one channel", []string{"guide", "--root", root, "--channel", "trains"}, 0},
		{"accepts an instant", []string{"guide", "--root", root, "--at", "2026-09-23T04:00:00Z"}, 0},
		{"unknown flag", []string{"guide", "--root", root, "--nope"}, 1},
		{"unexpected argument", []string{"guide", "--root", root, "trains"}, 1},
		{"hours below one", []string{"guide", "--root", root, "--hours", "0"}, 1},
		{"hours above 48", []string{"guide", "--root", root, "--hours", "49"}, 1},
		{"unparseable instant", []string{"guide", "--root", root, "--at", "yesterday"}, 1},
		{"unknown channel", []string{"guide", "--root", root, "--channel", "nope"}, 1},
		{"missing root", []string{"guide", "--root", filepath.Join(t.TempDir(), "absent")}, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := run(tc.args); got != tc.want {
				t.Errorf("exit code = %d, want %d", got, tc.want)
			}
		})
	}
}
