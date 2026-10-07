package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/movie"
)

// runPrepareMovie prepares rips into <root>/local/movies, or --out, by hand.
//
// On the Pi, serve does this by itself for every rip deploy/ingest.sh moves
// into the movie inbox; this is for trying a rip on the Mac or preparing one
// again. Arguments are files or folders; a folder contributes the rips directly
// in it. The rips are left where they are. It exits 0 when every rip was
// prepared, 2 when any failed, and 1 for a usage error.
func runPrepareMovie(g *globals, args []string) error {
	fs := g.flagSet("prepare-movie")
	out := fs.String("out", "", "where the prepared movies go (default <root>/local/movies)")
	threads := fs.Int("threads", 0, "encoder threads; 0 lets ffmpeg decide, 2 leaves room for playback")
	ffmpeg := fs.String("ffmpeg", "ffmpeg", "path to the ffmpeg binary")
	ffprobe := fs.String("ffprobe", "ffprobe", "path to the ffprobe binary")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "channel3 prepare-movie: name the rips, or a folder of them")
		fs.Usage()
		return &exitError{code: 1}
	}
	if *out == "" {
		*out = moviesDir(g.root)
	}

	var rips []string
	for _, arg := range fs.Args() {
		info, err := os.Stat(arg)
		if err != nil {
			return &exitError{code: 1, err: err}
		}
		if !info.IsDir() {
			rips = append(rips, arg)
			continue
		}
		entries, err := os.ReadDir(arg)
		if err != nil {
			return &exitError{code: 1, err: err}
		}
		for _, e := range entries {
			if !e.IsDir() && e.Name()[0] != '.' && movie.IsMovie(e.Name()) {
				rips = append(rips, filepath.Join(arg, e.Name()))
			}
		}
	}
	if len(rips) == 0 {
		fmt.Println("prepare-movie: no rips found")
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tools := movie.Tools{FFmpeg: *ffmpeg, FFprobe: *ffprobe, Threads: *threads,
		Loudness: library.FFmpegLoudness{Path: *ffmpeg}}

	failed := 0
	for i, src := range rips {
		if ctx.Err() != nil {
			return &exitError{code: 1, err: errors.New("interrupted; the movies already prepared are kept")}
		}
		res, err := tools.Prepare(ctx, src, *out)
		if err != nil {
			failed++
			fmt.Printf("failed %d/%d %s: %v\n", i+1, len(rips), filepath.Base(src), err)
			continue
		}
		how := "copied"
		if res.Movie.Encoded != "" {
			how = "re-encoded (" + res.Movie.Encoded + ")"
		}
		fmt.Printf("ok     %d/%d %s -> %s, %s, %d audio, %d subtitles\n", i+1, len(rips),
			filepath.Base(src), res.Movie.File, how, len(res.Movie.Audio), len(res.Movie.Subtitles))
		for _, w := range res.Warnings {
			fmt.Printf("       warning: %s\n", w)
		}
	}
	fmt.Printf("prepare-movie: %d ok, %d failed\n", len(rips)-failed, failed)
	if failed > 0 {
		return &exitError{code: 2, err: fmt.Errorf("%d rip(s) failed", failed)}
	}
	return nil
}
