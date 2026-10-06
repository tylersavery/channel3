package library

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// tinyMP4 is the one-second clip the fake runner hands back instead of
// downloading. It is a real mp4 so the tests that use the real prober have
// something ffprobe can read.
const tinyMP4 = "tiny.mp4"

// tinyMP4Size is the fixture clip's length in bytes, which is the size an
// ingested sidecar should record.
func tinyMP4Size(t *testing.T) int64 {
	t.Helper()
	info, err := os.Stat(filepath.Join("testdata", tinyMP4))
	if err != nil {
		t.Fatalf("stat %s: %v", tinyMP4, err)
	}
	return info.Size()
}

// copyTinyMP4 writes the fixture clip to dest, creating its directory.
func copyTinyMP4(t *testing.T, dest string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", tinyMP4))
	if err != nil {
		t.Fatalf("read %s: %v", tinyMP4, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dest, err)
	}
}

// fakeRunner answers Expand and Download from canned data and records every
// call, so a test can prove that an item already in the library never reached
// the network.
type fakeRunner struct {
	t *testing.T
	// entries answers Expand, keyed by source URL.
	entries map[string][]Entry
	// expandErr fails Expand for a source URL.
	expandErr map[string]error
	// downloadErr fails Download for an entry URL.
	downloadErr map[string]error
	// titles is the title yt-dlp reports at download time, keyed by entry URL.
	// It is what a sidecar's title should come from.
	titles map[string]string
	// duration is the whole-second figure yt-dlp reports, the ffprobe fallback.
	duration float64
	calls    []string
}

func (f *fakeRunner) Expand(source string) ([]Entry, error) {
	f.calls = append(f.calls, "expand "+source)
	if err, failing := f.expandErr[source]; failing {
		return nil, err
	}
	entries, known := f.entries[source]
	if !known {
		f.t.Fatalf("fake runner asked to expand an unconfigured source %q", source)
	}
	return entries, nil
}

func (f *fakeRunner) Download(source, destDir string) (Result, error) {
	f.calls = append(f.calls, "download "+source)
	if err, failing := f.downloadErr[source]; failing {
		return Result{}, err
	}
	entry, known := f.entryFor(source)
	if !known {
		f.t.Fatalf("fake runner asked to download an unconfigured url %q", source)
	}

	path := filepath.Join(destDir, entry.ID+".mp4")
	copyTinyMP4(f.t, path)
	return Result{ID: entry.ID, Title: f.titles[source], Path: path, Duration: f.duration}, nil
}

// entryFor finds the entry a download url belongs to.
func (f *fakeRunner) entryFor(url string) (Entry, bool) {
	for _, entries := range f.entries {
		for _, entry := range entries {
			if entry.URL == url {
				return entry, true
			}
		}
	}
	return Entry{}, false
}

// downloads returns just the download calls, in order.
func (f *fakeRunner) downloads() []string {
	var out []string
	for _, call := range f.calls {
		if after, found := strings.CutPrefix(call, "download "); found {
			out = append(out, after)
		}
	}
	return out
}

// fakeProber answers with canned durations instead of running ffprobe.
type fakeProber struct {
	// seconds overrides the duration for one path.
	seconds map[string]float64
	// fallback is the duration for any path seconds does not cover.
	fallback float64
	// err fails every call.
	err   error
	calls []string
}

func (p *fakeProber) DurationSeconds(path string) (float64, error) {
	p.calls = append(p.calls, path)
	if p.err != nil {
		return 0, p.err
	}
	if seconds, ok := p.seconds[path]; ok {
		return seconds, nil
	}
	return p.fallback, nil
}

// fixedTime is the clock every ingest test runs against, so sidecars compare.
func fixedTime() func() time.Time {
	instant := time.Date(2026, 9, 23, 2, 11, 0, 0, time.UTC)
	return func() time.Time { return instant }
}

// ingestFixture is one prepared ingest run.
type ingestFixture struct {
	root   string
	runner *fakeRunner
	prober *fakeProber
	out    *bytes.Buffer
}

// newIngestFixture prepares a run against a temporary root.
func newIngestFixture(t *testing.T) *ingestFixture {
	t.Helper()
	return &ingestFixture{
		root: t.TempDir(),
		runner: &fakeRunner{
			t:           t,
			entries:     map[string][]Entry{},
			expandErr:   map[string]error{},
			downloadErr: map[string]error{},
			titles:      map[string]string{},
			duration:    60,
		},
		prober: &fakeProber{seconds: map[string]float64{}, fallback: 1.0},
		out:    &bytes.Buffer{},
	}
}

// run performs the ingest.
func (f *ingestFixture) run(channels []Channel, adjust ...func(*IngestOptions)) (Report, error) {
	opts := IngestOptions{
		Root:     f.root,
		Channels: channels,
		Runner:   f.runner,
		Prober:   f.prober,
		Out:      f.out,
		Now:      fixedTime(),
	}
	for _, apply := range adjust {
		apply(&opts)
	}
	return Ingest(opts)
}

// sidecar reads back one item's sidecar.
func (f *ingestFixture) sidecar(t *testing.T, channelID, id string) Sidecar {
	t.Helper()
	s, err := ReadSidecar(filepath.Join(ChannelDir(f.root, channelID), id+".json"))
	if err != nil {
		t.Fatalf("read sidecar %s/%s: %v", channelID, id, err)
	}
	return s
}

// trainsChannel is the channel most of these tests ingest into, with one
// untitled source per url.
func trainsChannel(urls ...string) []Channel {
	sources := make([]Source, 0, len(urls))
	for _, url := range urls {
		sources = append(sources, Source{URL: url})
	}
	return trainsChannelWith(sources...)
}

// trainsChannelWith is the same channel built from sources that may carry a
// configured title.
func trainsChannelWith(sources ...Source) []Channel {
	return []Channel{{ID: "trains", Number: 3, Name: "Train TV", Sources: sources}}
}

func TestIngestSingleVideo(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "from the playlist"}}
	f.runner.titles[source] = "Steam Engines of the Rockies"
	f.prober.fallback = 612.437

	report, err := f.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want one ok", report)
	}

	got := f.sidecar(t, "trains", "zulu001")
	want := Sidecar{
		ID:       "zulu001",
		Title:    "Steam Engines of the Rockies",
		Source:   source,
		File:     "zulu001.mp4",
		Duration: 612.437,
		Size:     tinyMP4Size(t),
		Status:   StatusOK,
	}
	ingestedAt := got.IngestedAt
	got.IngestedAt = nil
	if got != want {
		t.Errorf("sidecar = %+v\nwant %+v", got, want)
	}
	if ingestedAt == nil || !ingestedAt.Equal(fixedTime()()) {
		t.Errorf("ingested_at = %v, want the run's clock", ingestedAt)
	}

	media := filepath.Join(ChannelDir(f.root, "trains"), "zulu001.mp4")
	if !isRegularFile(media) {
		t.Errorf("%s was not written", media)
	}
	if !strings.Contains(f.out.String(), "ok     trains/zulu001") {
		t.Errorf("no ok line printed:\n%s", f.out.String())
	}
	if !strings.Contains(f.out.String(), "ingest: 1 ok, 0 skipped, 0 failed") {
		t.Errorf("no summary printed:\n%s", f.out.String())
	}
}

func TestIngestPlaylistKeepsOrder(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const playlist = "https://www.youtube.com/playlist?list=PL1"
	f.runner.entries[playlist] = []Entry{
		{ID: "sample25", URL: "https://www.youtube.com/watch?v=sample25", Title: "Sapphire Canyon"},
		{ID: "sample24", URL: "https://www.youtube.com/watch?v=sample24", Title: "Comet Geyser"},
		{ID: "sample23", URL: "https://www.youtube.com/watch?v=sample23", Title: "Lefroy Bay"},
	}
	for _, entry := range f.runner.entries[playlist] {
		f.runner.titles[entry.URL] = entry.Title
	}

	report, err := f.run(trainsChannel(playlist))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 3}) {
		t.Errorf("report = %+v, want three ok", report)
	}

	want := []string{
		"https://www.youtube.com/watch?v=sample25",
		"https://www.youtube.com/watch?v=sample24",
		"https://www.youtube.com/watch?v=sample23",
	}
	if got := f.runner.downloads(); !equalStrings(got, want) {
		t.Errorf("downloaded %v, want playlist order %v", got, want)
	}
	for _, entry := range f.runner.entries[playlist] {
		if s := f.sidecar(t, "trains", entry.ID); s.Title != entry.Title {
			t.Errorf("%s title = %q, want %q", entry.ID, s.Title, entry.Title)
		}
	}
}

func TestIngestContinuesAfterAFailedItem(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const playlist = "https://www.youtube.com/playlist?list=PL1"
	broken := "https://www.youtube.com/watch?v=broken01"
	f.runner.entries[playlist] = []Entry{
		{ID: "good0001", URL: "https://www.youtube.com/watch?v=good0001", Title: "Good One"},
		{ID: "broken01", URL: broken, Title: "Broken One"},
		{ID: "good0002", URL: "https://www.youtube.com/watch?v=good0002", Title: "Good Two"},
	}
	f.runner.downloadErr[broken] = &ToolError{
		Tool:   "yt-dlp",
		Stderr: "ERROR: [youtube] broken01: This video is unavailable\n",
		Err:    errors.New("exit status 1"),
	}

	report, err := f.run(trainsChannel(playlist))
	if err != nil {
		t.Fatalf("Ingest returned an error for one bad item: %v", err)
	}
	if report != (Report{OK: 2, Failed: 1}) {
		t.Errorf("report = %+v, want two ok and one failed", report)
	}

	failed := f.sidecar(t, "trains", "broken01")
	if failed.Status != StatusFailed {
		t.Errorf("status = %q, want failed", failed.Status)
	}
	if failed.Error != "yt-dlp: ERROR: [youtube] broken01: This video is unavailable" {
		t.Errorf("error = %q", failed.Error)
	}
	if failed.File != "" || failed.Duration != 0 {
		t.Errorf("a failed sidecar recorded a file or duration: %+v", failed)
	}
	if failed.AttemptedAt == nil {
		t.Error("a failed sidecar has no attempted_at")
	}
	if failed.IngestedAt != nil {
		t.Error("a failed sidecar has an ingested_at")
	}

	for _, id := range []string{"good0001", "good0002"} {
		if s := f.sidecar(t, "trains", id); s.Status != StatusOK {
			t.Errorf("%s status = %q, want ok", id, s.Status)
		}
	}
	if !strings.Contains(f.out.String(), "failed trains/broken01  yt-dlp: ERROR:") {
		t.Errorf("no failed line printed:\n%s", f.out.String())
	}
}

func TestIngestSkipsItemsAlreadyInTheLibrary(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}
	f.runner.titles[source] = "Steam Engines"

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.runner.entries[source] = f.runner.entries[source]

	report, err := second.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{Skipped: 1}) {
		t.Errorf("report = %+v, want one skip", report)
	}
	if got := second.runner.downloads(); len(got) != 0 {
		t.Errorf("an already-ingested item was downloaded again: %v", got)
	}
	if len(second.prober.calls) != 0 {
		t.Errorf("an already-ingested item was probed again: %v", second.prober.calls)
	}
	if !strings.Contains(second.out.String(), "skip   trains/zulu001") {
		t.Errorf("no skip line printed:\n%s", second.out.String())
	}
}

func TestIngestRetriesAFailedSidecar(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}
	f.runner.titles[source] = "Steam Engines"

	dir := ChannelDir(f.root, "trains")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	attempted := fixedTime()()
	err := WriteSidecar(filepath.Join(dir, "zulu001.json"), Sidecar{
		ID:          "zulu001",
		Source:      source,
		Status:      StatusFailed,
		Error:       "yt-dlp: ERROR: temporary",
		AttemptedAt: &attempted,
	})
	if err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	report, runErr := f.run(trainsChannel(source))
	if runErr != nil {
		t.Fatalf("Ingest: %v", runErr)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the failed item retried and ok", report)
	}
	got := f.sidecar(t, "trains", "zulu001")
	if got.Status != StatusOK || got.Error != "" {
		t.Errorf("sidecar was not replaced by a clean one: %+v", got)
	}
}

func TestIngestRedownloadsWhenTheFileIsGone(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}
	f.runner.titles[source] = "Steam Engines"

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if err := os.Remove(filepath.Join(ChannelDir(f.root, "trains"), "zulu001.mp4")); err != nil {
		t.Fatalf("remove media: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.runner.entries[source] = f.runner.entries[source]
	second.runner.titles[source] = "Steam Engines"

	report, err := second.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the missing file downloaded again", report)
	}
	if got := second.runner.downloads(); len(got) != 1 {
		t.Errorf("downloads = %v, want one", got)
	}
}

func TestIngestLocalFileIsRecordedWhereItLies(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	local := filepath.Join(t.TempDir(), "Steam Engines.mp4")
	copyTinyMP4(t, local)
	f.prober.seconds[local] = 90.25

	source := "file://" + local
	report, err := f.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want one ok", report)
	}
	if len(f.runner.calls) != 0 {
		t.Errorf("a local source reached the runner: %v", f.runner.calls)
	}

	got := f.sidecar(t, "trains", "steam-engines")
	if got.File != local {
		t.Errorf("file = %q, want the absolute source path %q", got.File, local)
	}
	if !filepath.IsAbs(got.File) {
		t.Errorf("file %q is not absolute", got.File)
	}
	if got.Duration != 90.25 {
		t.Errorf("duration = %v, want 90.25", got.Duration)
	}
	if got.Title != "Steam Engines" {
		t.Errorf("title = %q, want the file name without its extension", got.Title)
	}

	entries, err := os.ReadDir(ChannelDir(f.root, "trains"))
	if err != nil {
		t.Fatalf("read channel dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "steam-engines.json" {
		t.Errorf("the local file was copied into the library: %v", names(entries))
	}
}

func TestIngestRejectsDuplicateLocalSlugs(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "Steam Engines.mp4")
	second := filepath.Join(dir, "steam-engines.mp4")
	copyTinyMP4(t, first)
	copyTinyMP4(t, second)

	_, err := f.run(trainsChannel("file://"+first, "file://"+second))
	if err == nil {
		t.Fatal("Ingest accepted two local sources with the same slug")
	}
	for _, want := range []string{"trains", "steam-engines", "Steam Engines.mp4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(f.runner.calls) != 0 {
		t.Errorf("the runner was called before the config error was reported: %v", f.runner.calls)
	}
	if len(f.prober.calls) != 0 {
		t.Errorf("the prober was called before the config error was reported: %v", f.prober.calls)
	}
}

func TestIngestLocalFileMissing(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	missing := filepath.Join(t.TempDir(), "gone.mp4")

	report, err := f.run(trainsChannel("file://" + missing))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{Failed: 1}) {
		t.Errorf("report = %+v, want one failed", report)
	}
	got := f.sidecar(t, "trains", "gone")
	if got.Status != StatusFailed || !strings.Contains(got.Error, "not a readable file") {
		t.Errorf("sidecar = %+v", got)
	}
}

func TestIngestExpansionFailureIsStableAcrossRuns(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=nope"
	f.runner.expandErr[source] = &ToolError{
		Tool:   "yt-dlp",
		Stderr: string(readYTDLPFixture(t, "download-error.txt")),
		Err:    errors.New("exit status 1"),
	}

	report, err := f.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{Failed: 1}) {
		t.Errorf("report = %+v, want one failed", report)
	}

	entries, err := os.ReadDir(ChannelDir(f.root, "trains"))
	if err != nil {
		t.Fatalf("read channel dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %v, want one failed sidecar", names(entries))
	}
	first := entries[0].Name()
	if !strings.HasPrefix(first, "source-") {
		t.Errorf("failed sidecar %q is not keyed by a hash of the source url", first)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.runner.expandErr[source] = f.runner.expandErr[source]
	if _, err := second.run(trainsChannel(source)); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	entries, err = os.ReadDir(ChannelDir(f.root, "trains"))
	if err != nil {
		t.Fatalf("read channel dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != first {
		t.Errorf("a second run made a new sidecar: %v", names(entries))
	}
}

// TestIngestClearsAFailureThatIsFixed covers a source that would not expand on
// one run and expands on the next. Its failure sidecar is named after the source
// rather than a video, so nothing else would ever remove it.
func TestIngestClearsAFailureThatIsFixed(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/playlist?list=PL1"
	f.runner.expandErr[source] = errors.New("yt-dlp: ERROR: unable to download webpage")

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	failurePath := filepath.Join(ChannelDir(f.root, "trains"), sourceID(source)+".json")
	if !isRegularFile(failurePath) {
		t.Fatalf("no failure sidecar at %s", failurePath)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.runner.entries[source] = []Entry{
		{ID: "zulu001", URL: "https://www.youtube.com/watch?v=zulu001", Title: "Steam Engines"},
	}

	report, err := second.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want one ok", report)
	}
	if isRegularFile(failurePath) {
		t.Errorf("%s survived a run in which the source expanded", failurePath)
	}

	index, err := Scan(f.root, trainsChannel(source))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := index.Items("trains"); len(got) != 1 {
		t.Errorf("the library holds %+v, want only the ingested item", got)
	}
}

// TestIngestKeepsAnUnrelatedFailureSidecar checks that clearing a source failure
// does not reach past the source it belongs to.
func TestIngestKeepsAnUnrelatedFailureSidecar(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/playlist?list=PL1"
	f.runner.entries[source] = []Entry{
		{ID: "zulu001", URL: "https://www.youtube.com/watch?v=zulu001", Title: "Steam Engines"},
	}

	dir := ChannelDir(f.root, "trains")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	other := filepath.Join(dir, sourceID(source)+".json")
	attempted := fixedTime()()
	err := WriteSidecar(other, Sidecar{
		ID:          sourceID(source),
		Source:      "https://vimeo.com/something-else",
		Status:      StatusFailed,
		Error:       "yt-dlp: ERROR: unsupported url",
		AttemptedAt: &attempted,
	})
	if err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !isRegularFile(other) {
		t.Error("a failure sidecar belonging to a different source was removed")
	}
}

func TestIngestDryRunTouchesNothing(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}

	report, err := f.run(trainsChannel(source), func(o *IngestOptions) {
		o.DryRun = true
		o.Prober = nil
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{Planned: 1}) {
		t.Errorf("report = %+v, want one planned", report)
	}
	if got := f.runner.downloads(); len(got) != 0 {
		t.Errorf("a dry run downloaded %v", got)
	}
	if _, err := os.Stat(ChannelDir(f.root, "trains")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a dry run created the library directory: %v", err)
	}
	out := f.out.String()
	if !strings.Contains(out, "plan   trains/zulu001  Steam Engines") {
		t.Errorf("no plan line printed:\n%s", out)
	}
	if !strings.Contains(out, "ingest (dry run): 1 to download, 0 already ingested, 0 failed") {
		t.Errorf("no dry run summary printed:\n%s", out)
	}
}

func TestIngestChannelFilter(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const trainsSource = "https://www.youtube.com/watch?v=zulu001"
	const spaceSource = "https://www.youtube.com/watch?v=space001"
	f.runner.entries[trainsSource] = []Entry{{ID: "zulu001", URL: trainsSource, Title: "Steam Engines"}}
	f.runner.entries[spaceSource] = []Entry{{ID: "space001", URL: spaceSource, Title: "Orbits"}}

	channels := []Channel{
		{ID: "trains", Number: 3, Name: "Train TV", Sources: []Source{{URL: trainsSource}}},
		{ID: "space", Number: 12, Name: "Space Channel", Sources: []Source{{URL: spaceSource}}},
	}

	report, err := f.run(channels, func(o *IngestOptions) { o.ChannelID = "space" })
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want only the space item", report)
	}
	if _, err := os.Stat(ChannelDir(f.root, "trains")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the filtered-out channel was ingested: %v", err)
	}

	if _, err := f.run(channels, func(o *IngestOptions) { o.ChannelID = "nature" }); err == nil {
		t.Error("Ingest accepted a --channel that is not configured")
	}
}

func TestIngestFallsBackToTheReportedDuration(t *testing.T) {
	logs := captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}
	f.runner.duration = 612
	f.prober.err = errors.New("ffprobe: moov atom not found")

	report, err := f.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want one ok", report)
	}
	if got := f.sidecar(t, "trains", "zulu001").Duration; got != 612 {
		t.Errorf("duration = %v, want yt-dlp's 612", got)
	}
	if !strings.Contains(logs.String(), "falling back") {
		t.Errorf("the fallback was not logged:\n%s", logs.String())
	}
}

func TestIngestFailsWhenNeitherDurationIsAvailable(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}
	f.runner.duration = 0
	f.prober.err = errors.New("ffprobe: moov atom not found")

	report, err := f.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{Failed: 1}) {
		t.Errorf("report = %+v, want one failed", report)
	}
	if got := f.sidecar(t, "trains", "zulu001"); got.Status != StatusFailed || !strings.Contains(got.Error, "moov atom") {
		t.Errorf("sidecar = %+v", got)
	}
}

// TestIngestRefusesAnEntryIDThatEscapesTheChannel covers a runner that reports
// an id built to climb out of the library. The id names both the video file and
// its sidecar, so it is the one piece of outside input that becomes a path.
func TestIngestRefusesAnEntryIDThatEscapesTheChannel(t *testing.T) {
	captureLogs(t)

	for _, badID := range []string{"../../../escaped", "../escaped", "a/b", "..", "with space"} {
		t.Run(badID, func(t *testing.T) {
			sandbox := t.TempDir()
			f := newIngestFixture(t)
			f.root = filepath.Join(sandbox, "root")

			const source = "https://www.youtube.com/playlist?list=PL1"
			f.runner.entries[source] = []Entry{{ID: badID, URL: "https://example.com/a", Title: "Escaped"}}

			report, err := f.run(trainsChannel(source))
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if report != (Report{Failed: 1}) {
				t.Errorf("report = %+v, want one failed record", report)
			}
			if got := f.runner.downloads(); len(got) != 0 {
				t.Errorf("an unusable id was downloaded anyway: %v", got)
			}

			// Everything written must sit inside the one channel directory.
			channelDir := ChannelDir(f.root, "trains")
			var written []string
			walkErr := filepath.WalkDir(sandbox, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					written = append(written, path)
				}
				return nil
			})
			if walkErr != nil {
				t.Fatalf("walk: %v", walkErr)
			}
			if len(written) != 1 {
				t.Fatalf("wrote %v, want one failed sidecar", written)
			}
			if filepath.Dir(written[0]) != channelDir {
				t.Errorf("wrote %q, which is outside %q", written[0], channelDir)
			}
			if filepath.Base(written[0]) != sourceID(source)+".json" {
				t.Errorf("the failure was not keyed by the source url: %q", written[0])
			}

			record, err := ReadSidecar(written[0])
			if err != nil {
				t.Fatalf("ReadSidecar: %v", err)
			}
			if record.Status != StatusFailed || !strings.Contains(record.Error, badID) {
				t.Errorf("record = %+v, want a failure naming the id", record)
			}
		})
	}
}

// TestValidItemID pins the one rule that keeps an id inside its channel.
func TestValidItemID(t *testing.T) {
	for _, id := range []string{"8-X8acD_r38", "dQw4w9WgXcQ", "a", "A_1-2", "source-1efc03051ee0"} {
		if !ValidItemID(id) {
			t.Errorf("ValidItemID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "..", "../a", "a/b", `a\b`, "a b", "a.mp4", "a;b", "a\nb"} {
		if ValidItemID(id) {
			t.Errorf("ValidItemID(%q) = true, want false", id)
		}
	}
}

// TestProcessAlive covers the check that keeps ingest from running during a
// broadcast: a live process and one that has exited.
func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("processAlive says this very process is not running")
	}

	child := exec.Command("/bin/sh", "-c", "exit 0")
	if err := child.Start(); err != nil {
		t.Fatalf("start a child process: %v", err)
	}
	pid := child.Process.Pid
	if err := child.Wait(); err != nil {
		t.Fatalf("wait for the child process: %v", err)
	}
	if processAlive(pid) {
		t.Errorf("processAlive says pid %d is running after it exited", pid)
	}
}

func TestIngestRefusesWhileServeIsBroadcasting(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	if err := os.WriteFile(PIDFile(f.root), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	_, err := f.run(trainsChannel("https://www.youtube.com/watch?v=zulu001"))
	if err == nil {
		t.Fatal("Ingest ran while serve was broadcasting")
	}
	if !strings.Contains(err.Error(), "broadcasting") {
		t.Errorf("error %q does not say why", err)
	}
	if len(f.runner.calls) != 0 {
		t.Errorf("the runner was called anyway: %v", f.runner.calls)
	}
}

// TestIngestIgnoresAStalePIDFile covers the pid file serve left behind when it
// was killed rather than stopped. The process it names has gone, so ingest runs.
func TestIngestIgnoresAStalePIDFile(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}

	child := exec.Command("/bin/sh", "-c", "exit 0")
	if err := child.Start(); err != nil {
		t.Fatalf("start a child process: %v", err)
	}
	stale := child.Process.Pid
	if err := child.Wait(); err != nil {
		t.Fatalf("wait for the child process: %v", err)
	}
	if err := os.WriteFile(PIDFile(f.root), []byte(strconv.Itoa(stale)+"\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	report, err := f.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("Ingest refused to run for a stale pid file: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the run to have gone ahead", report)
	}
}

// TestIngestIgnoresAnUnreadablePIDFile covers a pid file that holds something
// other than a number. It is a broken file, not a running broadcast.
func TestIngestIgnoresAnUnreadablePIDFile(t *testing.T) {
	logs := captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}
	if err := os.WriteFile(PIDFile(f.root), []byte("not a pid\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("Ingest refused to run for an unreadable pid file: %v", err)
	}
	if !strings.Contains(logs.String(), "unreadable pid file") {
		t.Errorf("the unreadable pid file was not logged:\n%s", logs.String())
	}
}

func TestIngestedItemsAreVisibleToScan(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	broken := "https://www.youtube.com/watch?v=broken01"
	f.runner.entries[source] = []Entry{
		{ID: "zulu001", URL: source, Title: "Steam Engines"},
		{ID: "broken01", URL: broken, Title: "Broken"},
	}
	f.runner.titles[source] = "Steam Engines"
	f.runner.downloadErr[broken] = errors.New("yt-dlp: ERROR: unavailable")
	f.prober.fallback = 612.437

	channels := trainsChannel(source)
	if _, err := f.run(channels); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	index, err := Scan(f.root, channels)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	items := index.Items("trains")
	if len(items) != 1 || items[0].ID != "zulu001" {
		t.Fatalf("got %+v, want only the ok item", items)
	}
	if items[0].Duration != 612437*time.Millisecond {
		t.Errorf("duration = %v, want 612.437s", items[0].Duration)
	}
	if !filepath.IsAbs(items[0].Path) {
		t.Errorf("path %q is not absolute", items[0].Path)
	}
}

func TestIngestRejectsBadOptions(t *testing.T) {
	captureLogs(t)
	cases := []struct {
		name string
		opts IngestOptions
		want string
	}{
		{"no root", IngestOptions{Runner: &fakeRunner{}, Prober: &fakeProber{}}, "no root"},
		{"no runner", IngestOptions{Root: t.TempDir(), Prober: &fakeProber{}}, "no runner"},
		{"no prober", IngestOptions{Root: t.TempDir(), Runner: &fakeRunner{}}, "no prober"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Ingest(tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Steam Engines", "steam-engines"},
		{"steam-engines", "steam-engines"},
		{"Steam_Engines (1972)", "steam-engines-1972"},
		{"  spaced  out  ", "spaced-out"},
		{"----", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLocalPath(t *testing.T) {
	const root = "/srv/channel3"
	cases := []struct {
		name    string
		source  string
		want    string
		wantErr string
	}{
		{name: "absolute", source: "file:///srv/channel3/local/a.mp4", want: "/srv/channel3/local/a.mp4"},
		{name: "localhost", source: "file://localhost/srv/a.mp4", want: "/srv/a.mp4"},
		{name: "escaped space", source: "file:///srv/Steam%20Engines.mp4", want: "/srv/Steam Engines.mp4"},
		{name: "other host", source: "file://nas/srv/a.mp4", wantErr: "names the host"},
		{name: "bare name reads as a host", source: "file://a.mp4", wantErr: "names the host"},
		{name: "no path at all", source: "file://localhost", wantErr: "absolute path"},
		{name: "root-relative", source: "local/a.mp4", want: "/srv/channel3/local/a.mp4"},
		{name: "root-relative subdirectory", source: "local/kids/a.mp4", want: "/srv/channel3/local/kids/a.mp4"},
		{name: "climbs out of the root", source: "../a.mp4", wantErr: "inside the root"},
		{name: "cleans its way out of the root", source: "local/../../a.mp4", wantErr: "inside the root"},
		{name: "absolute with no scheme", source: "/etc/passwd", wantErr: "inside the root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := localPath(root, tc.source)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one mentioning %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("localPath(%q): %v", tc.source, err)
			}
			if got != tc.want {
				t.Errorf("localPath(%q) = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
}

// TestIsLocalSource pins which sources ingest downloads and which it records
// where they already lie.
func TestIsLocalSource(t *testing.T) {
	for _, source := range []string{"local/a.mp4", "file:///srv/a.mp4", "a.mp4"} {
		if !isLocalSource(source) {
			t.Errorf("isLocalSource(%q) = false, want true", source)
		}
	}
	for _, source := range []string{"https://example.com/a", "http://example.com/a"} {
		if isLocalSource(source) {
			t.Errorf("isLocalSource(%q) = true, want false", source)
		}
	}
}

func TestDefaultLocalTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"steam-engines_v2.mp4", "steam engines v2"},
		{"Steam Engines.mp4", "Steam Engines"},
		{"/srv/channel3/local/steam-engines.mp4", "steam engines"},
		{"a__b--c.mp4", "a b c"},
		{"  spaced  out  .mp4", "spaced out"},
		{"Colour_Bars.mkv", "Colour Bars"},
	}
	for _, tc := range cases {
		if got := defaultLocalTitle(tc.in); got != tc.want {
			t.Errorf("defaultLocalTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSourceIDIsStableAndSafe(t *testing.T) {
	const source = "https://www.youtube.com/watch?v=nope&list=PL1"
	first := sourceID(source)
	if first != sourceID(source) {
		t.Error("sourceID is not stable for one url")
	}
	if first == sourceID(source+"x") {
		t.Error("sourceID collides between two urls")
	}
	if !idPattern.MatchString(first) {
		t.Errorf("sourceID %q is not a safe file name", first)
	}
}

// equalStrings compares two string slices.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// names lists directory entry names for an error message.
func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

// TestIngestReprobesALocalFileThatChanged is somebody dropping a longer cut of
// a video in under the same name. The sidecar's duration would otherwise stay
// on the old length and every schedule boundary after it would be wrong.
func TestIngestReprobesALocalFileThatChanged(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	local := filepath.Join(t.TempDir(), "Steam Engines.mp4")
	copyTinyMP4(t, local)
	f.prober.seconds[local] = 90.25

	source := "file://" + local
	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	first := f.sidecar(t, "trains", "steam-engines")
	if first.Size != tinyMP4Size(t) {
		t.Fatalf("size = %d, want the file's %d", first.Size, tinyMP4Size(t))
	}

	// The file is replaced with a longer one.
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("read the local file: %v", err)
	}
	if err := os.WriteFile(local, append(data, data...), 0o644); err != nil {
		t.Fatalf("grow the local file: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.prober.seconds[local] = 181.5

	report, err := second.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the changed file ingested again", report)
	}
	if len(second.prober.calls) != 1 {
		t.Errorf("the changed file was probed %d times, want once", len(second.prober.calls))
	}

	got := second.sidecar(t, "trains", "steam-engines")
	if got.Duration != 181.5 {
		t.Errorf("duration = %v, want the new length 181.5", got.Duration)
	}
	if want := int64(len(data) * 2); got.Size != want {
		t.Errorf("size = %d, want the new size %d", got.Size, want)
	}
}

// TestIngestDownloadsAgainWhenTheFileChanged is the same check for a remote
// item: a truncated or replaced download is fetched again rather than kept.
func TestIngestDownloadsAgainWhenTheFileChanged(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}
	f.runner.titles[source] = "Steam Engines"

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	// The download was cut short, which leaves a playable file of the wrong
	// length rather than no file at all.
	media := filepath.Join(ChannelDir(f.root, "trains"), "zulu001.mp4")
	if err := os.Truncate(media, 1024); err != nil {
		t.Fatalf("truncate the download: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.runner.entries[source] = f.runner.entries[source]
	second.runner.titles[source] = "Steam Engines"

	report, err := second.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the changed item downloaded again", report)
	}
	if got := second.runner.downloads(); len(got) != 1 {
		t.Errorf("downloads = %v, want the changed item fetched again", got)
	}
	if got := second.sidecar(t, "trains", "zulu001"); got.Size != tinyMP4Size(t) {
		t.Errorf("size = %d, want the redownloaded file's %d", got.Size, tinyMP4Size(t))
	}
}

// TestIngestSkipsASidecarWithNoSize covers every library written before the
// size field existed. There is nothing to compare, so the item is skipped
// exactly as it always was rather than downloaded again on the next run.
func TestIngestSkipsASidecarWithNoSize(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "Steam Engines"}}

	dir := ChannelDir(f.root, "trains")
	copyTinyMP4(t, filepath.Join(dir, "zulu001.mp4"))
	ingested := fixedTime()()
	err := WriteSidecar(filepath.Join(dir, "zulu001.json"), Sidecar{
		ID:         "zulu001",
		Title:      "Steam Engines",
		Source:     source,
		File:       "zulu001.mp4",
		Duration:   612.437,
		IngestedAt: &ingested,
		Status:     StatusOK,
	})
	if err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	report, err := f.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{Skipped: 1}) {
		t.Errorf("report = %+v, want one skip", report)
	}
	if got := f.runner.downloads(); len(got) != 0 {
		t.Errorf("a sidecar with no size was downloaded again: %v", got)
	}
	if got := f.sidecar(t, "trains", "zulu001"); got.Size != 0 {
		t.Errorf("the skipped sidecar was rewritten with size %d", got.Size)
	}
}

// TestIngestLocalFileUnderTheRootIsRecordedRelatively is the whole point of a
// root-relative source: the sidecar names the video by a path that resolves
// from the sidecar's own directory, so the library plays on the machine it is
// copied to as well as the one it was ingested on.
func TestIngestLocalFileUnderTheRootIsRecordedRelatively(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	video := filepath.Join(f.root, "local", "steam-engines.mp4")
	copyTinyMP4(t, video)
	f.prober.seconds[video] = 90.25

	report, err := f.run(trainsChannel("local/steam-engines.mp4"))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want one ok", report)
	}
	if len(f.runner.calls) != 0 {
		t.Errorf("a local source reached the runner: %v", f.runner.calls)
	}

	got := f.sidecar(t, "trains", "steam-engines")
	want := filepath.Join("..", "..", "local", "steam-engines.mp4")
	if got.File != want {
		t.Errorf("file = %q, want %q", got.File, want)
	}
	if got.Source != "local/steam-engines.mp4" {
		t.Errorf("source = %q, want the configured path", got.Source)
	}
	if got.Title != "steam engines" {
		t.Errorf("title = %q, want the file name read as words", got.Title)
	}
	if got.Duration != 90.25 {
		t.Errorf("duration = %v, want 90.25", got.Duration)
	}
}

// TestIngestFileURLUnderTheRootIsRecordedRelatively covers a file:// source
// that happens to point inside the root. It is the same video as a relative
// source names, so it is recorded the same way.
func TestIngestFileURLUnderTheRootIsRecordedRelatively(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	video := filepath.Join(f.root, "local", "steam-engines.mp4")
	copyTinyMP4(t, video)

	if _, err := f.run(trainsChannel("file://" + video)); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	got := f.sidecar(t, "trains", "steam-engines")
	want := filepath.Join("..", "..", "local", "steam-engines.mp4")
	if got.File != want {
		t.Errorf("file = %q, want %q", got.File, want)
	}
}

// TestIngestLocalFileOutsideTheRootStaysAbsolute covers video that was never
// put under the root. There is no relative path that survives a copy, so the
// absolute one is kept and the item plays only on this machine.
func TestIngestLocalFileOutsideTheRootStaysAbsolute(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	outside := filepath.Join(t.TempDir(), "steam-engines.mp4")
	copyTinyMP4(t, outside)

	if _, err := f.run(trainsChannel("file://" + outside)); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	got := f.sidecar(t, "trains", "steam-engines")
	if got.File != outside {
		t.Errorf("file = %q, want the absolute path %q", got.File, outside)
	}
	if !filepath.IsAbs(got.File) {
		t.Errorf("file %q is not absolute", got.File)
	}
}

// TestIngestSkipsBothKindsOfLocalFile checks that the skip logic follows a
// relative sidecar path as well as an absolute one, so a second run of an
// unchanged library probes nothing.
func TestIngestSkipsBothKindsOfLocalFile(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	inside := filepath.Join(f.root, "local", "inside.mp4")
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	copyTinyMP4(t, inside)
	copyTinyMP4(t, outside)

	channels := trainsChannel("local/inside.mp4", "file://"+outside)
	if _, err := f.run(channels); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	report, err := second.run(channels)
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{Skipped: 2}) {
		t.Errorf("report = %+v, want both files skipped", report)
	}
	if len(second.prober.calls) != 0 {
		t.Errorf("an unchanged file was probed again: %v", second.prober.calls)
	}
}

// TestIngestReprobesARelativeLocalFileThatChanged is the size check reaching
// through a relative sidecar path. A longer cut dropped in under the same name
// has to be probed again or every schedule boundary after it is wrong.
func TestIngestReprobesARelativeLocalFileThatChanged(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	video := filepath.Join(f.root, "local", "steam-engines.mp4")
	copyTinyMP4(t, video)
	f.prober.seconds[video] = 90.25

	channels := trainsChannel("local/steam-engines.mp4")
	if _, err := f.run(channels); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	data, err := os.ReadFile(video)
	if err != nil {
		t.Fatalf("read the local file: %v", err)
	}
	if err := os.WriteFile(video, append(data, data...), 0o644); err != nil {
		t.Fatalf("grow the local file: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.prober.seconds[video] = 181.5

	report, err := second.run(channels)
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the changed file ingested again", report)
	}
	got := second.sidecar(t, "trains", "steam-engines")
	if got.Duration != 181.5 {
		t.Errorf("duration = %v, want the new length 181.5", got.Duration)
	}
	if want := int64(len(data) * 2); got.Size != want {
		t.Errorf("size = %d, want the new size %d", got.Size, want)
	}
}

// TestIngestConfiguredTitleWins covers the mapping form of a source for both a
// local file, where it replaces the file name, and a remote one, where it
// replaces what yt-dlp reports.
func TestIngestConfiguredTitleWins(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	video := filepath.Join(f.root, "local", "colour-bars.mp4")
	copyTinyMP4(t, video)
	const remote = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[remote] = []Entry{{ID: "zulu001", URL: remote, Title: "from the playlist"}}
	f.runner.titles[remote] = "Steam Engines of the Rockies"

	channels := trainsChannelWith(
		Source{URL: "local/colour-bars.mp4", Title: "Colour Bars Two"},
		Source{URL: remote, Title: "Trains, The Whole Hour"},
	)
	if _, err := f.run(channels); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got := f.sidecar(t, "trains", "colour-bars").Title; got != "Colour Bars Two" {
		t.Errorf("local title = %q, want the configured one", got)
	}
	if got := f.sidecar(t, "trains", "zulu001").Title; got != "Trains, The Whole Hour" {
		t.Errorf("remote title = %q, want the configured one to beat yt-dlp", got)
	}
}

// TestIngestRewritesAChangedTitleWithoutReprobing is editing a title in the
// config: the video has not changed, so it is not downloaded or probed again,
// but the guide has to show the new title on the next run.
func TestIngestRewritesAChangedTitleWithoutReprobing(t *testing.T) {
	logs := captureLogs(t)
	f := newIngestFixture(t)
	video := filepath.Join(f.root, "local", "colour-bars.mp4")
	copyTinyMP4(t, video)

	if _, err := f.run(trainsChannel("local/colour-bars.mp4")); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	first := f.sidecar(t, "trains", "colour-bars")
	if first.Title != "colour bars" {
		t.Fatalf("title = %q, want the default from the file name", first.Title)
	}

	second := newIngestFixture(t)
	second.root = f.root
	report, err := second.run(trainsChannelWith(Source{URL: "local/colour-bars.mp4", Title: "Colour Bars Two"}))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the retitle reported as ok", report)
	}
	if len(second.prober.calls) != 0 {
		t.Errorf("a retitle probed the file again: %v", second.prober.calls)
	}

	got := second.sidecar(t, "trains", "colour-bars")
	if got.Title != "Colour Bars Two" {
		t.Errorf("title = %q, want the new configured one", got.Title)
	}
	first.Title = got.Title
	if got.IngestedAt == nil || !got.IngestedAt.Equal(*first.IngestedAt) {
		t.Errorf("ingested_at = %v, want the first run's %v", got.IngestedAt, first.IngestedAt)
	}
	got.IngestedAt, first.IngestedAt = nil, nil
	if got != first {
		t.Errorf("a retitle changed more than the title:\ngot  %+v\nwant %+v", got, first)
	}
	if !strings.Contains(second.out.String(), "ok     trains/colour-bars  title") {
		t.Errorf("the retitle was not reported with its reason:\n%s", second.out.String())
	}
	if !strings.Contains(logs.String(), "configured title has changed") {
		t.Errorf("the retitle was not logged:\n%s", logs.String())
	}
}

// TestIngestRewritesARemoteTitleWithoutDownloading is the same for a remote
// item, where the configured title is the only one that can change without a
// download.
func TestIngestRewritesARemoteTitleWithoutDownloading(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "from the playlist"}}
	f.runner.titles[source] = "Steam Engines of the Rockies"

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.runner.entries[source] = f.runner.entries[source]
	report, err := second.run(trainsChannelWith(Source{URL: source, Title: "Trains, The Whole Hour"}))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the retitle reported as ok", report)
	}
	if got := second.runner.downloads(); len(got) != 0 {
		t.Errorf("a retitle downloaded the item again: %v", got)
	}
	if got := second.sidecar(t, "trains", "zulu001").Title; got != "Trains, The Whole Hour" {
		t.Errorf("title = %q, want the new configured one", got)
	}
}

// TestIngestKeepsARemoteTitleWhenNoneIsConfigured guards the retitle from
// firing on every run: yt-dlp's title is only known after a download, and the
// playlist listing carries a different, shorter one.
func TestIngestKeepsARemoteTitleWhenNoneIsConfigured(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	const source = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[source] = []Entry{{ID: "zulu001", URL: source, Title: "from the playlist"}}
	f.runner.titles[source] = "Steam Engines of the Rockies"

	if _, err := f.run(trainsChannel(source)); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	second.runner.entries[source] = f.runner.entries[source]
	report, err := second.run(trainsChannel(source))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report != (Report{Skipped: 1}) {
		t.Errorf("report = %+v, want one skip", report)
	}
	if got := second.sidecar(t, "trains", "zulu001").Title; got != "Steam Engines of the Rockies" {
		t.Errorf("title = %q, want the one yt-dlp reported", got)
	}
}

// TestIngestDryRunWritesNoTitle keeps --dry-run honest: it never writes, so a
// title change is reported as the skip it is until a real run happens.
func TestIngestDryRunWritesNoTitle(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	video := filepath.Join(f.root, "local", "colour-bars.mp4")
	copyTinyMP4(t, video)

	if _, err := f.run(trainsChannel("local/colour-bars.mp4")); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	second := newIngestFixture(t)
	second.root = f.root
	retitled := trainsChannelWith(Source{URL: "local/colour-bars.mp4", Title: "Colour Bars Two"})
	report, err := second.run(retitled, func(o *IngestOptions) {
		o.DryRun = true
		o.Prober = nil
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if report != (Report{Skipped: 1}) {
		t.Errorf("report = %+v, want one skip", report)
	}
	if got := second.sidecar(t, "trains", "colour-bars").Title; got != "colour bars" {
		t.Errorf("a dry run rewrote the sidecar title to %q", got)
	}
}

// TestIngestedLibraryMovesBetweenRoots is the Mac-to-Pi rsync: a library
// ingested under one root, copied to another, plays from the new one. Nothing
// is ingested again and no path in the sidecars names the old root.
func TestIngestedLibraryMovesBetweenRoots(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	copyTinyMP4(t, filepath.Join(f.root, "local", "steam-engines.mp4"))
	const remote = "https://www.youtube.com/watch?v=zulu001"
	f.runner.entries[remote] = []Entry{{ID: "zulu001", URL: remote, Title: "Steam Engines"}}
	f.runner.titles[remote] = "Steam Engines of the Rockies"

	channels := trainsChannelWith(
		Source{URL: "local/steam-engines.mp4", Title: "Steam Engines At Home"},
		Source{URL: remote},
	)
	if _, err := f.run(channels); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// rsync of <root>/library and <root>/local onto the other machine.
	other := t.TempDir()
	for _, dir := range []string{"library", "local"} {
		copyTree(t, filepath.Join(f.root, dir), filepath.Join(other, dir))
	}

	index, err := Scan(other, channels)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	items := index.Items("trains")
	if len(items) != 2 {
		t.Fatalf("got %d items under the new root, want 2: %+v", len(items), items)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.Path, other+string(filepath.Separator)) {
			t.Errorf("item %s plays from %q, which is not under the new root %q", item.ID, item.Path, other)
		}
		if !isRegularFile(item.Path) {
			t.Errorf("item %s names %q, which is not there", item.ID, item.Path)
		}
	}
	if items[0].Title != "Steam Engines At Home" {
		t.Errorf("title = %q, want the configured one to have travelled", items[0].Title)
	}
}

// copyTree copies a directory recursively, which is what the rsync in
// deploy/README.md does between the Mac and the Pi.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", from, to, err)
	}
}

// TestIngestFolderSourceTakesEveryVideoInIt is the Home Movies channel: one
// source naming a folder, and every clip prepared into it becomes an item
// titled with its file name, while hidden files, other files and subfolders are
// left alone.
func TestIngestFolderSourceTakesEveryVideoInIt(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	home := filepath.Join(f.root, "local", "home")
	if err := os.MkdirAll(filepath.Join(home, "originals"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2025-06-14 14.03.mp4", "2025-06-14 14.03 2.mp4", "Beach.mov", ".hidden.mp4", "notes.txt", "half.mp4.part"} {
		copyTinyMP4(t, filepath.Join(home, name))
	}

	report, err := f.run(trainsChannel("local/home"))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 3}) {
		t.Errorf("report = %+v, want the three videos ok", report)
	}
	for id, title := range map[string]string{
		"2025-06-14-14-03":   "2025-06-14 14.03",
		"2025-06-14-14-03-2": "2025-06-14 14.03 2",
		"beach":              "Beach",
	} {
		if got := f.sidecar(t, "trains", id); got.Title != title {
			t.Errorf("%s titled %q, want %q", id, got.Title, title)
		}
	}

	// A clip added later is picked up by the next run and the rest are kept.
	copyTinyMP4(t, filepath.Join(home, "2025-07-01 09.15.mp4"))
	report, err = f.run(trainsChannel("local/home"))
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if report.OK != 1 || report.Skipped != 3 {
		t.Errorf("second run report = %+v, want the new clip ok and three skipped", report)
	}
}

// TestLocalOnlyIngestRunsBesideABroadcast is what serve does after a home video
// upload: a channel with a folder and a URL source, a live pid file, and no
// runner. The folder is ingested, the URL is left alone, and the pid guard does
// not stop it.
func TestLocalOnlyIngestRunsBesideABroadcast(t *testing.T) {
	captureLogs(t)
	f := newIngestFixture(t)
	home := filepath.Join(f.root, "local", "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	copyTinyMP4(t, filepath.Join(home, "2025-06-14 14.03.mp4"))
	if err := os.WriteFile(PIDFile(f.root), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Ingest(IngestOptions{
		Root:      f.root,
		Channels:  trainsChannel("local/home", "https://example.com/never-fetched"),
		Prober:    f.prober,
		LocalOnly: true,
		Out:       io.Discard,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if report != (Report{OK: 1}) {
		t.Errorf("report = %+v, want the one clip ok and the URL untouched", report)
	}
}
