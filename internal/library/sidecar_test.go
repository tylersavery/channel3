package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mustTime parses an RFC 3339 timestamp for a fixture.
func mustTime(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return &parsed
}

// readFixture reads one of the sidecar fixtures under testdata.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sidecars", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestSidecarRoundTripsByteForByte pins the on-disk schema. Phase 2 writes these
// files and Phase 5 reads them, so a change here invalidates every library.
func TestSidecarRoundTripsByteForByte(t *testing.T) {
	for _, name := range []string{"ok.json", "failed.json"} {
		t.Run(name, func(t *testing.T) {
			want := readFixture(t, name)

			sidecar, err := UnmarshalSidecar(want)
			if err != nil {
				t.Fatalf("UnmarshalSidecar: %v", err)
			}
			got, err := MarshalSidecar(sidecar)
			if err != nil {
				t.Fatalf("MarshalSidecar: %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("round trip changed the file.\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestSidecarOKFields(t *testing.T) {
	sidecar, err := UnmarshalSidecar(readFixture(t, "ok.json"))
	if err != nil {
		t.Fatalf("UnmarshalSidecar: %v", err)
	}

	if sidecar.ID != "dQw4w9WgXcQ" {
		t.Errorf("id = %q", sidecar.ID)
	}
	if sidecar.Title != "Steam Engines of the Rockies" {
		t.Errorf("title = %q", sidecar.Title)
	}
	if sidecar.Source != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("source = %q", sidecar.Source)
	}
	if sidecar.File != "dQw4w9WgXcQ.mp4" {
		t.Errorf("file = %q", sidecar.File)
	}
	if sidecar.Duration != 612.437 {
		t.Errorf("duration = %v", sidecar.Duration)
	}
	if sidecar.Status != StatusOK {
		t.Errorf("status = %q", sidecar.Status)
	}
	if sidecar.IngestedAt == nil || !sidecar.IngestedAt.Equal(*mustTime(t, "2026-09-23T02:11:00Z")) {
		t.Errorf("ingested_at = %v", sidecar.IngestedAt)
	}
	if sidecar.Error != "" || sidecar.AttemptedAt != nil {
		t.Errorf("an ok sidecar carries no failure fields: %+v", sidecar)
	}
}

func TestSidecarFailedFields(t *testing.T) {
	sidecar, err := UnmarshalSidecar(readFixture(t, "failed.json"))
	if err != nil {
		t.Fatalf("UnmarshalSidecar: %v", err)
	}

	if sidecar.Status != StatusFailed {
		t.Errorf("status = %q, want failed", sidecar.Status)
	}
	if sidecar.Error != "ERROR: [youtube] aB3dEfGhIjK: Video unavailable" {
		t.Errorf("error = %q", sidecar.Error)
	}
	if sidecar.AttemptedAt == nil || !sidecar.AttemptedAt.Equal(*mustTime(t, "2026-09-23T02:12:30Z")) {
		t.Errorf("attempted_at = %v", sidecar.AttemptedAt)
	}
	if sidecar.File != "" || sidecar.Duration != 0 || sidecar.IngestedAt != nil {
		t.Errorf("a failed sidecar carries no file, duration or ingest time: %+v", sidecar)
	}
}

// TestFailedSidecarOmitsSuccessFields checks the encoder, not just the decoder:
// a failed item must not write an empty file or a zero duration.
func TestFailedSidecarOmitsSuccessFields(t *testing.T) {
	data, err := MarshalSidecar(Sidecar{
		ID:          "aB3dEfGhIjK",
		Source:      "https://www.youtube.com/watch?v=aB3dEfGhIjK",
		Status:      StatusFailed,
		Error:       "ERROR: [youtube] aB3dEfGhIjK: Video unavailable",
		AttemptedAt: mustTime(t, "2026-09-23T02:12:30Z"),
	})
	if err != nil {
		t.Fatalf("MarshalSidecar: %v", err)
	}

	for _, absent := range []string{`"file"`, `"duration"`, `"title"`, `"ingested_at"`} {
		if strings.Contains(string(data), absent) {
			t.Errorf("failed sidecar contains %s:\n%s", absent, data)
		}
	}
	if string(data) != string(readFixture(t, "failed.json")) {
		t.Errorf("failed sidecar does not match the fixture:\n got:\n%s\nwant:\n%s", data, readFixture(t, "failed.json"))
	}
}

func TestSidecarIgnoresUnknownFields(t *testing.T) {
	sidecar, err := UnmarshalSidecar(readFixture(t, "unknown-fields.json"))
	if err != nil {
		t.Fatalf("UnmarshalSidecar: %v", err)
	}

	got, err := MarshalSidecar(sidecar)
	if err != nil {
		t.Fatalf("MarshalSidecar: %v", err)
	}
	want := readFixture(t, "ok.json")
	if string(got) != string(want) {
		t.Errorf("unknown fields were not dropped.\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalSidecarNormalisesTimesToUTC(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("no tzdata for America/Toronto: %v", err)
	}
	local := time.Date(2026, 9, 22, 22, 11, 0, 0, toronto)

	data, err := MarshalSidecar(Sidecar{
		ID:         "dQw4w9WgXcQ",
		Source:     "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		File:       "dQw4w9WgXcQ.mp4",
		Duration:   612.437,
		IngestedAt: &local,
		Status:     StatusOK,
	})
	if err != nil {
		t.Fatalf("MarshalSidecar: %v", err)
	}
	if !strings.Contains(string(data), `"ingested_at": "2026-09-23T02:11:00Z"`) {
		t.Errorf("time was not normalised to UTC:\n%s", data)
	}
}

// TestMarshalSidecarKeepsURLsReadable guards against encoding/json's default
// HTML escaping, which would turn every playlist ampersand into \u0026.
func TestMarshalSidecarKeepsURLsReadable(t *testing.T) {
	const source = "https://www.youtube.com/watch?v=abc&list=PL123"

	data, err := MarshalSidecar(Sidecar{ID: "abc", Source: source, Status: StatusOK})
	if err != nil {
		t.Fatalf("MarshalSidecar: %v", err)
	}
	if !strings.Contains(string(data), source) {
		t.Errorf("source URL was escaped:\n%s", data)
	}
}

func TestWriteAndReadSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dQw4w9WgXcQ.json")
	want := Sidecar{
		ID:         "dQw4w9WgXcQ",
		Title:      "Steam Engines of the Rockies",
		Source:     "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		File:       "dQw4w9WgXcQ.mp4",
		Duration:   612.437,
		IngestedAt: mustTime(t, "2026-09-23T02:11:00Z"),
		Status:     StatusOK,
	}

	if err := WriteSidecar(path, want); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(onDisk) != string(readFixture(t, "ok.json")) {
		t.Errorf("written file does not match the fixture:\n got:\n%s\nwant:\n%s", onDisk, readFixture(t, "ok.json"))
	}

	got, err := ReadSidecar(path)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if got.ID != want.ID || got.Duration != want.Duration || got.Status != want.Status {
		t.Errorf("read back %+v, want %+v", got, want)
	}
	if got.IngestedAt == nil || !got.IngestedAt.Equal(*want.IngestedAt) {
		t.Errorf("ingested_at = %v, want %v", got.IngestedAt, want.IngestedAt)
	}
}

func TestReadSidecarErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := ReadSidecar(filepath.Join(dir, "absent.json")); err == nil {
		t.Error("ReadSidecar accepted a missing file")
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadSidecar(broken); err == nil {
		t.Error("ReadSidecar accepted malformed JSON")
	}
}
