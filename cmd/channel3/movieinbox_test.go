package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tylersavery/channel3/internal/movie"
)

func movieTools(t *testing.T) movie.Tools {
	t.Helper()
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg and ffprobe are not installed")
	}
	return movie.Tools{FFmpeg: ffmpeg, FFprobe: ffprobe}
}

// writeRip makes a two second rip at path.
func writeRip(t *testing.T, tools movie.Tools, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tools.FFmpeg, "-hide_banner", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=2",
		"-f", "lavfi", "-i", "sine=duration=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot make the test rip: %v: %s", err, out)
	}
}

func TestMovieInboxPreparesAndClearsRips(t *testing.T) {
	tools := movieTools(t)
	root := t.TempDir()
	inbox := movieInboxDir(root)
	writeRip(t, tools, filepath.Join(inbox, "Up (2009)", "Up (2009).mkv"))
	srt := filepath.Join(inbox, "Up (2009)", "Up (2009).en.srt")
	if err := os.WriteFile(srt, []byte("1\n00:00:00,500 --> 00:00:01,500\nHi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "Broken (2001).mkv"), []byte("not a movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, ".Ponyo.mkv.Xq3a"), []byte("rsync in flight"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(moviesDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moviesDir(root), "Cut Off.mkv.part"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	newMovieInbox(root, tools, slog.New(slog.NewTextHandler(io.Discard, nil))).Run(context.Background())

	m, err := movie.ReadSidecar(filepath.Join(moviesDir(root), "Up (2009).json"))
	if err != nil {
		t.Fatalf("the movie was not prepared: %v", err)
	}
	if len(m.Subtitles) != 1 {
		t.Errorf("the .srt beside the rip was not added: %+v", m.Subtitles)
	}
	if _, err := os.Stat(filepath.Join(inbox, "Up (2009)")); !os.IsNotExist(err) {
		t.Errorf("the prepared rip's folder should be gone: %v", err)
	}
	for _, name := range []string{"Broken (2001).mkv"} {
		if _, err := os.Stat(filepath.Join(movieFailedDir(root), name)); err != nil {
			t.Errorf("%s was not set aside: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(inbox, ".Ponyo.mkv.Xq3a")); err != nil {
		t.Errorf("a transfer in flight was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moviesDir(root), "Cut Off.mkv.part")); !os.IsNotExist(err) {
		t.Errorf("an unfinished movie was left: %v", err)
	}
}

func TestMovieInboxWithNothingWaiting(t *testing.T) {
	root := t.TempDir()
	// No inbox at all is the normal case on a Pi nobody has dropped a movie on.
	newMovieInbox(root, movie.Tools{}, slog.New(slog.NewTextHandler(io.Discard, nil))).Run(context.Background())
	if _, err := os.Stat(moviesDir(root)); !os.IsNotExist(err) {
		t.Errorf("an empty pass should not create anything: %v", err)
	}
}
