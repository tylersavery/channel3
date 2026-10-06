package main

import (
	"fmt"
	"os"

	"github.com/tylersavery/channel3/internal/library"
)

// runLoudness measures every ingested item that has no loudness yet, so it
// plays levelled with everything else.
//
// New items are measured as they are ingested; this is for a library that
// predates levelling, or an item whose measurement failed. It runs at low
// priority and is safe beside a broadcasting serve. The new levels take effect
// when serve next rescans: a restart, an upload, or the 04:00 rollover.
func runLoudness(g *globals, args []string) error {
	fs := g.flagSet("loudness")
	ffmpeg := fs.String("ffmpeg", "ffmpeg", "path to the ffmpeg binary")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	report, err := library.MeasureMissing(g.root, library.FFmpegLoudness{Path: *ffmpeg, Nice: true}, os.Stdout)
	if err != nil {
		return &exitError{code: 1, err: err}
	}
	fmt.Printf("loudness: %d measured, %d with no sound, %d failed\n", report.Measured, report.Silent, report.Failed)
	if report.Failed > 0 {
		return &exitError{code: 2, err: fmt.Errorf("%d item(s) could not be measured", report.Failed)}
	}
	return nil
}
