package homevideo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseProbe(t *testing.T) {
	cases := []struct {
		name string
		json string
		want Clip
	}{
		{
			name: "iphone portrait, rotation in side data, HLG, local creation date",
			json: `{"streams":[{"codec_type":"video","width":1920,"height":1080,"avg_frame_rate":"30/1","color_transfer":"arib-std-b67","side_data_list":[{"rotation":-90}]},{"codec_type":"audio"}],
				"format":{"tags":{"creation_time":"2025-06-14T18:03:09.000000Z","com.apple.quicktime.creationdate":"2025-06-14T14:03:09-0400"}}}`,
			want: Clip{Width: 1080, Height: 1920, FPS: 30, HDR: true, HasAudio: true,
				Recorded: time.Date(2025, 6, 14, 14, 3, 9, 0, time.FixedZone("", -4*3600))},
		},
		{
			name: "landscape, old rotate tag of 180, no audio, UTC time only",
			json: `{"streams":[{"codec_type":"video","width":1280,"height":720,"avg_frame_rate":"30000/1001","color_transfer":"bt709","tags":{"rotate":"180"}}],
				"format":{"tags":{"creation_time":"2024-01-02T03:04:05Z"}}}`,
			want: Clip{Width: 1280, Height: 720, FPS: 30000.0 / 1001, Recorded: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		},
		{
			name: "PQ HDR, rotate tag of 270, no dates",
			json: `{"streams":[{"codec_type":"video","width":3840,"height":2160,"avg_frame_rate":"60/1","color_transfer":"smpte2084","tags":{"rotate":"270"}}],"format":{}}`,
			want: Clip{Width: 2160, Height: 3840, FPS: 60, HDR: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProbe([]byte(tc.json))
			if err != nil {
				t.Fatalf("parseProbe: %v", err)
			}
			if !got.Recorded.Equal(tc.want.Recorded) {
				t.Errorf("recorded %v, want %v", got.Recorded, tc.want.Recorded)
			}
			got.Recorded, tc.want.Recorded = time.Time{}, time.Time{}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseProbeWithoutVideoIsAnError(t *testing.T) {
	if _, err := parseProbe([]byte(`{"streams":[{"codec_type":"audio"}],"format":{}}`)); err == nil {
		t.Error("a file with no video stream parsed without error")
	}
}

// TestPortraitFilterFillsTheFrame is the arithmetic that matters: the sides
// and the clip add up to exactly 1920, every width is even, and the side
// colour is smoothed over about three seconds at the clip's own frame rate.
func TestPortraitFilterFillsTheFrame(t *testing.T) {
	for _, clip := range []Clip{
		{Width: 1080, Height: 1920, FPS: 30},
		{Width: 720, Height: 1280, FPS: 60},
		{Width: 1080, Height: 1350, FPS: 24},
		{Width: 607, Height: 1080, FPS: 29.97},
	} {
		f := Filter(clip)
		fg := evenRound(1080 * float64(clip.Width) / float64(clip.Height))
		left := evenRound(float64(1920-fg) / 2)
		right := 1920 - fg - left
		for _, w := range []int{fg, left, right} {
			if w%2 != 0 {
				t.Errorf("%dx%d: width %d is odd", clip.Width, clip.Height, w)
			}
		}
		if !strings.Contains(f, "hstack=3") {
			t.Errorf("%dx%d: portrait filter has no sides: %s", clip.Width, clip.Height, f)
		}
		if want := "tmix=frames=" + strconv.Itoa(smoothingFrames(clip.FPS)); !strings.Contains(f, want) {
			t.Errorf("%dx%d: filter lacks %s", clip.Width, clip.Height, want)
		}
	}
}

func TestLandscapeFilterHasNoSides(t *testing.T) {
	f := Filter(Clip{Width: 1920, Height: 1080, FPS: 30})
	if strings.Contains(f, "hstack") || strings.Contains(f, "tmix") {
		t.Errorf("a landscape clip got sides: %s", f)
	}
}

func TestHDRIsToneMapped(t *testing.T) {
	if f := Filter(Clip{Width: 1080, Height: 1920, FPS: 30, HDR: true}); !strings.Contains(f, "tonemap=") {
		t.Errorf("an HDR clip was not tone mapped: %s", f)
	}
	if f := Filter(Clip{Width: 1080, Height: 1920, FPS: 30}); strings.Contains(f, "tonemap=") {
		t.Errorf("an SDR clip was tone mapped: %s", f)
	}
}

func TestSmoothingFrames(t *testing.T) {
	cases := map[float64]int{30: 90, 60: 180, 0: 90, 29.97: 90, 1000: 1024}
	for fps, want := range cases {
		if got := smoothingFrames(fps); got != want {
			t.Errorf("smoothingFrames(%v) = %d, want %d", fps, got, want)
		}
	}
}

func TestFreeNameNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	first, err := freeName(dir, "2025-06-14 14.03")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := freeName(dir, "2025-06-14 14.03")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "2025-06-14 14.03 2.mp4" {
		t.Errorf("second clip from the same minute is %q, want the 2 suffix", filepath.Base(second))
	}
}

// tools returns real ffmpeg and ffprobe, or skips the test.
func tools(t *testing.T) Tools {
	t.Helper()
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg and ffprobe are not installed")
	}
	return Tools{FFmpeg: ffmpeg, FFprobe: ffprobe}
}

// makeClip renders a two second test clip with ffmpeg's own test pattern.
func makeClip(t *testing.T, tl Tools, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	base := []string{"-hide_banner", "-v", "error", "-y"}
	cmd := exec.Command(tl.FFmpeg, append(append(base, args...), path)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot make the test clip %s with this ffmpeg: %v: %s", name, err, out)
	}
	return path
}

// TestPrepareRealClips runs the whole thing through real ffmpeg: a portrait
// clip with sound, a landscape clip stored sideways with a rotation flag the
// way a phone does it, and an HDR clip.
func TestPrepareRealClips(t *testing.T) {
	tl := tools(t)
	ctx := context.Background()

	portrait := makeClip(t, tl, "portrait.mp4",
		"-f", "lavfi", "-i", "testsrc2=size=360x640:rate=30:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	sideways := makeClip(t, tl, "sideways.mp4",
		"-display_rotation", "90",
		"-i", makeClip(t, tl, "landscape.mp4", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30:duration=2", "-c:v", "libx264", "-pix_fmt", "yuv420p"),
		"-c", "copy")
	hdr := makeClip(t, tl, "hdr.mp4",
		"-f", "lavfi", "-i", "testsrc2=size=360x640:rate=30:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p10le",
		"-color_primaries", "bt2020", "-color_trc", "arib-std-b67", "-colorspace", "bt2020nc")

	cases := []struct {
		name, src string
		audio     bool
	}{
		{"portrait", portrait, true},
		{"sideways phone clip", sideways, false},
		{"hdr", hdr, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, err := tl.Probe(ctx, tc.src)
			if err != nil {
				t.Fatalf("probe the source: %v", err)
			}
			if !before.Portrait() {
				t.Fatalf("the source reads as %dx%d, want portrait", before.Width, before.Height)
			}
			out, err := tl.Prepare(ctx, tc.src, t.TempDir())
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			after, err := tl.Probe(ctx, out)
			if err != nil {
				t.Fatalf("probe the prepared clip: %v", err)
			}
			if after.Width != 1920 || after.Height != 1080 {
				t.Errorf("prepared clip is %dx%d, want 1920x1080", after.Width, after.Height)
			}
			if after.HDR {
				t.Error("the prepared clip is still HDR")
			}
			if after.HasAudio != tc.audio {
				t.Errorf("prepared clip has audio %v, want %v", after.HasAudio, tc.audio)
			}
			if _, err := os.Stat(out + ".part"); !os.IsNotExist(err) {
				t.Error("the temporary file was left behind")
			}
		})
	}
}
