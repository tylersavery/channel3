package movie

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tylersavery/channel3/internal/library"
)

// tools returns ffmpeg tools, or skips the test without them.
func tools(t *testing.T) Tools {
	t.Helper()
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg and ffprobe are not installed")
	}
	return Tools{FFmpeg: ffmpeg, FFprobe: ffprobe, Loudness: library.FFmpegLoudness{Path: ffmpeg}}
}

// makeRip writes a short test rip with ffmpeg: video of the given size and
// pixel format, a commentary track ahead of English audio, and an embedded
// English subtitle.
func makeRip(t *testing.T, tl Tools, path, size, pixFmt string) {
	t.Helper()
	srt := filepath.Join(t.TempDir(), "in.srt")
	if err := os.WriteFile(srt, []byte("1\n00:00:00,500 --> 00:00:01,500\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tl.FFmpeg, "-hide_banner", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size="+size+":rate=24:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=600:duration=3",
		"-i", srt,
		"-map", "0", "-map", "1", "-map", "2", "-map", "3",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", pixFmt,
		"-c:a", "aac", "-c:s", "srt",
		"-metadata:s:a:0", "title=Commentary", "-metadata:s:a:0", "language=eng",
		"-metadata:s:a:1", "language=eng",
		"-metadata:s:s:0", "language=eng",
		path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot make the test rip with this ffmpeg: %v: %s", err, out)
	}
}

func TestPrepareRemuxesARipThatPlays(t *testing.T) {
	tl := tools(t)
	in, out := t.TempDir(), t.TempDir()
	src := filepath.Join(in, "The.Test.Film.2021.1080p.BluRay.mkv")
	makeRip(t, tl, src, "1280x720", "yuv420p")
	if err := os.WriteFile(filepath.Join(in, "The.Test.Film.2021.1080p.BluRay.fr.srt"),
		[]byte("1\n00:00:01,000 --> 00:00:02,000\nBonjour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := tl.Prepare(context.Background(), src, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range res.Warnings {
		t.Errorf("warning: %s", w)
	}
	m := res.Movie
	if m.Name() != "The Test Film (2021)" || m.File != "The Test Film (2021).mkv" || m.Encoded != "" {
		t.Errorf("movie %+v", m)
	}
	if m.Poster != "The Test Film (2021).jpg" || !m.PosterIsFrame {
		t.Errorf("poster %q frame %v", m.Poster, m.PosterIsFrame)
	}
	if m.Loudness == nil || m.Gain() == 0 {
		t.Errorf("loudness %v gain %v", m.Loudness, m.Gain())
	}
	if len(m.Audio) != 2 || m.Audio[0].Comment {
		t.Errorf("the English track should play first, not the commentary: %+v", m.Audio)
	}
	if len(m.Subtitles) != 2 || m.Subtitles[0].Language != "eng" || m.Subtitles[1].Language != "fre" {
		t.Errorf("subtitles %+v", m.Subtitles)
	}

	// The written file agrees with the sidecar, defaults included.
	got, err := tl.Probe(context.Background(), filepath.Join(out, m.File))
	if err != nil {
		t.Fatal(err)
	}
	if got.Audio[0].Comment || !got.Audio[0].Default || got.Audio[1].Default {
		t.Errorf("audio in the file %+v", got.Audio)
	}
	if !got.Subtitles[0].Default || got.Subtitles[1].Default || got.Subtitles[1].Language != "fre" {
		t.Errorf("subtitles in the file %+v", got.Subtitles)
	}

	back, err := ReadSidecar(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name() != m.Name() || back.Duration != m.Duration {
		t.Errorf("sidecar read back %+v", back)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("Prepare should leave the rip alone: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(out, "*.part")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestPrepareEncodesALargeTenBitRip(t *testing.T) {
	tl := tools(t)
	in, out := t.TempDir(), t.TempDir()
	src := filepath.Join(in, "Big (1988).mkv")
	makeRip(t, tl, src, "2560x1080", "yuv420p10le")
	if err := os.WriteFile(filepath.Join(in, "Big (1988).jpg"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := tl.Prepare(context.Background(), src, out)
	if err != nil {
		t.Fatal(err)
	}
	if res.Movie.Encoded == "" {
		t.Error("a 2560 wide 10 bit rip should be re-encoded")
	}
	// The dropped poster is empty, so it fails, and the movie is still made.
	if res.Movie.Poster != "" || len(res.Warnings) != 1 {
		t.Errorf("poster %q warnings %v", res.Movie.Poster, res.Warnings)
	}
	got, err := tl.Probe(context.Background(), filepath.Join(out, "Big (1988).mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Video.Width != 1920 || got.Video.PixFmt != "yuv420p" {
		t.Errorf("video %+v, want 1920 wide 8 bit", got.Video)
	}
}

func TestPrepareRefusesAFileWithoutVideo(t *testing.T) {
	tl := tools(t)
	src := filepath.Join(t.TempDir(), "Song.mkv")
	cmd := exec.Command(tl.FFmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=duration=1", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot make the test file: %v: %s", err, out)
	}
	out := t.TempDir()
	if _, err := tl.Prepare(context.Background(), src, out); err == nil {
		t.Error("want an error")
	}
	if left, _ := os.ReadDir(out); len(left) > 0 {
		t.Errorf("files left behind: %v", left)
	}
}
