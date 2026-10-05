package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSettings puts body in a settings file under a fresh root and returns it.
func writeSettings(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMissingFileIsTheDefaults(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != Default() {
		t.Errorf("got %+v, want the defaults %+v", got, Default())
	}
}

func TestEmptyFileIsTheDefaults(t *testing.T) {
	got, err := Load(writeSettings(t, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != Default() {
		t.Errorf("got %+v, want the defaults %+v", got, Default())
	}
}

// TestPresentFieldsOverrideAndAbsentOnesKeepTheirDefault is the point of the
// pointers in the file shape: turning bumpers off must not also zero the
// channel number's duration.
func TestPresentFieldsOverrideAndAbsentOnesKeepTheirDefault(t *testing.T) {
	root := writeSettings(t, `
channel_number:
  duration: 3s
bumpers:
  enabled: false
`)
	got, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := Default()
	want.ChannelNumber.Duration = 3 * time.Second
	want.Bumper.Enabled = false
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestBadFilesAreErrorsWithTheDefaults(t *testing.T) {
	cases := []struct {
		name, body, wantInErr string
	}{
		{"unknown field", "bumpers:\n  enable: false\n", "enable"},
		{"unknown section", "channel_numbers:\n  enabled: false\n", "channel_numbers"},
		{"not a duration", "bumpers:\n  duration: two\n", "not a duration"},
		{"too short", "channel_number:\n  duration: 10ms\n", "outside"},
		{"too long", "bumpers:\n  duration: 1m\n", "outside"},
		{"not yaml", "bumpers: [\n", "settings.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Load(writeSettings(t, tc.body))
			if err == nil {
				t.Fatal("load succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantInErr)
			}
			if got != Default() {
				t.Errorf("a bad file returned %+v, want the defaults so the station still starts", got)
			}
		})
	}
}
