package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/tylersavery/channel3/internal/homevideo"
)

// homeDir and homeOriginalsDir are where prepared home videos and the
// untouched originals live under the root. The Home Movies channel names
// local/home as a folder source.
func homeDir(root string) string          { return filepath.Join(root, "local", "home") }
func homeOriginalsDir(root string) string { return filepath.Join(root, "local", "home-originals") }

// runPrepare converts phone clips into <root>/local/home and moves each
// original into <root>/local/home-originals once its clip is ready.
//
// Arguments are files or folders; a folder contributes the videos directly in
// it. Each clip is converted on its own, so one bad file is reported and the
// rest carry on. It exits 0 when every clip was prepared, 2 when any failed,
// and 1 for a usage error.
//
// The originals are kept so a clip can be prepared again if the look changes.
// Prepare does not touch the library: the next ingest picks the new clips up.
func runPrepare(g *globals, args []string) error {
	fs := g.flagSet("prepare")
	threads := fs.Int("threads", 0, "encoder threads; 0 lets ffmpeg decide, 2 leaves room for playback")
	ffmpeg := fs.String("ffmpeg", "ffmpeg", "path to the ffmpeg binary")
	ffprobe := fs.String("ffprobe", "ffprobe", "path to the ffprobe binary")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "channel3 prepare: name the clips, or a folder of them")
		fs.Usage()
		return &exitError{code: 1}
	}

	clips, err := collectClips(fs.Args())
	if err != nil {
		return &exitError{code: 1, err: err}
	}
	if len(clips) == 0 {
		fmt.Println("prepare: no video files found")
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tools := homevideo.Tools{FFmpeg: *ffmpeg, FFprobe: *ffprobe, Threads: *threads}
	out, originals := homeDir(g.root), homeOriginalsDir(g.root)
	if err := os.MkdirAll(originals, 0o755); err != nil {
		return &exitError{code: 1, err: err}
	}

	failed := 0
	for i, src := range clips {
		if ctx.Err() != nil {
			return &exitError{code: 1, err: errors.New("interrupted; the clips already prepared are kept")}
		}
		dst, err := tools.Prepare(ctx, src, out)
		if err != nil {
			failed++
			fmt.Printf("failed %d/%d %s: %v\n", i+1, len(clips), filepath.Base(src), err)
			continue
		}
		if err := moveOriginal(src, originals); err != nil {
			fmt.Printf("ok     %d/%d %s -> %s (the original stays where it was: %v)\n",
				i+1, len(clips), filepath.Base(src), filepath.Base(dst), err)
			continue
		}
		fmt.Printf("ok     %d/%d %s -> %s\n", i+1, len(clips), filepath.Base(src), filepath.Base(dst))
	}

	fmt.Printf("prepare: %d ok, %d failed\n", len(clips)-failed, failed)
	if failed > 0 {
		return &exitError{code: 2, err: fmt.Errorf("%d clip(s) failed", failed)}
	}
	return nil
}

// collectClips expands the arguments into video files, in name order within a
// folder and argument order across them.
func collectClips(args []string) ([]string, error) {
	var clips []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			clips = append(clips, arg)
			continue
		}
		entries, err := os.ReadDir(arg)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			if !e.IsDir() && e.Name()[0] != '.' && homevideo.IsVideo(e.Name()) {
				found = append(found, filepath.Join(arg, e.Name()))
			}
		}
		slices.Sort(found)
		clips = append(clips, found...)
	}
	return clips, nil
}

// moveOriginal moves a prepared clip's source into dir, keeping its name and
// never overwriting one already there.
func moveOriginal(src, dir string) error {
	dst := filepath.Join(dir, filepath.Base(src))
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists in %s", filepath.Base(src), dir)
	}
	return os.Rename(src, dst)
}
