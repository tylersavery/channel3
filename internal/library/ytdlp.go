package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tylersavery/channel3/internal/proc"
)

// Entry is one video found by expanding a source URL. A plain video URL expands
// to one entry; a playlist expands to its entries in playlist order.
type Entry struct {
	ID    string
	URL   string
	Title string
}

// Result is what a finished download reports back.
type Result struct {
	ID    string
	Title string
	// Path is the file yt-dlp actually produced, after any merge.
	Path string
	// Duration is yt-dlp's own idea of the length in seconds. It is rounded to
	// whole seconds and is only a fallback for when ffprobe cannot read the file.
	Duration float64
}

// Runner expands source URLs into entries and downloads them. It is the seam
// between ingest and yt-dlp: tests supply a fake, and nothing else in this
// package reaches the network.
type Runner interface {
	Expand(source string) ([]Entry, error)
	Download(source, destDir string) (Result, error)
}

// DefaultFormat asks for H.264 video at 1080p or below with AAC audio, which is
// what an ordinary mp4 holds and what plays everywhere.
//
// The Pi decodes H.264 in hardware and nothing else plays smoothly, so the video
// cap is a hard one. The audio preference is softer: YouTube's best audio is
// usually Opus, and Opus inside an mp4 plays in mpv but in little else, which
// makes a library that is awkward to check on a Mac. The second choice takes the
// best audio anyway rather than fail, and the third takes a file that is already
// merged.
const DefaultFormat = "bv*[vcodec^=avc1][height<=1080]+ba[ext=m4a]/" +
	"bv*[vcodec^=avc1][height<=1080]+ba/" +
	"b[height<=1080]"

// defaultYTDLP is the binary name used when no path is configured. It is looked
// up on PATH.
const defaultYTDLP = "yt-dlp"

// Timeouts for the two kinds of yt-dlp call. Expansion reads metadata and should
// take seconds, so a couple of minutes is already a stuck process. A download of
// a long video over a slow connection can legitimately take much longer.
//
// Their purpose is that ingest fails and moves on rather than hanging: an ingest
// that never returns is worse than a failed sidecar, because nothing says so.
const (
	defaultExpandTimeout   = 2 * time.Minute
	defaultDownloadTimeout = 30 * time.Minute
)

// YTDLP is the real Runner. It shells out to the yt-dlp binary and is the only
// thing in Channel Three that opens a network connection.
type YTDLP struct {
	// Path is the yt-dlp binary. Empty means "yt-dlp" from PATH.
	Path string
	// Format overrides the format selector. Empty means DefaultFormat.
	Format string
	// ExpandTimeout bounds one expansion. Zero means defaultExpandTimeout.
	ExpandTimeout time.Duration
	// DownloadTimeout bounds one download. Zero means defaultDownloadTimeout.
	DownloadTimeout time.Duration
}

// Expand lists the videos a source URL stands for, without downloading anything.
//
// The flat dump is deliberately shallow: it asks yt-dlp for the playlist's
// entries rather than each video's full metadata, so expanding a hundred-video
// playlist is one request rather than a hundred.
func (y YTDLP) Expand(source string) ([]Entry, error) {
	stdout, err := y.run(orDuration(y.ExpandTimeout, defaultExpandTimeout), expandArgs(source))
	if err != nil {
		return nil, err
	}
	entries, err := parseEntries(stdout)
	if err != nil {
		return nil, fmt.Errorf("expand %s: %w", source, err)
	}
	return entries, nil
}

// expandArgs is the command line for one expansion.
func expandArgs(source string) []string {
	return []string{
		ignoreConfigFlag,
		"--flat-playlist",
		"--dump-single-json",
		"--no-progress",
		"--", source,
	}
}

// Download fetches one video into destDir as <id>.<ext> and reports where it
// landed.
//
// No .info.json is written. The library index reads every *.json in a channel
// directory as a sidecar, so a stray metadata file would look like a broken one.
func (y YTDLP) Download(source, destDir string) (Result, error) {
	format := y.Format
	if format == "" {
		format = DefaultFormat
	}
	timeout := orDuration(y.DownloadTimeout, defaultDownloadTimeout)
	stdout, err := y.run(timeout, downloadArgs(source, destDir, format))
	if err != nil {
		return Result{}, err
	}

	result, err := parseDownload(stdout)
	if err != nil {
		return Result{}, fmt.Errorf("download %s: %w", source, err)
	}
	if result.Path == "" || !isRegularFile(result.Path) {
		// Older yt-dlp releases report the pre-merge name, so fall back to
		// whatever it left behind under the id it reported.
		path, findErr := locateDownload(destDir, result.ID)
		if findErr != nil {
			return Result{}, fmt.Errorf("download %s: %w", source, findErr)
		}
		result.Path = path
	}
	return result, nil
}

// downloadArgs is the command line for one download.
func downloadArgs(source, destDir, format string) []string {
	return []string{
		ignoreConfigFlag,
		"--format", format,
		"--concurrent-fragments", concurrentFragments,
		"--merge-output-format", "mp4",
		"--no-playlist",
		"--no-overwrites",
		"--no-progress",
		"--no-write-info-json",
		"--print-json",
		"--output", filepath.Join(destDir, "%(id)s.%(ext)s"),
		"--", source,
	}
}

// concurrentFragments is how many pieces of one video yt-dlp downloads at once.
//
// YouTube caps each connection at a few megabytes a second whatever the line
// can carry, so one 1080p60 hour took ten minutes on the Pi's gigabit Ethernet.
// Four connections move the same file several times faster. Playback never
// sees this; it only shortens how long an ingest keeps the television dark.
const concurrentFragments = "4"

// ignoreConfigFlag keeps yt-dlp's own configuration files out of the run.
//
// Channel Three depends on the exact flags it passes. A user config that added
// --write-info-json would drop a .info.json beside every download, which the
// library index would then read as a broken sidecar; one that changed the format
// selector would put video on the Pi that it cannot decode.
const ignoreConfigFlag = "--ignore-config"

// run executes yt-dlp and returns its standard output. A non-zero exit becomes a
// ToolError carrying stderr, which is what ends up in a failed sidecar.
//
// Standard output is collected whole, because one --print-json document runs to
// hundreds of kilobytes. Standard error is capped: a tool failing in a loop can
// produce megabytes of it and only the first lines are ever recorded.
func (y YTDLP) run(timeout time.Duration, args []string) ([]byte, error) {
	bin := y.Path
	if bin == "" {
		bin = defaultYTDLP
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var stdout bytes.Buffer
	stderr := &boundedBuffer{limit: maxCapturedStderr}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	// yt-dlp starts ffmpeg to merge a download. Without this the deadline kills
	// yt-dlp alone and Run waits on the pipes ffmpeg inherited.
	proc.Harden(cmd)

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, &ToolError{Tool: "yt-dlp", Err: fmt.Errorf("timed out after %s", timeout)}
		}
		return nil, &ToolError{Tool: "yt-dlp", Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// orDuration returns value, or fallback when value is not set.
func orDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

// ytdlpDump is the handful of fields Channel Three reads out of yt-dlp's JSON.
// Everything else in those documents, and there is a great deal of it, is
// ignored.
type ytdlpDump struct {
	Type        string      `json:"_type"`
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	WebpageURL  string      `json:"webpage_url"`
	Duration    float64     `json:"duration"`
	Filename    string      `json:"filename"`
	OldFilename string      `json:"_filename"`
	Entries     []ytdlpDump `json:"entries"`
}

// parseEntries turns one --dump-single-json document into entries, flattening
// any nesting a channel or a playlist of playlists produces.
func parseEntries(data []byte) ([]Entry, error) {
	dump, err := decodeDump(data)
	if err != nil {
		return nil, err
	}
	entries := flatten(dump, nil)
	if len(entries) == 0 {
		return nil, fmt.Errorf("yt-dlp returned no videos")
	}
	// One unusable id fails the whole source rather than being dropped quietly,
	// so the problem is visible in the library instead of the channel silently
	// coming up one video short.
	for _, entry := range entries {
		if !ValidItemID(entry.ID) {
			return nil, fmt.Errorf("yt-dlp returned the item id %q, which %s", entry.ID, itemIDRule)
		}
	}
	return entries, nil
}

// flatten walks a dump and appends every video it names.
func flatten(dump ytdlpDump, into []Entry) []Entry {
	if len(dump.Entries) > 0 || dump.Type == "playlist" {
		for _, child := range dump.Entries {
			into = flatten(child, into)
		}
		return into
	}

	url := dump.WebpageURL
	if url == "" {
		url = dump.URL
	}
	if dump.ID == "" || url == "" {
		slog.Warn("skipping a playlist entry with no id or url", "id", dump.ID, "url", url, "title", dump.Title)
		return into
	}
	return append(into, Entry{ID: dump.ID, URL: url, Title: dump.Title})

}

// parseDownload turns the --print-json line yt-dlp writes after a download into
// a Result.
func parseDownload(data []byte) (Result, error) {
	dump, err := decodeDump(data)
	if err != nil {
		return Result{}, err
	}
	if dump.ID == "" {
		return Result{}, fmt.Errorf("yt-dlp reported no id")
	}
	if !ValidItemID(dump.ID) {
		return Result{}, fmt.Errorf("yt-dlp reported the item id %q, which %s", dump.ID, itemIDRule)
	}

	path := dump.Filename
	if path == "" {
		path = dump.OldFilename
	}
	return Result{ID: dump.ID, Title: dump.Title, Path: path, Duration: dump.Duration}, nil
}

// decodeDump reads the first JSON document out of yt-dlp's standard output.
func decodeDump(data []byte) (ytdlpDump, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return ytdlpDump{}, fmt.Errorf("yt-dlp produced no JSON output")
	}
	var dump ytdlpDump
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&dump); err != nil {
		return ytdlpDump{}, fmt.Errorf("decode yt-dlp output: %w", err)
	}
	return dump, nil
}

// locateDownload finds the file yt-dlp wrote for id when its JSON did not name
// one that exists. The output template is <id>.<ext>, so the id is the whole
// base name.
func locateDownload(destDir, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("cannot look for a download with no id")
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return "", fmt.Errorf("read download directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if strings.TrimSuffix(name, ext) != id || ext == ".json" || ext == ".part" {
			continue
		}
		return filepath.Join(destDir, name), nil
	}
	return "", fmt.Errorf("yt-dlp named no file for %q and none was found in %s", id, destDir)
}

// isRegularFile reports whether path is a file that can be played.
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// ToolError is a failure reported by an external tool. Its message is the first
// line of the tool's stderr, which is what a failed sidecar records and what a
// person reads to find out why an item did not ingest.
type ToolError struct {
	Tool   string
	Stderr string
	Err    error
}

func (e *ToolError) Error() string {
	if line := errorLine(e.Stderr); line != "" {
		return fmt.Sprintf("%s: %s", e.Tool, line)
	}
	return fmt.Sprintf("%s: %v", e.Tool, e.Err)
}

func (e *ToolError) Unwrap() error { return e.Err }

// errorPrefix is how yt-dlp and ffmpeg mark the line that says why they gave up.
const errorPrefix = "ERROR:"

// errorLine picks the line of a tool's standard error that a failed sidecar
// should record.
//
// yt-dlp often prints warnings before the line that actually explains the
// failure, so taking the first line would record "unable to extract the
// thumbnail" for a video that is private. The ERROR: line wins wherever it
// appears; the first line is only the fallback for output that has none.
func errorLine(s string) string {
	first := ""
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, errorPrefix) {
			return trimmed
		}
		if first == "" {
			first = trimmed
		}
	}
	return first
}

// firstLine returns the first non-blank line of s, collapsed onto one line.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// maxCapturedStderr is how much of a tool's standard error is kept. Only the
// first lines are ever read, and a tool that fails in a loop can produce far
// more than this.
const maxCapturedStderr = 64 << 10

// boundedBuffer collects the first limit bytes written to it and counts the
// rest, so a runaway tool cannot fill memory with its own complaints.
type boundedBuffer struct {
	limit   int
	buf     bytes.Buffer
	dropped int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	room := min(max(b.limit-b.buf.Len(), 0), len(p))
	if room > 0 {
		if _, err := b.buf.Write(p[:room]); err != nil {
			return 0, err
		}
	}
	b.dropped += len(p) - room
	return len(p), nil
}

// String returns what was kept, saying so when anything was dropped.
func (b *boundedBuffer) String() string {
	if b.dropped == 0 {
		return b.buf.String()
	}
	return fmt.Sprintf("%s\n... %d more bytes of output were dropped", b.buf.String(), b.dropped)
}
