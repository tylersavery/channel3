package library

import (
	"bytes"
	"errors"
	"image/color"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
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
	want := []Source{
		{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{URL: "file:///srv/channel3/local/steam-engines.mp4"},
		{URL: "local/branch-line.mp4"},
		{URL: "local/shunting_yard.mp4", Title: "Shunting Yard"},
	}
	if !reflect.DeepEqual(trains.Sources, want) {
		t.Errorf("trains sources = %+v\nwant %+v", trains.Sources, want)
	}
}

// TestLoadChannelsSourceForms is the whole grammar of one sources entry: a URL,
// a root-relative path, and the mapping that gives an item its guide title.
func TestLoadChannelsSourceForms(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		want    Source
		wantErr string
	}{
		{
			name:  "https url",
			entry: "  - https://www.youtube.com/watch?v=dQw4w9WgXcQ\n",
			want:  Source{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		},
		{
			name:  "http url",
			entry: "  - http://example.com/a.mp4\n",
			want:  Source{URL: "http://example.com/a.mp4"},
		},
		{
			name:  "file url",
			entry: "  - file:///srv/channel3/local/a.mp4\n",
			want:  Source{URL: "file:///srv/channel3/local/a.mp4"},
		},
		{
			name:  "root-relative path",
			entry: "  - local/x.mp4\n",
			want:  Source{URL: "local/x.mp4"},
		},
		{
			name:  "root-relative path in a subdirectory",
			entry: "  - local/kids/x.mp4\n",
			want:  Source{URL: "local/kids/x.mp4"},
		},
		{
			name:  "mapping with a title",
			entry: "  - source: local/x.mp4\n    title: Colour Bars\n",
			want:  Source{URL: "local/x.mp4", Title: "Colour Bars"},
		},
		{
			name:  "mapping with a title for a url",
			entry: "  - source: https://example.com/a\n    title: Colour Bars\n",
			want:  Source{URL: "https://example.com/a", Title: "Colour Bars"},
		},
		{
			name:  "mapping with no title",
			entry: "  - source: local/x.mp4\n",
			want:  Source{URL: "local/x.mp4"},
		},
		{
			name:    "path climbing out of the root",
			entry:   "  - ../x.mp4\n",
			wantErr: "sources[0]",
		},
		{
			name:    "path climbing out through a subdirectory",
			entry:   "  - local/../../x.mp4\n",
			wantErr: "sources[0]",
		},
		{
			name:    "unclean path",
			entry:   "  - local/../x.mp4\n",
			wantErr: "sources[0]",
		},
		{
			name:    "absolute path with no scheme",
			entry:   "  - /abs/x.mp4\n",
			wantErr: "sources[0]",
		},
		{
			name:    "unknown scheme",
			entry:   "  - ftp://example.com/a.mp4\n",
			wantErr: "sources[0]",
		},
		{
			name:    "mapping with no source",
			entry:   "  - title: Colour Bars\n",
			wantErr: "must have a source",
		},
		{
			name:    "mapping with an unknown field",
			entry:   "  - source: local/x.mp4\n    name: Colour Bars\n",
			wantErr: "unknown field",
		},
		{
			name:    "entry that is neither a string nor a mapping",
			entry:   "  - - local/x.mp4\n",
			wantErr: "a mapping",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureLogs(t)
			dir := t.TempDir()
			writeConfig(t, dir, "one.yaml", "id: trains\nnumber: 3\nname: Train TV\nsources:\n"+tc.entry)

			channels, err := LoadChannels(dir)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadChannels accepted %q", tc.entry)
				}
				if !strings.Contains(err.Error(), "one.yaml") {
					t.Errorf("error %q does not name the file", err)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q does not mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadChannels rejected %q: %v", tc.entry, err)
			}
			if len(channels) != 1 || len(channels[0].Sources) != 1 {
				t.Fatalf("got %+v, want one channel with one source", channels)
			}
			if got := channels[0].Sources[0]; got != tc.want {
				t.Errorf("source = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestSourceProblemNamesTheRootRelativeForm keeps the config error readable: a
// bare path that is rejected has to say what a good one looks like.
func TestSourceProblemNamesTheRootRelativeForm(t *testing.T) {
	problem := sourceProblem("/srv/channel3/local/a.mp4")
	if problem == "" {
		t.Fatal("an absolute path with no scheme was accepted")
	}
	if !strings.Contains(problem, "root-relative") {
		t.Errorf("problem %q does not describe the root-relative form", problem)
	}
	if problem := sourceProblem("ftp://example.com/a.mp4"); !strings.Contains(problem, "root-relative") {
		t.Errorf("problem %q does not offer the root-relative form", problem)
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

// TestLoadChannelsDuplicateIDAcrossFiles matters because the id names the
// library directory: two channels sharing one would share their media.
func TestLoadChannelsDuplicateIDAcrossFiles(t *testing.T) {
	captureLogs(t)
	dir := t.TempDir()
	writeConfig(t, dir, "trains.yaml", "id: trains\nnumber: 3\nname: Train TV\nsources:\n  - https://example.com/a\n")
	writeConfig(t, dir, "more-trains.yaml", "id: trains\nnumber: 4\nname: More Trains\nsources:\n  - https://example.com/b\n")

	_, err := LoadChannels(dir)
	if err == nil {
		t.Fatal("LoadChannels accepted a duplicate id")
	}
	msg := err.Error()
	for _, want := range []string{"more-trains.yaml", "trains.yaml", "id", "trains"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

// TestLoadChannelsDuplicateInvalidIDReportedOnce checks that a file whose id is
// already invalid is not also reported as a duplicate of the next bad one.
func TestLoadChannelsDuplicateInvalidIDReportedOnce(t *testing.T) {
	captureLogs(t)
	dir := t.TempDir()
	writeConfig(t, dir, "a.yaml", "id: Trains\nnumber: 3\nname: Train TV\nsources: []\n")
	writeConfig(t, dir, "b.yaml", "id: Trains\nnumber: 4\nname: More Trains\nsources: []\n")

	_, err := LoadChannels(dir)
	if err == nil {
		t.Fatal("LoadChannels accepted two invalid ids")
	}
	if got := strings.Count(err.Error(), "already used"); got != 0 {
		t.Errorf("got %d duplicate reports for ids that are invalid anyway:\n%s", got, err)
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
			name:  "absolute path source",
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

func TestLoadChannelsBumper(t *testing.T) {
	captureLogs(t)
	const head = "id: farm\nnumber: 3\nname: FarmTV\nsources: []\n"

	cases := []struct {
		name, bumper string
		want         *Bumper
		wantErr      string
	}{
		{"absent", "", nil, ""},
		{"icon and colour", "bumper:\n  icon: tractor.svg\n  color: \"#E8A33D\"\n",
			&Bumper{Icon: "tractor.svg", Color: color.RGBA{R: 0xE8, G: 0xA3, B: 0x3D, A: 0xff}}, ""},
		{"colour alone", "bumper:\n  color: \"#1a2b3c\"\n",
			&Bumper{Color: color.RGBA{R: 0x1a, G: 0x2b, B: 0x3c, A: 0xff}}, ""},
		{"missing colour", "bumper:\n  icon: tractor.svg\n", nil, "bumper.color"},
		{"short colour", "bumper:\n  color: \"#E8A\"\n", nil, "#RRGGBB"},
		{"named colour", "bumper:\n  color: orange\n", nil, "#RRGGBB"},
		{"icon with a path", "bumper:\n  icon: ../tractor.svg\n  color: \"#E8A33D\"\n", nil, "bumper.icon"},
		{"icon that is not svg", "bumper:\n  icon: tractor.png\n  color: \"#E8A33D\"\n", nil, "bumper.icon"},
		{"unknown field", "bumper:\n  colour: \"#E8A33D\"\n", nil, "bumper.colour"},
		{"not a mapping", "bumper: tractor.svg\n", nil, "must be a mapping"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeConfig(t, dir, "farm.yaml", head+tc.bumper)
			channels, err := LoadChannels(dir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one mentioning %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadChannels: %v", err)
			}
			got := channels[0].Bumper
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("bumper = %+v, want %+v", got, tc.want)
			}
		})
	}
}
