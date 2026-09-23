package library

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseProbe(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		want    float64
		wantErr string
	}{
		{
			name: "whole seconds",
			data: `{"format":{"duration":"60.000000"}}`,
			want: 60,
		},
		{
			name: "fractional seconds",
			data: `{"format":{"duration":"612.437000"}}`,
			want: 612.437,
		},
		{
			name:    "not available",
			data:    `{"format":{"duration":"N/A"}}`,
			wantErr: "N/A",
		},
		{
			name:    "no format section",
			data:    `{}`,
			wantErr: "no duration",
		},
		{
			name:    "zero",
			data:    `{"format":{"duration":"0.000000"}}`,
			wantErr: "not a length",
		},
		{
			name:    "negative",
			data:    `{"format":{"duration":"-1.0"}}`,
			wantErr: "not a length",
		},
		{
			name:    "not json",
			data:    "ffprobe: command not found",
			wantErr: "decode",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProbe([]byte(tc.data))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one mentioning %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProbe: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFFProbeReadsTheFixtureClip runs the real ffprobe against the committed
// one-second mp4. It touches no network, and is skipped where ffprobe is absent.
func TestFFProbeReadsTheFixtureClip(t *testing.T) {
	if _, err := exec.LookPath(defaultFFProbe); err != nil {
		t.Skipf("ffprobe is not installed: %v", err)
	}

	got, err := FFProbe{}.DurationSeconds(filepath.Join("testdata", tinyMP4))
	if err != nil {
		t.Fatalf("DurationSeconds: %v", err)
	}
	if got < 0.9 || got > 1.1 {
		t.Errorf("duration = %v, want about one second", got)
	}
}

func TestFFProbeReportsAMissingBinary(t *testing.T) {
	_, err := FFProbe{Path: filepath.Join(t.TempDir(), "no-such-ffprobe")}.DurationSeconds("whatever.mp4")
	if err == nil {
		t.Fatal("DurationSeconds accepted a binary that does not exist")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("error %v is not a ToolError", err)
	}
	if toolErr.Tool != "ffprobe" {
		t.Errorf("tool = %q, want ffprobe", toolErr.Tool)
	}
}

func TestFFProbeReportsAnUnreadableFile(t *testing.T) {
	if _, err := exec.LookPath(defaultFFProbe); err != nil {
		t.Skipf("ffprobe is not installed: %v", err)
	}

	_, err := FFProbe{}.DurationSeconds(filepath.Join(t.TempDir(), "gone.mp4"))
	if err == nil {
		t.Fatal("DurationSeconds accepted a file that does not exist")
	}
	if !strings.Contains(err.Error(), "ffprobe") {
		t.Errorf("error %q does not name the tool", err)
	}
}

// TestFFProbeTimesOut proves a probe that never returns becomes a failed item
// rather than a hang, and that the recorded reason names the timeout.
func TestFFProbeTimesOut(t *testing.T) {
	sleeper := sleepBinary(t)

	started := time.Now()
	_, err := FFProbe{Path: sleeper, Timeout: stuckTimeout}.DurationSeconds("whatever.mp4")
	if err == nil {
		t.Fatal("DurationSeconds waited for a binary that never returns")
	}
	if took := time.Since(started); took > killedWithin {
		t.Errorf("DurationSeconds took %s to give up, want the deadline to take the whole process tree", took)
	}
	if !strings.Contains(err.Error(), "timed out after 250ms") {
		t.Errorf("error %q does not name the timeout", err)
	}
	if !strings.Contains(err.Error(), "whatever.mp4") {
		t.Errorf("error %q does not name the file", err)
	}
}
