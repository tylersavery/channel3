package library

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tylersavery/channel3/internal/proc"
)

// Prober reports the exact duration of a media file, in seconds.
//
// Exactness is the point. The schedule lays items end to end and its boundaries
// are cumulative, so a duration that is a second off moves every item after it.
// yt-dlp reports whole seconds; ffprobe reads the container.
type Prober interface {
	DurationSeconds(path string) (float64, error)
}

// Tags is what a music file says about itself.
type Tags struct {
	Title  string
	Artist string
}

// Tagger reads a file's title and artist tags. FFProbe is one; a Prober that
// is not leaves every file titled by its name.
type Tagger interface {
	Tags(path string) (Tags, error)
}

// defaultFFProbe is the binary name used when no path is configured. It is
// looked up on PATH.
const defaultFFProbe = "ffprobe"

// defaultProbeTimeout bounds one probe. Reading a container's duration is a
// local, near-instant operation, so a couple of minutes means ffprobe is stuck
// on a damaged file rather than working.
const defaultProbeTimeout = 2 * time.Minute

// FFProbe is the real Prober. It shells out to the ffprobe binary and reads
// nothing but the container's duration.
type FFProbe struct {
	// Path is the ffprobe binary. Empty means "ffprobe" from PATH.
	Path string
	// Timeout bounds one probe. Zero means defaultProbeTimeout.
	Timeout time.Duration
}

// DurationSeconds returns the length of the file at path.
func (f FFProbe) DurationSeconds(path string) (float64, error) {
	bin := f.Path
	if bin == "" {
		bin = defaultFFProbe
	}
	timeout := orDuration(f.Timeout, defaultProbeTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var stdout bytes.Buffer
	stderr := &boundedBuffer{limit: maxCapturedStderr}
	cmd := exec.CommandContext(ctx, bin, "-v", "error", "-show_entries", "format=duration", "-of", "json", path)
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	// A probe of a damaged file can leave a child behind. The whole group goes
	// when the deadline does, or the timeout would not bound anything.
	proc.Harden(cmd)

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, &ToolError{Tool: "ffprobe", Err: fmt.Errorf("timed out after %s on %s", timeout, path)}
		}
		return 0, &ToolError{Tool: "ffprobe", Stderr: stderr.String(), Err: err}
	}

	seconds, err := parseProbe(stdout.Bytes())
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	return seconds, nil
}

// Tags returns the title and artist tags of the file at path, either of which
// may be empty. The artist falls back to the album artist.
func (f FFProbe) Tags(path string) (Tags, error) {
	bin := f.Path
	if bin == "" {
		bin = defaultFFProbe
	}
	timeout := orDuration(f.Timeout, defaultProbeTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var stdout bytes.Buffer
	stderr := &boundedBuffer{limit: maxCapturedStderr}
	cmd := exec.CommandContext(ctx, bin, "-v", "error", "-show_entries", "format_tags", "-of", "json", path)
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	proc.Harden(cmd)
	if err := cmd.Run(); err != nil {
		return Tags{}, &ToolError{Tool: "ffprobe", Stderr: stderr.String(), Err: err}
	}
	return parseTags(stdout.Bytes())
}

// parseTags reads title and artist out of ffprobe's format_tags JSON. Tag names
// vary in case between formats, so they are matched without it.
func parseTags(data []byte) (Tags, error) {
	var out struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Tags{}, fmt.Errorf("decode ffprobe tags: %w", err)
	}
	get := func(name string) string {
		for k, v := range out.Format.Tags {
			if strings.EqualFold(k, name) {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	return Tags{Title: get("title"), Artist: cmp.Or(get("artist"), get("album_artist"))}, nil
}

// probeOutput is the shape of ffprobe's -show_entries format=duration JSON. The
// duration arrives as a string, and is "N/A" for a file ffprobe opened but could
// not measure.
type probeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// parseProbe reads a positive duration in seconds out of ffprobe's JSON.
func parseProbe(data []byte) (float64, error) {
	var out probeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return 0, fmt.Errorf("decode ffprobe output: %w", err)
	}
	if out.Format.Duration == "" {
		return 0, fmt.Errorf("ffprobe reported no duration")
	}

	seconds, err := strconv.ParseFloat(out.Format.Duration, 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe reported duration %q: %w", out.Format.Duration, err)
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return 0, fmt.Errorf("ffprobe reported duration %q, which is not a length", out.Format.Duration)
	}
	return seconds, nil
}
