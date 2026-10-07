// Package movie prepares ripped DVDs and Blu-rays for Movie Mode.
//
// A rip is prepared once, when it is added, into a Matroska file the Pi plays
// well: copied stream for stream when its video already plays, re-encoded to
// 1080p H.264 in standard dynamic range when it is 4K, HDR, 10 bit or a codec
// the Pi would struggle with. Every audio and subtitle track is kept, including
// the picture subtitles DVDs and Blu-rays carry, and any .srt file dropped
// beside the rip is added. The tracks are put in order so that what mpv plays
// by default is what this house wants: English audio that is not a commentary,
// and full English subtitles.
//
// Beside each prepared movie go a poster and a JSON sidecar the menu reads.
package movie

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/proc"
)

// Movie is a prepared movie's sidecar, <name>.json beside <name>.mkv.
type Movie struct {
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
	// File and Poster are names in the same directory as the sidecar. Poster
	// is empty when none could be made; the menu then draws the title alone.
	File   string `json:"file"`
	Poster string `json:"poster,omitempty"`
	// PosterIsFrame says the poster is a frame of the film, which the menu
	// draws the title over, rather than a poster that was dropped with it.
	PosterIsFrame bool    `json:"poster_is_frame,omitempty"`
	Duration      float64 `json:"duration"` // seconds
	// Loudness and TruePeak are of the first audio track, which is the one
	// that plays by default. Nil when it could not be measured.
	Loudness  *float64 `json:"loudness,omitempty"`
	TruePeak  *float64 `json:"true_peak,omitempty"`
	Audio     []Track  `json:"audio"`
	Subtitles []Track  `json:"subtitles"`
	// Interlaced asks the player to deinterlace, as DVD video often needs.
	Interlaced bool `json:"interlaced,omitempty"`
	// Encoded says the video was re-encoded, and why.
	Encoded  string    `json:"encoded,omitempty"`
	Source   string    `json:"source"`
	Prepared time.Time `json:"prepared"`
}

// Name is the movie's display name, "Up (2009)".
func (m Movie) Name() string { return Name{Title: m.Title, Year: m.Year}.String() }

// Gain is how far mpv raises or lowers this movie to the house loudness.
func (m Movie) Gain() float64 {
	if m.Loudness == nil || m.TruePeak == nil {
		return 0
	}
	return library.Gain(library.Loudness{Integrated: *m.Loudness, TruePeak: *m.TruePeak})
}

// Tools is where ffmpeg and ffprobe are and how hard they may work.
type Tools struct {
	FFmpeg  string
	FFprobe string
	// Threads caps ffmpeg's threads. Zero lets ffmpeg decide, which is right
	// on the Mac and wrong beside playback on the Pi.
	Threads int
	// Nice runs ffmpeg at the lowest priority, so a re-encode yields the CPU
	// to the broadcast.
	Nice bool
	// Loudness measures the prepared movie. Nil skips the measurement.
	Loudness library.Measurer
}

// Result is a prepared movie and anything that went wrong short of failing.
type Result struct {
	Path  string // the sidecar
	Movie Movie
	// Warnings are steps that failed without failing the movie: a poster
	// that would not render, a loudness that would not measure.
	Warnings []string
}

// Probe reads what MakePlan needs from path.
func (t Tools) Probe(ctx context.Context, path string) (Source, error) {
	cmd := exec.CommandContext(ctx, t.FFprobe, "-v", "error", "-print_format", "json",
		"-show_streams", "-show_format", "--", path)
	proc.Harden(cmd)
	out, err := cmd.Output()
	if err != nil {
		return Source{}, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), toolError(err))
	}
	return parseProbe(out)
}

// Prepare turns the rip at src into <dstDir>/<Title (Year)>.mkv with its
// poster and sidecar, and returns the sidecar.
//
// The movie is written under a temporary name and checked before it takes its
// place: it must last as long as the rip and hold every track the plan kept.
// A movie already prepared under the same name is replaced, so dropping a
// better rip of a film upgrades it. src and the files beside it are left
// alone; deleting them is the caller's decision.
func (t Tools) Prepare(ctx context.Context, src, dstDir string) (Result, error) {
	source, err := t.Probe(ctx, src)
	if err != nil {
		return Result{}, err
	}
	name := ParseName(src)
	external, err := ExternalSubtitles(src)
	if err != nil {
		return Result{}, fmt.Errorf("look for subtitles beside %s: %w", filepath.Base(src), err)
	}
	plan := MakePlan(source, external)

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create %s: %w", dstDir, err)
	}
	dst := filepath.Join(dstDir, name.String()+".mkv")
	tmp := dst + ".part"
	if err := t.run(ctx, Args(source, plan, src, tmp, name.String(), t.Threads)); err != nil {
		os.Remove(tmp)
		return Result{}, fmt.Errorf("ffmpeg %s: %w", filepath.Base(src), err)
	}
	prepared, err := t.verify(ctx, tmp, source, plan)
	if err != nil {
		os.Remove(tmp)
		return Result{}, fmt.Errorf("check %s: %w", filepath.Base(dst), err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return Result{}, fmt.Errorf("place %s: %w", filepath.Base(dst), err)
	}

	res := Result{Path: filepath.Join(dstDir, name.String()+".json")}
	m := Movie{
		Title: name.Title, Year: name.Year,
		File:       filepath.Base(dst),
		Duration:   prepared.Duration,
		Audio:      plan.Audio,
		Subtitles:  plan.Subtitles,
		Interlaced: source.Video.Interlaced(),
		Source:     filepath.Base(src),
		Prepared:   time.Now().UTC().Truncate(time.Second),
	}
	if plan.Encode {
		m.Encoded = plan.Reason
	}

	poster := filepath.Join(dstDir, name.String()+".jpg")
	if given := droppedPoster(src); given != "" {
		err = t.run(ctx, posterArgs(given, poster, 0))
	} else {
		m.PosterIsFrame = true
		err = t.run(ctx, posterArgs(dst, poster, prepared.Duration*0.10))
	}
	if err != nil {
		os.Remove(poster)
		res.Warnings = append(res.Warnings, "poster: "+err.Error())
	} else {
		m.Poster = filepath.Base(poster)
	}

	if t.Loudness != nil {
		l, err := t.Loudness.Loudness(dst)
		switch {
		case err == nil:
			m.Loudness, m.TruePeak = &l.Integrated, &l.TruePeak
		case !errors.Is(err, library.ErrNoAudio):
			res.Warnings = append(res.Warnings, "loudness: "+err.Error())
		}
	}

	if err := writeSidecar(res.Path, m); err != nil {
		return Result{}, err
	}
	res.Movie = m
	return res, nil
}

// run runs ffmpeg, at low priority when asked.
func (t Tools) run(ctx context.Context, args []string) error {
	name := t.FFmpeg
	if t.Nice {
		name, args = "nice", append([]string{"-n", "19", t.FFmpeg}, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	proc.Harden(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(firstLines(string(out), 6)))
	}
	return nil
}

// durationSlack is how far a prepared movie's length may differ from the
// rip's. Copying streams moves the end by a frame or two; anything more means
// ffmpeg stopped early.
func durationSlack(d float64) float64 { return math.Max(2, d*0.005) }

// verify probes a freshly written movie and checks it is whole.
func (t Tools) verify(ctx context.Context, path string, src Source, plan Plan) (Source, error) {
	got, err := t.Probe(ctx, path)
	if err != nil {
		return Source{}, err
	}
	if src.Duration > 0 && math.Abs(got.Duration-src.Duration) > durationSlack(src.Duration) {
		return Source{}, fmt.Errorf("it lasts %.0fs but the rip lasts %.0fs", got.Duration, src.Duration)
	}
	if len(got.Audio) != len(plan.Audio) || len(got.Subtitles) != len(plan.Subtitles) {
		return Source{}, fmt.Errorf("it has %d audio and %d subtitle tracks, want %d and %d",
			len(got.Audio), len(got.Subtitles), len(plan.Audio), len(plan.Subtitles))
	}
	return got, nil
}

// posterExts are the pictures that may be dropped beside a rip as its poster.
var posterExts = []string{".jpg", ".jpeg", ".png", ".JPG", ".JPEG", ".PNG"}

// droppedPoster is the picture beside src with the same name, or "".
func droppedPoster(src string) string {
	base := strings.TrimSuffix(src, filepath.Ext(src))
	for _, ext := range posterExts {
		if _, err := os.Stat(base + ext); err == nil {
			return base + ext
		}
	}
	return ""
}

// posterHeight is the tallest poster kept, which is the whole height of a
// 1080p screen.
const posterHeight = 1080

// posterArgs writes one picture from in to out as a JPEG no taller than
// posterHeight, taken at seconds into the input when it is a film.
func posterArgs(in, out string, seconds float64) []string {
	args := []string{"-hide_banner", "-v", "error", "-nostdin", "-y"}
	if seconds > 0 {
		args = append(args, "-ss", strconv.FormatFloat(seconds, 'f', 1, 64))
	}
	return append(args, "-i", in, "-frames:v", "1",
		"-vf", fmt.Sprintf("scale=-2:'min(%d,ih)'", posterHeight),
		"-q:v", "3", "-update", "1", out)
}

// writeSidecar writes m to path whole or not at all.
func writeSidecar(path string, m Movie) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("place %s: %w", filepath.Base(path), err)
	}
	return nil
}

// ReadSidecar reads a prepared movie's sidecar.
func ReadSidecar(path string) (Movie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Movie{}, err
	}
	var m Movie
	if err := json.Unmarshal(data, &m); err != nil {
		return Movie{}, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return m, nil
}

// VideoExts are the rips Prepare picks up.
var VideoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true}

// IsMovie reports whether path is a rip Prepare picks up.
func IsMovie(path string) bool {
	return VideoExts[strings.ToLower(filepath.Ext(path))]
}

// Companions are the files dropped beside src that belong to it: its .srt
// subtitles and its poster.
func Companions(src string) []string {
	var out []string
	if subs, err := ExternalSubtitles(src); err == nil {
		for _, s := range subs {
			out = append(out, s.External)
		}
	}
	if p := droppedPoster(src); p != "" {
		out = append(out, p)
	}
	return out
}

// toolError adds a tool's stderr to its exit error.
func toolError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(firstLines(string(exit.Stderr), 4)))
	}
	return err
}

// firstLines keeps the start of a tool's output for an error message.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
