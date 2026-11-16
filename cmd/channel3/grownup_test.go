package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/api"
	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/shelf"
)

const testGrownupPIN = "4217"

func TestGrownupCode(t *testing.T) {
	cases := []struct {
		name, upload, grownup, want, wantErr string
	}{
		{"off", "6635", "", "", ""},
		{"good", "6635", "4217", "4217", ""},
		{"no upload PIN", "", "4217", "", "no upload PIN"},
		{"shorter", "6635", "421", "", "same length"},
		{"the same", "6635", "6635", "", "same as the upload PIN"},
		{"not digits", "6635", "42a7", "", "digits only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := grownupCode(tc.upload, tc.grownup)
			if got != tc.want {
				t.Errorf("code %q, want %q", got, tc.want)
			}
			if tc.wantErr == "" && err != nil {
				t.Errorf("error %v, want none", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Errorf("error %v, want one mentioning %q", err, tc.wantErr)
			}
		})
	}
}

func TestPastedURL(t *testing.T) {
	cases := []struct {
		in, want, wantErr string
	}{
		{"  https://www.youtube.com/watch?v=abc123XYZ_-  ", "https://www.youtube.com/watch?v=abc123XYZ_-", ""},
		{"https://www.youtube.com/watch?v=abc&list=PL1&index=3&t=42s", "https://www.youtube.com/watch?v=abc", ""},
		{"https://m.youtube.com/watch?v=abc", "https://www.youtube.com/watch?v=abc", ""},
		{"https://youtu.be/abc?si=tracking&t=10", "https://youtu.be/abc", ""},
		{"https://vimeo.com/12345", "https://vimeo.com/12345", ""},
		{"https://www.youtube.com/playlist?list=PL1", "", "playlist"},
		{"https://www.youtube.com/watch?list=PL1", "", "names no video"},
		{"youtube.com/watch?v=abc", "", "https://"},
		{"file:///etc/passwd", "", "https://"},
		{"", "", "https://"},
	}
	for _, tc := range cases {
		got, err := pastedURL(tc.in)
		if got != tc.want {
			t.Errorf("pastedURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("pastedURL(%q) error %v, want one mentioning %q", tc.in, err, tc.wantErr)
		}
	}
}

// fakeYTDLP stands in for yt-dlp: every link is one video, written to disk as
// a few bytes when it is downloaded.
type fakeYTDLP struct {
	entries map[string]library.Entry
	fail    error
}

func (f fakeYTDLP) Expand(source string) ([]library.Entry, error) {
	e, ok := f.entries[source]
	if !ok {
		return nil, errors.New("ERROR: [youtube] not a video\nmore detail")
	}
	return []library.Entry{e}, nil
}

func (f fakeYTDLP) Download(source, destDir string) (library.Result, error) {
	if f.fail != nil {
		return library.Result{}, f.fail
	}
	e := f.entries[source]
	path := filepath.Join(destDir, e.ID+".mp4")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		return library.Result{}, err
	}
	return library.Result{ID: e.ID, Title: e.Title, Path: path, Duration: 600}, nil
}

type fakeProbe struct{}

func (fakeProbe) DurationSeconds(string) (float64, error) { return 600, nil }

// grownupFixture is a worker over a temp root with a hidden grownup channel.
func grownupFixture(t *testing.T, runner library.Runner, hidden bool) (*grownupWorker, string) {
	t.Helper()
	root := t.TempDir()
	channels := filepath.Join(root, "channels")
	if err := os.MkdirAll(channels, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "id: grownup\nnumber: 90\nname: Grown-ups\nsources: []\n"
	if hidden {
		yaml += "hidden: true\n"
	}
	if err := os.WriteFile(filepath.Join(channels, "grownup.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newGrownupWorker(root, shelf.NewStoreAt(shelf.GrownupPath(root)), shelf.Covers{Dir: shelf.CoversDir(root)},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.runner, w.prober, w.measurer = runner, fakeProbe{}, nil
	return w, root
}

const lecture = "https://www.youtube.com/watch?v=lect001"

func lectureRunner() fakeYTDLP {
	return fakeYTDLP{entries: map[string]library.Entry{
		lecture: {ID: "lect001", URL: lecture, Title: "How Bridges Work"},
	}}
}

// runOne runs the worker until the download keyed key has finished.
func runOne(t *testing.T, w *grownupWorker, key int) api.Download {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range w.Downloads() {
			if d.Key == key && (d.State == api.DownloadDone || d.State == api.DownloadFailed) {
				return d
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("download %d never finished: %+v", key, w.Downloads())
	return api.Download{}
}

func TestAPastedLinkLandsOnTheGrownupShelf(t *testing.T) {
	w, root := grownupFixture(t, lectureRunner(), true)
	rescans := 0
	w.rescan = func() { rescans++ }
	putOnGrownupShelf(t, root, shelf.Entry{Item: &shelf.Item{Channel: "docs", ID: "d1"}})

	d, err := w.Add(lecture + "&list=PL9")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := w.Add(lecture); again.Key != d.Key {
		t.Errorf("pasting the same link while it waits queued it again")
	}
	got := runOne(t, w, d.Key)
	if got.State != api.DownloadDone || got.Title != "How Bridges Work" || got.ID != "lect001" {
		t.Fatalf("download %+v, want it done with its title and id", got)
	}

	s, err := shelf.NewStoreAt(shelf.GrownupPath(root)).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 2 || s.Entries[0].Item == nil || s.Entries[0].Item.Ref() != (shelf.Ref{Channel: "grownup", ID: "lect001"}) {
		t.Errorf("shelf %+v, want the new video first", s.Entries)
	}
	if _, err := os.Stat(filepath.Join(library.ChannelDir(root, "grownup"), "lect001.mp4")); err != nil {
		t.Errorf("the video is not in the grown-up channel: %v", err)
	}
	if rescans != 1 {
		t.Errorf("%d rescans, want one so the menu can play it", rescans)
	}
	if _, err := os.Stat(shelf.Path(root)); err == nil {
		t.Error("Lane's shelf was written")
	}
}

func TestAGrownupDownloadThatFailsSaysWhy(t *testing.T) {
	w, _ := grownupFixture(t, lectureRunner(), true)
	d, _ := w.Add("https://youtu.be/nosuch")
	got := runOne(t, w, d.Key)
	if got.State != api.DownloadFailed || !strings.Contains(got.Error, "not a video") || strings.Contains(got.Error, "more detail") {
		t.Errorf("download %+v, want failed with the first line of the reason", got)
	}

	runner := lectureRunner()
	runner.fail = errors.New("HTTP Error 403")
	w, _ = grownupFixture(t, runner, true)
	d, _ = w.Add(lecture)
	if got := runOne(t, w, d.Key); got.State != api.DownloadFailed || !strings.Contains(got.Error, "403") {
		t.Errorf("download %+v, want failed with yt-dlp's reason", got)
	}
}

func TestTheGrownupChannelMustBeHidden(t *testing.T) {
	w, _ := grownupFixture(t, lectureRunner(), false)
	d, _ := w.Add(lecture)
	if got := runOne(t, w, d.Key); got.State != api.DownloadFailed || !strings.Contains(got.Error, "hidden: true") {
		t.Errorf("download %+v, want it refused for a channel on the dial", got)
	}
}

func TestDeletingAGrownupVideo(t *testing.T) {
	w, root := grownupFixture(t, lectureRunner(), true)
	d, _ := w.Add(lecture)
	runOne(t, w, d.Key)
	// It has been moved into a folder, where Delete must find it.
	store := shelf.NewStoreAt(shelf.GrownupPath(root))
	s, _ := store.Read()
	s.Entries = []shelf.Entry{{Folder: &shelf.Folder{Name: "Bridges", Items: []shelf.Item{
		{Channel: "grownup", ID: "lect001"}, {Channel: "docs", ID: "d1"},
	}}}}
	if _, err := store.Write(s); err != nil {
		t.Fatal(err)
	}

	if err := w.Delete("lect001"); err != nil {
		t.Fatal(err)
	}
	s, _ = store.Read()
	if s.Contains(shelf.Ref{Channel: "grownup", ID: "lect001"}) {
		t.Errorf("still on the shelf: %+v", s.Entries)
	}
	if len(s.Entries) != 1 || s.Entries[0].Folder == nil || len(s.Entries[0].Folder.Items) != 1 {
		t.Errorf("shelf %+v, want the folder kept with its other video", s.Entries)
	}
	left, _ := filepath.Glob(filepath.Join(library.ChannelDir(root, "grownup"), "lect001.*"))
	if len(left) != 0 {
		t.Errorf("files left behind: %v", left)
	}

	if err := w.Delete("lect001"); !errors.Is(err, api.ErrNoSuchVideo) {
		t.Errorf("deleting it again: %v, want ErrNoSuchVideo", err)
	}
	if err := w.Delete("../../channels/grownup"); !errors.Is(err, api.ErrNoSuchVideo) {
		t.Errorf("a path as an id: %v, want ErrNoSuchVideo", err)
	}
}

func putOnGrownupShelf(t *testing.T, root string, entries ...shelf.Entry) {
	t.Helper()
	if _, err := shelf.NewStoreAt(shelf.GrownupPath(root)).Write(shelf.Shelf{Entries: entries}); err != nil {
		t.Fatal(err)
	}
}

// grownupStation is shelfStation with the grown-up code on, and Doc One alone
// on the grown-up shelf.
func grownupStation(t *testing.T) *station {
	t.Helper()
	s, _, _, root := shelfStation(t)
	s.shelf.grownupPIN = testGrownupPIN
	s.shelf.grownup = shelf.NewStoreAt(shelf.GrownupPath(root))
	putOnGrownupShelf(t, root, shelf.Entry{Item: &shelf.Item{Channel: "docs", ID: "d1"}})
	return s
}

func TestTheGrownupCodeOpensTheGrownupShelf(t *testing.T) {
	s := grownupStation(t)
	press(s, "0"+testGrownupPIN)
	page := s.menuPage()
	if page.Title != "ON DEMAND" || len(page.Tiles) != 1 || page.Tiles[0].Title != "Doc One" {
		t.Errorf("grown-up menu %+v, want Doc One alone and no Movies folder", page)
	}
	press(s, "0")
	if s.cinema != nil {
		t.Fatal("0 should leave")
	}

	press(s, "0"+testPIN)
	if page := s.menuPage(); len(page.Tiles) != 3 || page.Tiles[0].Title != "Movies" {
		t.Errorf("Lane's code opened %+v, want Lane's shelf", page)
	}
}

func TestAnEmptyGrownupShelfIsAnEmptyMenu(t *testing.T) {
	s := grownupStation(t)
	if _, err := s.shelf.grownup.Write(shelf.Shelf{Revision: 1, Entries: []shelf.Entry{}}); err != nil {
		t.Fatal(err)
	}
	press(s, "0"+testGrownupPIN)
	if page := s.menuPage(); page.Title != "ON DEMAND" || len(page.Tiles) != 0 {
		t.Errorf("empty grown-up menu %+v, want no tiles and no movies", page)
	}
}

func TestWithoutTheGrownupCodeItIsWrong(t *testing.T) {
	s, _, _, _ := shelfStation(t)
	press(s, "0"+testGrownupPIN)
	if s.cinema != nil {
		t.Error("a code that is not configured opened Movie Mode")
	}
}
