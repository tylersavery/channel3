package main

import (
	"fmt"
	"os"

	"github.com/tylersavery/channel3/internal/library"
)

// runIngest downloads every configured source into the library and writes a
// sidecar for each item.
//
// It exits 0 when nothing failed, 2 when any item failed, and 1 for a usage or
// config error. With --beside-broadcast it runs while serve does, for one
// channel at the lowest priority; send serve SIGHUP afterwards to air it. A failed item is never fatal: it leaves a failed sidecar behind
// and the next run retries it.
func runIngest(g *globals, args []string) error {
	fs := g.flagSet("ingest")
	channel := fs.String("channel", "", "only ingest the channel with this id")
	dryRun := fs.Bool("dry-run", false, "expand the sources and print the plan without downloading")
	ytDLP := fs.String("yt-dlp", "yt-dlp", "path to the yt-dlp binary")
	ffprobe := fs.String("ffprobe", "ffprobe", "path to the ffprobe binary")
	ffmpeg := fs.String("ffmpeg", "ffmpeg", "path to the ffmpeg binary, which measures loudness")
	beside := fs.Bool("beside-broadcast", false,
		"run while serve is broadcasting, one channel at the lowest priority, then send serve SIGHUP to put it on air")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *beside && *channel == "" {
		fmt.Fprintln(os.Stderr, "channel3 ingest: --beside-broadcast needs --channel, so one channel downloads beside the broadcast rather than all of them")
		return &exitError{code: 1}
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "channel3 ingest: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return &exitError{code: 1}
	}

	channels, err := library.LoadChannels(library.ChannelsDir(g.root))
	if err != nil {
		return &exitError{code: 1, err: err}
	}

	report, err := library.Ingest(library.IngestOptions{
		Root:      g.root,
		Channels:  channels,
		Runner:    library.YTDLP{Path: *ytDLP, Nice: *beside},
		Prober:    library.FFProbe{Path: *ffprobe},
		Measurer:  library.FFmpegLoudness{Path: *ffmpeg, Nice: *beside},
		ChannelID: *channel,
		DryRun:    *dryRun,
		Out:       os.Stdout,
		// Beside the broadcast, yt-dlp and the loudness pass run under nice,
		// so the television keeps its CPU. It is how the Pi takes new video
		// without going dark; the Mac route needs the Mac to have the room.
		AlongsideBroadcast: *beside,
	})
	if err != nil {
		return &exitError{code: 1, err: err}
	}
	if report.Failed > 0 {
		return &exitError{code: 2, err: fmt.Errorf(
			"%d item(s) failed; the failed sidecars under %s say why",
			report.Failed, library.LibraryDir(g.root))}
	}
	return nil
}
