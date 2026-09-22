package library

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureLogs redirects the default logger into a buffer for the duration of a
// test, so warnings the loader emits can be asserted on.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prior) })
	return &buf
}

// writeConfig writes one channel config file into dir.
func writeConfig(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestLoadChannelsValid(t *testing.T) {
	captureLogs(t)

	channels, err := LoadChannels(filepath.Join("testdata", "channels"))
	if err != nil {
		t.Fatalf("LoadChannels: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("got %d channels, want 2: %+v", len(channels), channels)
	}

	// Sorted by number, so trains (3) comes before space (12).
	if channels[0].ID != "trains" || channels[1].ID != "space" {
		t.Errorf("got order %s, %s, want trains, space", channels[0].ID, channels[1].ID)
	}
	trains := channels[0]
	if trains.Number != 3 {
		t.Errorf("trains number = %d, want 3", trains.Number)
	}
	if trains.Name != "Train TV" {
		t.Errorf("trains name = %q, want %q", trains.Name, "Train TV")
	}
	if len(trains.Sources) != 2 {
		t.Fatalf("trains has %d sources, want 2", len(trains.Sources))
	}
	if trains.Sources[1] != "file:///srv/channel3/local/steam-engines.mp4" {
		t.Errorf("trains second source = %q", trains.Sources[1])
	}
}

func TestLoadChannelsDuplicateNumberAcrossFiles(t *testing.T) {
	captureLogs(t)
	dir := t.TempDir()
	writeConfig(t, dir, "trains.yaml", "id: trains\nnumber: 3\nname: Train TV\nsources:\n  - https://example.com/a\n")
	writeConfig(t, dir, "space.yaml", "id: space\nnumber: 3\nname: Space Channel\nsources:\n  - https://example.com/b\n")

	_, err := LoadChannels(dir)
	if err == nil {
		t.Fatal("LoadChannels accepted a duplicate number")
	}
	msg := err.Error()
	for _, want := range []string{"space.yaml", "trains.yaml", "number", "3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestLoadChannelsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		field string
	}{
		{
			name:  "uppercase id",
			body:  "id: Trains\nnumber: 3\nname: Train TV\nsources: []\n",
			field: "id",
		},
		{
			name:  "id starts with a dash",
			body:  "id: -trains\nnumber: 3\nname: Train TV\nsources: []\n",
			field: "id",
		},
		{
			name:  "number below range",
			body:  "id: trains\nnumber: 0\nname: Train TV\nsources: []\n",
			field: "number",
		},
		{
			name:  "number above range",
			body:  "id: trains\nnumber: 1000\nname: Train TV\nsources: []\n",
			field: "number",
		},
		{
			name:  "missing name",
			body:  "id: trains\nnumber: 3\nsources: []\n",
			field: "name",
		},
		{
			name:  "blank name",
			body:  "id: trains\nnumber: 3\nname: \"   \"\nsources: []\n",
			field: "name",
		},
		{
			name:  "unknown source scheme",
			body:  "id: trains\nnumber: 3\nname: Train TV\nsources:\n  - ftp://example.com/a.mp4\n",
			field: "sources[0]",
		},
		{
			name:  "bare path source",
			body:  "id: trains\nnumber: 3\nname: Train TV\nsources:\n  - /srv/channel3/local/a.mp4\n",
			field: "sources[0]",
		},
		{
			name:  "unknown field",
			body:  "id: trains\nnumber: 3\nname: Train TV\nsourses:\n  - https://example.com/a\n",
			field: "sourses",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureLogs(t)
			dir := t.TempDir()
			writeConfig(t, dir, "broken.yaml", tc.body)

			_, err := LoadChannels(dir)
			if err == nil {
				t.Fatalf("LoadChannels accepted %s", tc.name)
			}
			msg := err.Error()
			if !strings.Contains(msg, "broken.yaml") {
				t.Errorf("error %q does not name the file", msg)
			}
			if !strings.Contains(msg, tc.field) {
				t.Errorf("error %q does not name the field %q", msg, tc.field)
			}
		})
	}
}

func TestLoadChannelsReportsEveryProblem(t *testing.T) {
	captureLogs(t)
	dir := t.TempDir()
	writeConfig(t, dir, "broken.yaml", "id: Trains\nnumber: 0\nname: \"\"\nsources:\n  - ftp://example.com/a.mp4\n")

	_, err := LoadChannels(dir)
	if err == nil {
		t.Fatal("LoadChannels accepted a file with four problems")
	}
	msg := err.Error()
	for _, want := range []string{"id", "number", "name", "sources[0]"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %q:\n%s", want, msg)
		}
	}
	if got := strings.Count(msg, "broken.yaml"); got != 4 {
		t.Errorf("got %d problems, want 4:\n%s", got, msg)
	}
}

func TestLoadChannelsZeroSourcesWarns(t *testing.T) {
	logs := captureLogs(t)
	dir := t.TempDir()
	writeConfig(t, dir, "quiet.yaml", "id: quiet\nnumber: 9\nname: Quiet Channel\nsources: []\n")

	channels, err := LoadChannels(dir)
	if err != nil {
		t.Fatalf("LoadChannels rejected a channel with no sources: %v", err)
	}
	if len(channels) != 1 || len(channels[0].Sources) != 0 {
		t.Fatalf("got %+v, want one channel with no sources", channels)
	}
	if !strings.Contains(logs.String(), "no sources") {
		t.Errorf("no warning logged for a channel with no sources:\n%s", logs.String())
	}
}

func TestLoadChannelsEmptyDirectory(t *testing.T) {
	logs := captureLogs(t)

	channels, err := LoadChannels(t.TempDir())
	if err != nil {
		t.Fatalf("LoadChannels on an empty directory: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("got %d channels, want 0", len(channels))
	}
	if !strings.Contains(logs.String(), "no channel config found") {
		t.Errorf("no warning logged for an empty config directory:\n%s", logs.String())
	}
}

func TestLoadChannelsMissingDirectory(t *testing.T) {
	captureLogs(t)

	_, err := LoadChannels(filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("LoadChannels accepted a missing directory")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v does not wrap fs.ErrNotExist", err)
	}
}

// TestExampleConfigLoads keeps the one config file this repository ships honest.
func TestExampleConfigLoads(t *testing.T) {
	captureLogs(t)

	channels, err := LoadChannels(filepath.Join("..", "..", "channels"))
	if err != nil {
		t.Fatalf("channels/example.yaml does not load: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("got %d channels, want only the example", len(channels))
	}
	if channels[0].ID != "trains" || channels[0].Number != 3 {
		t.Errorf("example channel = %+v", channels[0])
	}
}
