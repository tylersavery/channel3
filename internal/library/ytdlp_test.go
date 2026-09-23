package library

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// readYTDLPFixture reads one of the recorded yt-dlp outputs under testdata.
// They are real output, trimmed of the fields Channel Three ignores.
func readYTDLPFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ytdlp", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestParseEntriesSingleVideo covers a plain video URL, which yt-dlp answers
// with one video document rather than a playlist.
func TestParseEntriesSingleVideo(t *testing.T) {
	captureLogs(t)

	got, err := parseEntries(readYTDLPFixture(t, "single-video.json"))
	if err != nil {
		t.Fatalf("parseEntries: %v", err)
	}
	want := []Entry{{
		ID:    "8-X8acD_r38",
		URL:   "https://www.youtube.com/watch?v=8-X8acD_r38",
		Title: "Mars in a Minute: How Do You Land on Mars?",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// TestParseEntriesFlatPlaylist covers a playlist, whose entries carry url rather
// than webpage_url because the dump is flat.
func TestParseEntriesFlatPlaylist(t *testing.T) {
	captureLogs(t)

	got, err := parseEntries(readYTDLPFixture(t, "flat-playlist.json"))
	if err != nil {
		t.Fatalf("parseEntries: %v", err)
	}
	want := []Entry{
		{
			ID:    "9bLhVJKoIOg",
			URL:   "https://www.youtube.com/watch?v=9bLhVJKoIOg",
			Title: "Meet the Mars Samples: Sapphire Canyon (Sample 25)",
		},
		{
			ID:    "0EMy3vs-UbM",
			URL:   "https://www.youtube.com/watch?v=0EMy3vs-UbM",
			Title: "Meet the Mars Samples: Comet Geyser (Sample 24)",
		},
		{
			ID:    "SCdSSQkgn_Q",
			URL:   "https://www.youtube.com/watch?v=SCdSSQkgn_Q",
			Title: "Meet the Mars Samples: Lefroy Bay (Sample 23) | #Shorts #Mars #NASA",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseEntriesFlattensNesting(t *testing.T) {
	captureLogs(t)

	nested := `{"_type":"playlist","id":"CH","entries":[
		{"_type":"playlist","id":"PL1","entries":[
			{"_type":"url","id":"aaa","url":"https://example.com/aaa","title":"A"}]},
		{"_type":"url","id":"bbb","url":"https://example.com/bbb","title":"B"}]}`

	got, err := parseEntries([]byte(nested))
	if err != nil {
		t.Fatalf("parseEntries: %v", err)
	}
	want := []Entry{
		{ID: "aaa", URL: "https://example.com/aaa", Title: "A"},
		{ID: "bbb", URL: "https://example.com/bbb", Title: "B"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseEntriesRejectsEmptyOutput(t *testing.T) {
	captureLogs(t)

	cases := []struct {
		name string
		data string
		want string
	}{
		{"nothing at all", "", "no JSON output"},
		{"null", "null\n", "no videos"},
		{"empty playlist", `{"_type":"playlist","id":"PL1","entries":[]}`, "no videos"},
		{"entry with no url", `{"_type":"playlist","entries":[{"_type":"url","id":"aaa"}]}`, "no videos"},
		{"not json", "<html>", "decode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseEntries([]byte(tc.data)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestParseDownload(t *testing.T) {
	captureLogs(t)

	line := `{"id":"8-X8acD_r38","title":"Mars in a Minute","ext":"mp4","duration":60,` +
		`"filename":"/srv/channel3/library/space/8-X8acD_r38.mp4",` +
		`"_filename":"/srv/channel3/library/space/8-X8acD_r38.f137.mp4"}`

	got, err := parseDownload([]byte(line))
	if err != nil {
		t.Fatalf("parseDownload: %v", err)
	}
	want := Result{
		ID:       "8-X8acD_r38",
		Title:    "Mars in a Minute",
		Path:     "/srv/channel3/library/space/8-X8acD_r38.mp4",
		Duration: 60,
	}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// TestParseDownloadFallsBackToTheOldFilenameField covers yt-dlp releases that
// only report _filename.
func TestParseDownloadFallsBackToTheOldFilenameField(t *testing.T) {
	captureLogs(t)

	got, err := parseDownload([]byte(`{"id":"aaa","title":"A","_filename":"/tmp/aaa.mp4"}`))
	if err != nil {
		t.Fatalf("parseDownload: %v", err)
	}
	if got.Path != "/tmp/aaa.mp4" {
		t.Errorf("path = %q, want the _filename field", got.Path)
	}
}

func TestParseDownloadRejectsOutputWithNoID(t *testing.T) {
	captureLogs(t)

	if _, err := parseDownload([]byte(`{"title":"A"}`)); err == nil || !strings.Contains(err.Error(), "no id") {
		t.Errorf("error = %v, want one mentioning a missing id", err)
	}
}

func TestLocateDownload(t *testing.T) {
	captureLogs(t)
	dir := t.TempDir()

	for _, name := range []string{"aaa.json", "aaa.mp4.part", "aaa.mp4", "bbb.mkv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got, err := locateDownload(dir, "aaa")
	if err != nil {
		t.Fatalf("locateDownload: %v", err)
	}
	if got != filepath.Join(dir, "aaa.mp4") {
		t.Errorf("got %q, want the media file rather than the sidecar or the part file", got)
	}

	if _, err := locateDownload(dir, "ccc"); err == nil {
		t.Error("locateDownload found a file for an id that was never downloaded")
	}
	if _, err := locateDownload(dir, ""); err == nil {
		t.Error("locateDownload accepted an empty id")
	}
}

// TestToolErrorPrefersTheErrorLine pins what a failed sidecar records. yt-dlp
// prints warnings before the line that says why it gave up, so the first line is
// often the wrong one to keep.
func TestToolErrorPrefersTheErrorLine(t *testing.T) {
	captureLogs(t)

	cases := []struct {
		name   string
		stderr string
		want   string
	}{
		{
			name:   "error on its own",
			stderr: string(readYTDLPFixture(t, "download-error.txt")),
			want:   "yt-dlp: ERROR: [youtube] zzzzNOTREAL: This video is unavailable",
		},
		{
			name:   "warning before the error",
			stderr: string(readYTDLPFixture(t, "warning-then-error.txt")),
			want: "yt-dlp: ERROR: [youtube:tab] PLbogus000000000000000000000000000: " +
				"YouTube said: The playlist does not exist.",
		},
		{
			name:   "leading blank lines",
			stderr: "\n\n   \nERROR: [youtube] aaa: Sign in to confirm your age\n",
			want:   "yt-dlp: ERROR: [youtube] aaa: Sign in to confirm your age",
		},
		{
			name:   "no error line at all",
			stderr: "WARNING: unable to extract the thumbnail\nWARNING: falling back\n",
			want:   "yt-dlp: WARNING: unable to extract the thumbnail",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorded := &ToolError{Tool: "yt-dlp", Stderr: tc.stderr, Err: errors.New("exit status 1")}
			if got := recorded.Error(); got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}

	silent := &ToolError{Tool: "yt-dlp", Err: errors.New("exec: \"yt-dlp\": file does not exist")}
	if got := silent.Error(); !strings.Contains(got, "file does not exist") {
		t.Errorf("got %q, want the wrapped error when stderr is empty", got)
	}
	if !errors.Is(silent, silent.Err) {
		t.Error("ToolError does not unwrap to its cause")
	}
}

// TestIgnoreConfigIsAlwaysPassed keeps a user's yt-dlp config from changing what
// Channel Three downloads or dropping a .info.json into the library.
func TestIgnoreConfigIsAlwaysPassed(t *testing.T) {
	cases := map[string][]string{
		"expand":   expandArgs("https://example.com/a"),
		"download": downloadArgs("https://example.com/a", "/srv/channel3/library/trains", DefaultFormat),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if len(args) == 0 || args[0] != ignoreConfigFlag {
				t.Fatalf("%s args do not start with %s: %v", name, ignoreConfigFlag, args)
			}
			// The source must stay behind the -- separator, so a url that looks
			// like a flag is never read as one.
			if args[len(args)-2] != "--" {
				t.Errorf("%s args do not end with a -- separator before the source: %v", name, args)
			}
		})
	}

	if slices.Contains(downloadArgs("u", "d", DefaultFormat), "--write-info-json") {
		t.Error("the download writes an info json, which the index would read as a sidecar")
	}
	if !slices.Contains(downloadArgs("u", "d", DefaultFormat), "--no-write-info-json") {
		t.Error("the download does not suppress the info json")
	}
}

// TestBoundedBufferKeepsOnlyTheFirstBytes covers the cap on captured stderr.
func TestBoundedBufferKeepsOnlyTheFirstBytes(t *testing.T) {
	b := &boundedBuffer{limit: 10}
	for range 5 {
		n, err := b.Write([]byte("abcde"))
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if n != 5 {
			t.Fatalf("Write reported %d bytes, want 5; a short write would fail the command", n)
		}
	}

	got := b.String()
	if !strings.HasPrefix(got, "abcdeabcde") {
		t.Errorf("got %q, want the first ten bytes", got)
	}
	if !strings.Contains(got, "15 more bytes") {
		t.Errorf("got %q, want a note of what was dropped", got)
	}

	small := &boundedBuffer{limit: 10}
	if _, err := small.Write([]byte("abc")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if small.String() != "abc" {
		t.Errorf("got %q, want the whole of a short write with no note", small.String())
	}
}

// TestYTDLPTimesOut proves a stuck yt-dlp becomes a failed item rather than a
// hang, and that the recorded reason names the timeout.
func TestYTDLPTimesOut(t *testing.T) {
	captureLogs(t)
	sleeper := sleepBinary(t)

	started := time.Now()
	_, err := YTDLP{Path: sleeper, ExpandTimeout: stuckTimeout}.Expand("https://example.com/a")
	if err == nil {
		t.Fatal("Expand waited for a binary that never returns")
	}
	if !strings.Contains(err.Error(), "timed out after 250ms") {
		t.Errorf("error %q does not name the timeout", err)
	}
	if took := time.Since(started); took > killedWithin {
		t.Errorf("Expand took %s to give up, want the deadline to take the whole process tree", took)
	}

	started = time.Now()
	_, err = YTDLP{Path: sleeper, DownloadTimeout: stuckTimeout}.Download("https://example.com/a", t.TempDir())
	if err == nil {
		t.Fatal("Download waited for a binary that never returns")
	}
	if !strings.Contains(err.Error(), "timed out after 250ms") {
		t.Errorf("error %q does not name the timeout", err)
	}
	if took := time.Since(started); took > killedWithin {
		t.Errorf("Download took %s to give up, want the deadline to take the whole process tree", took)
	}
}

// TestYTDLPDefaultTimeouts pins the bounds an unconfigured runner uses.
func TestYTDLPDefaultTimeouts(t *testing.T) {
	if got := orDuration(YTDLP{}.ExpandTimeout, defaultExpandTimeout); got != 2*time.Minute {
		t.Errorf("expand timeout = %v, want 2m", got)
	}
	if got := orDuration(YTDLP{}.DownloadTimeout, defaultDownloadTimeout); got != 30*time.Minute {
		t.Errorf("download timeout = %v, want 30m", got)
	}
	if got := orDuration(FFProbe{}.Timeout, defaultProbeTimeout); got != 2*time.Minute {
		t.Errorf("probe timeout = %v, want 2m", got)
	}
	if got := orDuration(90*time.Second, time.Minute); got != 90*time.Second {
		t.Errorf("an explicit timeout was overridden: %v", got)
	}
}

// TestParseEntriesRejectsAnUnsafeID stops a crafted or broken extractor from
// naming a file outside the channel directory.
func TestParseEntriesRejectsAnUnsafeID(t *testing.T) {
	captureLogs(t)

	for _, id := range []string{"../../../escaped", "a/b", "..", "with space", "sem;colon"} {
		body := `{"_type":"playlist","entries":[{"_type":"url","id":"` + id +
			`","url":"https://example.com/a","title":"A"}]}`
		if _, err := parseEntries([]byte(body)); err == nil {
			t.Errorf("parseEntries accepted the item id %q", id)
		}
	}
	for _, id := range []string{"8-X8acD_r38", "dQw4w9WgXcQ", "a", "A_1-2"} {
		body := `{"_type":"playlist","entries":[{"_type":"url","id":"` + id +
			`","url":"https://example.com/a","title":"A"}]}`
		if _, err := parseEntries([]byte(body)); err != nil {
			t.Errorf("parseEntries rejected the ordinary item id %q: %v", id, err)
		}
	}
}

func TestParseDownloadRejectsAnUnsafeID(t *testing.T) {
	captureLogs(t)

	if _, err := parseDownload([]byte(`{"id":"../escaped","title":"A","filename":"/tmp/x.mp4"}`)); err == nil {
		t.Error("parseDownload accepted an item id that escapes its directory")
	}
}

// sleepBinary writes a tiny script that never returns in the time a test waits,
// and that leaves a grandchild holding the output pipes when it is killed.
//
// The grandchild is the whole point. A tool that forks, which yt-dlp does to
// merge a download and ffprobe can do on a damaged file, hands its own stdout
// and stderr to the child. Killing only the process that was started leaves
// those pipes open and Wait blocks on them until the child finishes by itself,
// so the timeout bounds nothing. This script reproduces that exactly: the
// background sleep outlives the shell unless the whole process group is killed.
func sleepBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sleeper")
	script := "#!/bin/sh\nsleep 30 &\nwait\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write the sleeper: %v", err)
	}
	return path
}

// stuckTimeout is the deadline the timeout tests give a tool that never
// returns.
//
// It has to outlast the sleeper forking its grandchild. A deadline of a few
// milliseconds can land before the fork, and the test would then prove only
// that a single process can be killed, which was never the bug.
const stuckTimeout = 250 * time.Millisecond

// killedWithin is how long a call whose deadline has passed may still take.
//
// Killing the process group ends it in milliseconds. This bound is loose enough
// for a busy machine and still well under the two second WaitDelay that would
// be reached if the group kill ever stopped working, so a regression fails here
// rather than passing slowly.
const killedWithin = 2 * time.Second

// TestDefaultFormatCapsAt1080pH264 keeps the one decision the Pi depends on
// visible: anything but H.264 at 1080p or below will not play smoothly.
func TestDefaultFormatCapsAt1080pH264(t *testing.T) {
	for _, want := range []string{"vcodec^=avc1", "height<=1080", "ba[ext=m4a]"} {
		if !strings.Contains(DefaultFormat, want) {
			t.Errorf("DefaultFormat %q does not contain %q", DefaultFormat, want)
		}
	}
	// Every branch of the selector must keep the height cap, or a fallback would
	// quietly pull a 4K file onto the Pi.
	for _, choice := range strings.Split(DefaultFormat, "/") {
		if !strings.Contains(choice, "height<=1080") {
			t.Errorf("format choice %q does not cap the height", choice)
		}
	}
}
