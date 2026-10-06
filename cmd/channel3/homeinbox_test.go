package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/homevideo"
)

// testInbox returns an inbox under a fresh root with counting ingest and
// rescan hooks.
func testInbox(t *testing.T, tools homevideo.Tools) (*homeInbox, *int, *int) {
	t.Helper()
	ingests, rescans := 0, 0
	h := newHomeInbox(t.TempDir(), tools,
		func() error { ingests++; return nil },
		func() { rescans++ },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.now = func() time.Time { return time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC) }
	return h, &ingests, &rescans
}

func TestAcceptKeepsUploadsInsideTheInbox(t *testing.T) {
	h, _, _ := testInbox(t, homevideo.Tools{})

	stored, err := h.Accept("../../etc/IMG 0001.MOV", strings.NewReader("video bytes"))
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if filepath.Base(stored) != stored || !strings.HasSuffix(stored, "IMG 0001.MOV") {
		t.Errorf("stored as %q, want a plain name ending in the upload's base name", stored)
	}
	data, err := os.ReadFile(filepath.Join(homeInboxDir(h.root), stored))
	if err != nil || string(data) != "video bytes" {
		t.Errorf("the inbox holds %q, %v, want the uploaded bytes", data, err)
	}
	select {
	case <-h.wake:
	default:
		t.Error("an upload did not wake the worker")
	}

	if _, err := h.Accept("notes.txt", strings.NewReader("not a video")); err == nil {
		t.Error("a file that is not a video was accepted")
	}
}

func TestBrokenUploadIsSetAside(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg and ffprobe are not installed")
	}
	h, ingests, rescans := testInbox(t, homevideo.Tools{FFmpeg: ffmpeg, FFprobe: ffprobe})
	stored, err := h.Accept("broken.mov", strings.NewReader("this is not a video"))
	if err != nil {
		t.Fatal(err)
	}

	h.processAll(context.Background())

	if _, err := os.Stat(filepath.Join(homeFailedDir(h.root), stored)); err != nil {
		t.Errorf("the broken clip was not set aside: %v", err)
	}
	if left := h.waiting(); len(left) != 0 {
		t.Errorf("the inbox still holds %v, want the broken clip gone so it is not retried", left)
	}
	if *ingests != 0 || *rescans != 0 {
		t.Errorf("nothing was prepared but ingest ran %d and rescan %d times", *ingests, *rescans)
	}
}

func TestUploadedClipReachesHome(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg and ffprobe are not installed")
	}
	src := filepath.Join(t.TempDir(), "IMG_0002.MOV")
	gen := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=360x640:rate=30:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-f", "mov", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot make a test clip: %v: %s", err, out)
	}
	h, ingests, rescans := testInbox(t, homevideo.Tools{FFmpeg: ffmpeg, FFprobe: ffprobe, Threads: 2, Nice: true})
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stored, err := h.Accept("IMG_0002.MOV", f)
	if err != nil {
		t.Fatal(err)
	}

	h.processAll(context.Background())

	prepared, _ := filepath.Glob(filepath.Join(homeDir(h.root), "*.mp4"))
	if len(prepared) != 1 {
		t.Errorf("local/home holds %v, want the one prepared clip", prepared)
	}
	if _, err := os.Stat(filepath.Join(homeOriginalsDir(h.root), stored)); err != nil {
		t.Errorf("the original was not kept: %v", err)
	}
	if *ingests != 1 || *rescans != 1 {
		t.Errorf("ingest ran %d and rescan %d times, want once each", *ingests, *rescans)
	}
}

func TestRunRemovesUnfinishedUploads(t *testing.T) {
	h, _, _ := testInbox(t, homevideo.Tools{})
	if err := os.MkdirAll(homeInboxDir(h.root), 0o755); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(homeInboxDir(h.root), "cut off.mov.part")
	if err := os.WriteFile(partial, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Run(ctx)
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("an unfinished upload survived the worker starting")
	}
}

func TestUploadedNameDropsTheArrivalStamp(t *testing.T) {
	if got := uploadedName("20261006-090012.908 IMG_9999.MOV"); got != "IMG_9999.MOV" {
		t.Errorf("uploadedName = %q, want the name the phone sent", got)
	}
	if got := uploadedName("IMG_9999.MOV"); got != "IMG_9999.MOV" {
		t.Errorf("uploadedName left %q, want an unstamped name unchanged", got)
	}
}

// TestAcceptNamesWithoutAnExtension is the name an iOS Shortcut offers first:
// IMG_1234 with no .MOV.
func TestAcceptNamesWithoutAnExtension(t *testing.T) {
	h, _, _ := testInbox(t, homevideo.Tools{})
	stored, err := h.Accept("IMG_1234", strings.NewReader("video bytes"))
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if !strings.HasSuffix(stored, "IMG_1234.mov") {
		t.Errorf("stored as %q, want it given a .mov extension", stored)
	}
}
