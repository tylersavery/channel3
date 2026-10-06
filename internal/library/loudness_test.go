package library

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const ebur128Summary = `[Parsed_ebur128_0 @ 0x1] Summary:

  Integrated loudness:
    I:         -25.4 LUFS
    Threshold: -35.7 LUFS

  Loudness range:
    LRA:        12.1 LU

  True peak:
    Peak:       -3.1 dBFS
`

func TestParseLoudness(t *testing.T) {
	got, err := parseLoudness([]byte(ebur128Summary))
	if err != nil {
		t.Fatal(err)
	}
	if got != (Loudness{Integrated: -25.4, TruePeak: -3.1}) {
		t.Errorf("loudness = %+v, want -25.4 LUFS and -3.1 dBTP", got)
	}
	silent := []byte("    I:         -inf LUFS\n    Peak:       -inf dBFS\n")
	if _, err := parseLoudness(silent); !errors.Is(err, ErrNoAudio) {
		t.Errorf("silence gave %v, want ErrNoAudio", err)
	}
	if _, err := parseLoudness([]byte("nothing here")); err == nil {
		t.Error("output with no summary parsed")
	}
}

func TestGain(t *testing.T) {
	cases := []struct {
		name string
		in   Loudness
		want float64
	}{
		{"quiet cartoon with room to spare", Loudness{Integrated: -25.4, TruePeak: -12}, 9.4},
		{"quiet but peaky, held below clipping", Loudness{Integrated: -25.4, TruePeak: -3.1}, 2.1},
		{"loud pop song comes down", Loudness{Integrated: -8.2, TruePeak: 0.4}, -7.8},
		{"already at the target", Loudness{Integrated: -16, TruePeak: -1}, 0},
		{"very quiet, capped at mpv's limit", Loudness{Integrated: -40, TruePeak: -30}, 12},
		{"quiet with its peak at full scale, left alone", Loudness{Integrated: -20, TruePeak: 0}, 0},
	}
	for _, tc := range cases {
		if got := Gain(tc.in); got != tc.want {
			t.Errorf("%s: Gain(%+v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestFFmpegMeasuresARealTone generates a tone and a video with no sound and
// measures both with the real ffmpeg.
func TestFFmpegMeasuresARealTone(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	dir := t.TempDir()
	tone := filepath.Join(dir, "tone.m4a")
	silentVideo := filepath.Join(dir, "silent.mp4")
	for _, args := range [][]string{
		// lavfi's sine is an eighth of full scale, a peak of about -18 dBFS.
		{"-f", "lavfi", "-i", "sine=frequency=1000:duration=5", "-c:a", "aac", tone},
		{"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10:duration=2", "-c:v", "libx264", silentVideo},
	} {
		if out, err := exec.Command(ffmpeg, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("cannot make test media: %v: %s", err, out)
		}
	}

	got, err := FFmpegLoudness{Path: ffmpeg}.Loudness(tone)
	if err != nil {
		t.Fatalf("measure the tone: %v", err)
	}
	if got.TruePeak < -19 || got.TruePeak > -15 || got.Integrated < -24 || got.Integrated > -18 {
		t.Errorf("a 1 kHz sine at an eighth of full scale measured %+v, want a peak near -18 dBTP (AAC overshoots a little) and about -21 LUFS", got)
	}
	if _, err := (FFmpegLoudness{Path: ffmpeg}).Loudness(silentVideo); !errors.Is(err, ErrNoAudio) {
		t.Errorf("a video with no audio gave %v, want ErrNoAudio", err)
	}
}

// countingMeasurer measures every file at one level, or reports no audio for
// the paths in silent, and counts its calls.
type countingMeasurer struct {
	silent map[string]bool
	calls  *int
}

func (m countingMeasurer) Loudness(path string) (Loudness, error) {
	*m.calls++
	if m.silent[filepath.Base(path)] {
		return Loudness{}, ErrNoAudio
	}
	return Loudness{Integrated: -20, TruePeak: -6}, nil
}

func TestMeasureMissingFillsOnlyTheGaps(t *testing.T) {
	root := t.TempDir()
	dir := ChannelDir(root, "farm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	measured := -14.0
	for _, s := range []Sidecar{
		{ID: "new", Title: "New", File: "new.mp4", Status: StatusOK},
		{ID: "old", Title: "Old", File: "old.mp4", Status: StatusOK, Loudness: &measured, TruePeak: &measured},
		{ID: "aerial", Title: "Aerial", File: "aerial.mov", Status: StatusOK},
		{ID: "broken", Title: "Broken", Status: StatusFailed},
	} {
		if err := WriteSidecar(filepath.Join(dir, s.ID+".json"), s); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	report, err := MeasureMissing(root, countingMeasurer{silent: map[string]bool{"aerial.mov": true}, calls: &calls}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if report != (MeasureReport{Measured: 1, Silent: 1}) || calls != 2 {
		t.Errorf("report = %+v after %d measurements, want one measured, one silent, two calls", report, calls)
	}
	s, err := ReadSidecar(filepath.Join(dir, "new.json"))
	if err != nil || s.Loudness == nil || *s.Loudness != -20 {
		t.Errorf("the new item's sidecar = %+v, %v, want -20 LUFS written in", s, err)
	}
}
