package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// IngestOptions configures one ingest run.
type IngestOptions struct {
	// Root is the config and library root, <root>/library/<channel-id>.
	Root string
	// Channels are the channels to consider, already loaded and validated.
	Channels []Channel
	// Runner expands and downloads source URLs.
	Runner Runner
	// Prober measures downloaded files. Unused by a dry run.
	Prober Prober
	// ChannelID limits the run to one channel. Empty means every channel.
	ChannelID string
	// DryRun expands sources and prints the plan without downloading anything,
	// writing any sidecar, or creating any directory.
	DryRun bool
	// Out receives one line per entry and a final count. Nil means os.Stdout.
	Out io.Writer
	// Now supplies sidecar timestamps. Nil means time.Now.
	Now func() time.Time
}

// Report counts what one run did. An item is in exactly one of these.
type Report struct {
	// OK is items ingested during this run.
	OK int
	// Skipped is items that were already ingested and whose file is still there.
	Skipped int
	// Failed is items that could not be ingested and now have a failed sidecar.
	Failed int
	// Planned is items a dry run would have downloaded.
	Planned int
}

// PIDFile is where serve records its process id, so ingest can refuse to run
// while a broadcast is playing on the same machine.
func PIDFile(root string) string {
	return filepath.Join(root, "serve.pid")
}

// Ingest downloads every configured source into the library and writes a sidecar
// for each item.
//
// One bad item never stops a run. Anything that fails gets a failed sidecar
// naming the reason and is counted in Report.Failed, and the run moves on to the
// next entry. The returned error is reserved for problems with the run itself:
// bad options, a config mistake found before any download, or a broadcast
// already playing.
func Ingest(opts IngestOptions) (Report, error) {
	run, err := newIngestRun(opts)
	if err != nil {
		return Report{}, err
	}

	for _, ch := range run.channels {
		if err := run.ingestChannel(ch); err != nil {
			return run.report, err
		}
	}
	run.printSummary()
	return run.report, nil
}

// ingestRun is the state of one run of Ingest.
type ingestRun struct {
	root     string
	channels []Channel
	runner   Runner
	prober   Prober
	dryRun   bool
	out      io.Writer
	now      func() time.Time
	report   Report
}

// newIngestRun validates the options and everything that can be checked before a
// single byte is downloaded.
func newIngestRun(opts IngestOptions) (*ingestRun, error) {
	if strings.TrimSpace(opts.Root) == "" {
		return nil, errors.New("ingest: no root given")
	}
	if opts.Runner == nil {
		return nil, errors.New("ingest: no runner given")
	}
	if opts.Prober == nil && !opts.DryRun {
		return nil, errors.New("ingest: no prober given")
	}

	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("ingest: resolve root %q: %w", opts.Root, err)
	}

	channels, err := selectChannels(opts.Channels, opts.ChannelID)
	if err != nil {
		return nil, err
	}
	if err := checkLocalSlugs(channels); err != nil {
		return nil, err
	}
	if err := checkNotBroadcasting(root); err != nil {
		return nil, err
	}

	run := &ingestRun{
		root:     root,
		channels: channels,
		runner:   opts.Runner,
		prober:   opts.Prober,
		dryRun:   opts.DryRun,
		out:      opts.Out,
		now:      opts.Now,
	}
	if run.out == nil {
		run.out = os.Stdout
	}
	if run.now == nil {
		run.now = time.Now
	}
	return run, nil
}

// selectChannels applies the --channel filter.
func selectChannels(channels []Channel, channelID string) ([]Channel, error) {
	if channelID == "" {
		return channels, nil
	}
	for _, ch := range channels {
		if ch.ID == channelID {
			return []Channel{ch}, nil
		}
	}
	return nil, fmt.Errorf("ingest: no channel with id %q is configured", channelID)
}

// checkNotBroadcasting refuses to run while serve is playing on this machine.
// Ingest is heavy and runs ffmpeg; a broadcast is not the moment for it.
func checkNotBroadcasting(root string) error {
	path := PIDFile(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ingest: read %s: %w", path, err)
	}

	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil || pid <= 0 {
		slog.Warn("ignoring an unreadable pid file", "path", path, "contents", strings.TrimSpace(string(data)))
		return nil
	}
	if !processAlive(pid) {
		slog.Info("ignoring a stale pid file", "path", path, "pid", pid)
		return nil
	}
	return fmt.Errorf("ingest: serve is broadcasting as pid %d (%s); stop it first", pid, path)
}

// processAlive reports whether pid names a running process. Signal 0 delivers
// nothing and only reports whether the process is there; a permission error
// means it is there and belongs to someone else.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, os.ErrPermission)
}

// checkLocalSlugs reports every pair of file:// sources in one channel that
// would claim the same item id, before anything is downloaded. Two files named
// "Steam Engines.mp4" and "steam-engines.mp4" would otherwise overwrite each
// other's sidecar.
func checkLocalSlugs(channels []Channel) error {
	var problems []error
	for _, ch := range channels {
		claimedBy := make(map[string]string)
		for _, source := range ch.Sources {
			if !isLocalSource(source) {
				continue
			}
			path, err := localPath(source)
			if err != nil {
				continue // Reported per item, with a failed sidecar.
			}
			id := slugify(stem(path))
			if id == "" {
				continue // Same.
			}
			if prior, taken := claimedBy[id]; taken {
				problems = append(problems, fmt.Errorf(
					"channel %s: local sources %q and %q both become item id %q; rename one file",
					ch.ID, prior, source, id))
				continue
			}
			claimedBy[id] = source
		}
	}
	return errors.Join(problems...)
}

// ingestChannel walks one channel's sources in config order.
func (r *ingestRun) ingestChannel(ch Channel) error {
	dir := ChannelDir(r.root, ch.ID)
	for _, source := range ch.Sources {
		var err error
		if isLocalSource(source) {
			err = r.ingestLocal(ch, dir, source)
		} else {
			err = r.ingestRemote(ch, dir, source)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ingestRemote expands one URL and ingests everything it names.
//
// Expansion always runs, even when every video behind the source is already in
// the library, because that is the only way a video added to a playlist since
// the last run is noticed.
func (r *ingestRun) ingestRemote(ch Channel, dir, source string) error {
	entries, err := r.runner.Expand(source)
	if err != nil {
		return r.fail(ch, dir, sourceID(source), source, err)
	}
	if err := r.clearSourceFailure(dir, source); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := r.ingestEntry(ch, dir, source, entry); err != nil {
			return err
		}
	}
	return nil
}

// clearSourceFailure removes the failed sidecar a previous run wrote for a
// source that would not expand. An entry's own failed sidecar is overwritten in
// place when it succeeds, but this one is named after the source rather than any
// video, so nothing else would ever clean it up and the library would show a
// failure that has been fixed.
func (r *ingestRun) clearSourceFailure(dir, source string) error {
	if r.dryRun {
		return nil
	}
	path, err := r.sidecarPath(dir, sourceID(source))
	if err != nil {
		return err
	}
	sidecar, err := ReadSidecar(path)
	if err != nil {
		return nil // Nothing there, or nothing this run should touch.
	}
	if sidecar.Status != StatusFailed || sidecar.Source != source {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove the stale failure record %s: %w", path, err)
	}
	slog.Info("the source expanded again, clearing its failure record", "source", source, "sidecar", path)
	return nil
}

// ingestEntry downloads one video unless it is already in the library.
//
// The id decides the name of both the video file and its sidecar, and it comes
// from whatever the runner reported, so it is checked here before any path is
// built from it.
func (r *ingestRun) ingestEntry(ch Channel, dir, source string, entry Entry) error {
	if !ValidItemID(entry.ID) {
		return r.fail(ch, dir, sourceID(source), source,
			fmt.Errorf("the source offered the item id %q, which %s", entry.ID, itemIDRule))
	}
	if r.alreadyIngested(dir, entry.ID) {
		r.line("skip", ch, entry.ID, "")
		r.report.Skipped++
		return nil
	}
	if r.dryRun {
		r.line("plan", ch, entry.ID, entry.Title)
		r.report.Planned++
		return nil
	}
	if err := r.ensureDir(dir); err != nil {
		return err
	}

	result, err := r.runner.Download(entry.URL, dir)
	if err != nil {
		return r.fail(ch, dir, entry.ID, entry.URL, err)
	}

	duration, err := r.duration(result.Path, result.Duration)
	if err != nil {
		return r.fail(ch, dir, entry.ID, entry.URL, err)
	}

	title := result.Title
	if title == "" {
		title = entry.Title
	}
	file, err := filepath.Rel(dir, result.Path)
	if err != nil || strings.HasPrefix(file, "..") {
		// A download outside its own channel directory should not happen, but
		// an absolute path in the sidecar still plays.
		file = result.Path
	}
	return r.writeOK(ch, dir, Sidecar{
		ID:       entry.ID,
		Title:    title,
		Source:   entry.URL,
		File:     file,
		Duration: duration,
	})
}

// ingestLocal records a file:// source where it already lies. Local video is
// never copied into the library: only its sidecar goes there.
func (r *ingestRun) ingestLocal(ch Channel, dir, source string) error {
	path, err := localPath(source)
	if err != nil {
		return r.fail(ch, dir, sourceID(source), source, err)
	}
	id := slugify(stem(path))
	if id == "" {
		return r.fail(ch, dir, sourceID(source), source,
			fmt.Errorf("file name %q has no letters or digits to make an item id from", filepath.Base(path)))
	}

	if r.alreadyIngested(dir, id) {
		r.line("skip", ch, id, "")
		r.report.Skipped++
		return nil
	}
	if r.dryRun {
		r.line("plan", ch, id, stem(path))
		r.report.Planned++
		return nil
	}
	if !isRegularFile(path) {
		return r.fail(ch, dir, id, source, fmt.Errorf("%s is not a readable file", path))
	}
	if err := r.ensureDir(dir); err != nil {
		return err
	}

	duration, err := r.duration(path, 0)
	if err != nil {
		return r.fail(ch, dir, id, source, err)
	}
	return r.writeOK(ch, dir, Sidecar{
		ID:       id,
		Title:    stem(path),
		Source:   source,
		File:     path,
		Duration: duration,
	})
}

// duration probes path, falling back to yt-dlp's whole-second figure when
// ffprobe cannot read the file. The fallback is logged because it makes the
// schedule's boundaries less exact.
func (r *ingestRun) duration(path string, reported float64) (float64, error) {
	seconds, err := r.prober.DurationSeconds(path)
	if err == nil {
		return seconds, nil
	}
	if reported <= 0 {
		return 0, err
	}
	slog.Warn("ffprobe failed, falling back to the duration yt-dlp reported",
		"path", path, "duration", reported, "err", err)
	return reported, nil
}

// writeOK records a successfully ingested item.
func (r *ingestRun) writeOK(ch Channel, dir string, s Sidecar) error {
	now := r.now().UTC()
	s.IngestedAt = &now
	s.Status = StatusOK
	path, err := r.sidecarPath(dir, s.ID)
	if err != nil {
		return err
	}
	if err := WriteSidecar(path, s); err != nil {
		return err
	}
	r.line("ok", ch, s.ID, s.Title)
	r.report.OK++
	return nil
}

// fail records an item that could not be ingested and lets the run continue. The
// sidecar stays in the library so the failure is visible in the guide rather
// than silent, and the next run retries it.
//
// The returned error is only ever non-nil when the sidecar itself could not be
// written, which is a broken library rather than a broken video.
func (r *ingestRun) fail(ch Channel, dir, id, source string, cause error) error {
	reason := firstLine(cause.Error())
	r.report.Failed++
	r.line("failed", ch, id, reason)
	slog.Warn("ingest failed", "channel", ch.ID, "id", id, "source", source, "err", cause)

	if r.dryRun {
		return nil
	}
	if err := r.ensureDir(dir); err != nil {
		return err
	}
	path, err := r.sidecarPath(dir, id)
	if err != nil {
		return err
	}
	now := r.now().UTC()
	return WriteSidecar(path, Sidecar{
		ID:          id,
		Source:      source,
		Status:      StatusFailed,
		Error:       reason,
		AttemptedAt: &now,
	})
}

// alreadyIngested reports whether this item can be left alone: an ok sidecar
// whose file is still on disk. A failed sidecar is retried, and so is an ok one
// whose file has gone.
func (r *ingestRun) alreadyIngested(dir, id string) bool {
	path, err := r.sidecarPath(dir, id)
	if err != nil {
		return false
	}
	sidecar, err := ReadSidecar(path)
	if err != nil {
		return false
	}
	if sidecar.Status != StatusOK || sidecar.File == "" {
		return false
	}
	media := sidecar.File
	if !filepath.IsAbs(media) {
		media = filepath.Join(dir, media)
	}
	return isRegularFile(media)
}

// sidecarPath is where one item's sidecar lives.
//
// It refuses an id that is not safe to put in a path. Callers validate before
// they get here, so an error from this is a bug rather than bad input, and it
// stops the run instead of writing somewhere unexpected.
func (r *ingestRun) sidecarPath(dir, id string) (string, error) {
	if !ValidItemID(id) {
		return "", fmt.Errorf("refusing to build a library path from the item id %q, which %s", id, itemIDRule)
	}
	return filepath.Join(dir, id+".json"), nil
}

// ensureDir creates a channel's library directory on first write.
func (r *ingestRun) ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create library directory %s: %w", dir, err)
	}
	return nil
}

// line prints one entry's outcome. The verb is padded so a run of a hundred
// items reads as a column.
func (r *ingestRun) line(verb string, ch Channel, id, detail string) {
	if detail == "" {
		fmt.Fprintf(r.out, "%-6s %s/%s\n", verb, ch.ID, id)
		return
	}
	fmt.Fprintf(r.out, "%-6s %s/%s  %s\n", verb, ch.ID, id, detail)
}

// printSummary prints the closing count.
func (r *ingestRun) printSummary() {
	if r.dryRun {
		fmt.Fprintf(r.out, "ingest (dry run): %d to download, %d already ingested, %d failed\n",
			r.report.Planned, r.report.Skipped, r.report.Failed)
		return
	}
	fmt.Fprintf(r.out, "ingest: %d ok, %d skipped, %d failed\n", r.report.OK, r.report.Skipped, r.report.Failed)
}

// localScheme is the prefix of a source that names video already on disk.
const localScheme = "file://"

// isLocalSource reports whether source names a local file rather than something
// to download.
func isLocalSource(source string) bool {
	return strings.HasPrefix(source, localScheme)
}

// localPath turns a file:// source into an absolute path. Only this machine's
// own files are accepted: a host in the URL would name another machine, which
// playback cannot read.
func localPath(source string) (string, error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", source, err)
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", fmt.Errorf("%q names the host %q; only local files can be played", source, parsed.Host)
	}
	if !filepath.IsAbs(parsed.Path) {
		return "", fmt.Errorf("%q is not an absolute path; write it as file:///path/to/video.mp4", source)
	}
	return filepath.Clean(parsed.Path), nil
}

// stem is a file's base name without its extension.
func stem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// itemIDPattern is every id that may be turned into a path in the library.
//
// An id arrives from outside: yt-dlp reports it for a video, and a local file
// contributes its own name. It then becomes a file name, so an id holding a
// slash or a pair of dots would write outside the channel directory. Nothing
// builds a path from an id that does not match this.
var itemIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// itemIDRule finishes the sentence "the item id %q, which ...".
const itemIDRule = "is not a name a library file may have: only letters, digits, dashes and underscores"

// ValidItemID reports whether id is safe to build a library path from.
func ValidItemID(id string) bool {
	return itemIDPattern.MatchString(id)
}

// slugUnsafe matches every run of characters an item id may not contain. The id
// becomes a file name in the library, which is why it stays this narrow.
var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns a local file's name into an item id.
func slugify(name string) string {
	return strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// sourceID names the failed sidecar of a source URL that could not even be
// expanded into entries, since there is no video id to use. Hashing the URL
// keeps the name stable, so a broken source has one sidecar that is retried
// rather than a new one every run.
func sourceID(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "source-" + hex.EncodeToString(sum[:6])
}
