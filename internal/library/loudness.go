package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/tylersavery/channel3/internal/proc"
)

// TargetLUFS is the integrated loudness every item is brought to on playback.
// It is a common streaming level: loud enough that a quiet cartoon does not
// need the remote, quiet enough that a mastered pop song comes down to meet it.
const TargetLUFS = -16.0

// peakHeadroom is how far below full scale a boosted item's loudest moment is
// kept, in dB, so raising a quiet item never makes it clip.
const peakHeadroom = 1.0

// maxGain is the most an item is raised, which is also the most mpv's
// volume-gain allows.
const maxGain = 12.0

// Loudness is one file's measurement: its integrated loudness in LUFS and its
// true peak in dBTP, per EBU R128.
type Loudness struct {
	Integrated float64
	TruePeak   float64
}

// ErrNoAudio is a file with nothing to measure. It plays at its own level.
var ErrNoAudio = errors.New("no audio to measure")

// Measurer measures a file's loudness.
type Measurer interface {
	Loudness(path string) (Loudness, error)
}

// FFmpegLoudness measures with ffmpeg's ebur128 filter.
type FFmpegLoudness struct {
	// Path is the ffmpeg binary. Empty means "ffmpeg" from PATH.
	Path string
	// Nice measures at the lowest priority, for running beside playback.
	Nice bool
	// Timeout bounds one measurement. Zero means an hour, which a ten hour
	// film measured at a few hundred times real time fits in easily.
	Timeout time.Duration
}

// Loudness decodes the file's first audio stream and returns its integrated
// loudness and true peak.
func (f FFmpegLoudness) Loudness(path string) (Loudness, error) {
	bin := f.Path
	if bin == "" {
		bin = "ffmpeg"
	}
	ctx, cancel := context.WithTimeout(context.Background(), orDuration(f.Timeout, time.Hour))
	defer cancel()

	args := []string{"-nostats", "-hide_banner", "-nostdin", "-i", path,
		"-map", "0:a:0", "-af", "ebur128=peak=true:framelog=quiet", "-f", "null", "-"}
	name := bin
	if f.Nice {
		name, args = "nice", append([]string{"-n", "19", bin}, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	proc.Harden(cmd)
	if err := cmd.Run(); err != nil {
		if bytes.Contains(stderr.Bytes(), []byte("matches no streams")) {
			return Loudness{}, ErrNoAudio
		}
		return Loudness{}, &ToolError{Tool: "ffmpeg", Stderr: lastLines(stderr.String(), 6), Err: err}
	}
	return parseLoudness(stderr.Bytes())
}

// The summary ebur128 prints at the end of a run.
var (
	integratedLine = regexp.MustCompile(`(?m)^\s*I:\s+(-?[0-9.]+|-inf)\s+LUFS`)
	truePeakLine   = regexp.MustCompile(`(?m)^\s*Peak:\s+(-?[0-9.]+|-inf)\s+dBFS`)
)

// parseLoudness reads the summary's integrated loudness and true peak. The
// filter prints a running figure for every frame too, which framelog=quiet
// suppresses, so the last match is the summary's.
func parseLoudness(out []byte) (Loudness, error) {
	i := integratedLine.FindAllSubmatch(out, -1)
	p := truePeakLine.FindAllSubmatch(out, -1)
	if len(i) == 0 || len(p) == 0 {
		return Loudness{}, errors.New("ffmpeg printed no loudness summary")
	}
	integrated, err := strconv.ParseFloat(string(i[len(i)-1][1]), 64)
	if err != nil || math.IsInf(integrated, 0) {
		// -inf is silence, which nothing can be raised to meet.
		return Loudness{}, ErrNoAudio
	}
	peak, err := strconv.ParseFloat(string(p[len(p)-1][1]), 64)
	if err != nil {
		return Loudness{}, fmt.Errorf("true peak %q: %w", p[len(p)-1][1], err)
	}
	return Loudness{Integrated: integrated, TruePeak: peak}, nil
}

// Gain is the change in dB that brings a file measured at l to TargetLUFS,
// lowered if raising it that far would take its true peak within peakHeadroom
// of clipping, and never more than maxGain. Lowering a loud file is never
// limited.
func Gain(l Loudness) float64 {
	gain := TargetLUFS - l.Integrated
	if gain > 0 {
		gain = min(gain, -peakHeadroom-l.TruePeak, maxGain)
		gain = max(gain, 0)
	}
	return math.Round(gain*10) / 10
}

// lastLines keeps the end of a tool's output, where ffmpeg puts its error.
func lastLines(s string, n int) string {
	lines := bytes.Split([]byte(s), []byte("\n"))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return string(bytes.Join(lines, []byte("\n")))
}

// MeasureReport counts what MeasureMissing did.
type MeasureReport struct {
	Measured int // newly measured
	Silent   int // nothing to measure; they play at their own level
	Failed   int // could not be measured
}

// MeasureMissing measures every ingested item under root that has no loudness
// yet and writes it into its sidecar, one line per item to out.
//
// It is how a library ingested before levelling existed catches up, and it is
// safe beside a running serve: it only reads media and rewrites sidecars,
// which serve reads only when it rescans, and every sidecar is replaced whole.
// The new gains take effect at serve's next rescan.
func MeasureMissing(root string, m Measurer, out io.Writer) (MeasureReport, error) {
	var report MeasureReport
	sidecars, err := filepath.Glob(filepath.Join(LibraryDir(root), "*", "*.json"))
	if err != nil {
		return report, err
	}
	for _, path := range sidecars {
		s, err := ReadSidecar(path)
		if err != nil || s.Status != StatusOK || s.File == "" || s.Loudness != nil {
			continue
		}
		media := s.File
		if !filepath.IsAbs(media) {
			media = filepath.Join(filepath.Dir(path), media)
		}
		l, err := m.Loudness(media)
		switch {
		case errors.Is(err, ErrNoAudio):
			report.Silent++
			continue
		case err != nil:
			report.Failed++
			fmt.Fprintf(out, "failed %s: %v\n", s.ID, err)
			continue
		}
		s.Loudness, s.TruePeak = &l.Integrated, &l.TruePeak
		if err := WriteSidecar(path, s); err != nil {
			return report, err
		}
		report.Measured++
		fmt.Fprintf(out, "%6.1f LUFS %+5.1f dB  %s\n", l.Integrated, Gain(l), s.Title)
	}
	return report, nil
}
