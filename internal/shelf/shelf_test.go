package shelf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/schedule"
)

func item(channel, id string) *Item { return &Item{Channel: channel, ID: id} }

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		shelf   Shelf
		wantErr string
	}{
		{"empty", Shelf{}, ""},
		{"an item and a folder", Shelf{Entries: []Entry{
			{Item: item("sesame", "BQbjApUBrEc")},
			{Folder: &Folder{Name: "Tractor Ted", Items: []Item{{Channel: "farmtv", ID: "abc"}}}},
		}}, ""},
		{"neither", Shelf{Entries: []Entry{{}}}, "an item or a folder"},
		{"both", Shelf{Entries: []Entry{{Item: item("a", "b"), Folder: &Folder{Name: "x"}}}}, "an item or a folder"},
		{"twice", Shelf{Entries: []Entry{
			{Item: item("farmtv", "abc")},
			{Folder: &Folder{Name: "Tractor Ted", Items: []Item{{Channel: "farmtv", ID: "abc"}}}},
		}}, "twice"},
		{"blank folder name", Shelf{Entries: []Entry{{Folder: &Folder{Name: "  "}}}}, "needs a name"},
		{"bad channel", Shelf{Entries: []Entry{{Item: item("Farm TV", "abc")}}}, "does not name"},
		{"bad id", Shelf{Entries: []Entry{{Item: item("farmtv", "../etc")}}}, "does not name"},
		{"bad folder id", Shelf{Entries: []Entry{{Folder: &Folder{ID: "../x", Name: "x"}}}}, "not a folder id"},
		{"folder id twice", Shelf{Entries: []Entry{
			{Folder: &Folder{ID: "ab", Name: "x"}}, {Folder: &Folder{ID: "ab", Name: "y"}},
		}}, "used twice"},
		{"long title", Shelf{Entries: []Entry{{Item: &Item{Channel: "a", ID: "b", Title: strings.Repeat("x", 81)}}}}, "at most"},
	}
	for _, tt := range tests {
		err := Validate(tt.shelf)
		if tt.wantErr == "" && err != nil {
			t.Errorf("%s: %v", tt.name, err)
		}
		if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%s: got %v, want an error about %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestStoreWriteAndRevision(t *testing.T) {
	store := NewStore(t.TempDir())
	empty, err := store.Read()
	if err != nil || empty.Revision != 0 || len(empty.Entries) != 0 || empty.Entries == nil {
		t.Fatalf("a shelf never saved should be empty at revision 0 with a list: %+v %v", empty, err)
	}

	saved, err := store.Write(Shelf{Revision: 0, Entries: []Entry{
		{Folder: &Folder{Name: " Tractor Ted ", Items: []Item{{Channel: "farmtv", ID: "abc", Title: "  "}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f := saved.Entries[0].Folder
	if saved.Revision != 1 || f.Name != "Tractor Ted" || !validFolderID(f.ID) || f.Items[0].Title != "" {
		t.Errorf("saved %+v %+v", saved, f)
	}

	if _, err := store.Write(Shelf{Revision: 0}); !errors.Is(err, ErrStale) {
		t.Errorf("a save based on an old revision: %v, want ErrStale", err)
	}

	back, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if back.Revision != 1 || back.Entries[0].Folder.ID != f.ID {
		t.Errorf("read back %+v", back)
	}

	// The folder keeps its id through a rename.
	back.Entries[0].Folder.Name = "Ted"
	again, err := store.Write(back)
	if err != nil {
		t.Fatal(err)
	}
	if again.Revision != 2 || again.Entries[0].Folder.ID != f.ID {
		t.Errorf("renamed %+v", again.Entries[0].Folder)
	}
}

func TestStoreRefusesInvalid(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Write(Shelf{Entries: []Entry{{}}}); err == nil {
		t.Error("an invalid shelf was saved")
	}
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Errorf("an invalid save left a file: %v", err)
	}
}

func TestPlayable(t *testing.T) {
	lib := NewLibrary([]schedule.Channel{
		{ID: "farmtv", Items: []schedule.Item{{ID: "a", Title: "Tractor Ted Meets Diggers"}, {ID: "b", Title: "Tractor Ted Harvest"}}},
		{ID: "sesame", Items: []schedule.Item{{ID: "c", Title: "Telly and the Fire Bell (1984)"}}},
		{ID: "guide", Guide: true, Items: []schedule.Item{{ID: "c"}}},
	})
	sh := Shelf{Entries: []Entry{
		{Item: &Item{Channel: "sesame", ID: "c", Title: "Telly"}},
		{Item: item("sesame", "gone")},
		{Folder: &Folder{ID: "f1", Name: "Tractor Ted", Items: []Item{{Channel: "farmtv", ID: "b"}, {Channel: "farmtv", ID: "gone"}}}},
		{Folder: &Folder{ID: "f2", Name: "Empty now", Items: []Item{{Channel: "farmtv", ID: "gone"}}}},
	}}
	shown := Playable(sh, lib)
	if len(shown) != 2 {
		t.Fatalf("shown %d entries, want the video and the folder that still has one", len(shown))
	}
	if v := shown[0].Video; v == nil || v.Title != "Telly" {
		t.Errorf("first %+v, want Telly under its own title", shown[0])
	}
	if f := shown[1].Folder; f == nil || len(f.Videos) != 1 || f.Videos[0].Title != "Tractor Ted Harvest" {
		t.Errorf("folder %+v, want the one episode left, under the library's title", shown[1].Folder)
	}
	if !sh.Contains(Ref{"farmtv", "b"}) || sh.Contains(Ref{"farmtv", "a"}) {
		t.Error("Contains is wrong")
	}
	if _, ok := lib[Ref{"guide", "c"}]; ok {
		t.Error("the guide's borrowed songs were indexed as its own")
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		key    string
		ref    Ref
		folder string
		ok     bool
	}{
		{"farmtv/abc_1-2", Ref{"farmtv", "abc_1-2"}, "", true},
		{"folder:0a1b", Ref{}, "0a1b", true},
		{"folder:../x", Ref{}, "", false},
		{"farmtv", Ref{}, "", false},
		{"farmtv/../x", Ref{}, "", false},
		{"../x/y", Ref{}, "", false},
	}
	for _, tt := range tests {
		ref, folder, err := ParseKey(tt.key)
		if (err == nil) != tt.ok || ref != tt.ref || folder != tt.folder {
			t.Errorf("%q: %v %q %v", tt.key, ref, folder, err)
		}
	}
	if ItemKey(Ref{"farmtv", "abc"}) != "farmtv/abc" || FolderKey("ab") != "folder:ab" {
		t.Error("keys do not round trip")
	}
}

func TestCoversFromPicture(t *testing.T) {
	covers := Covers{Dir: filepath.Join(t.TempDir(), "covers")}
	src := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	src.Set(10, 10, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	if err := covers.FromPicture("folder:ab", &buf); err != nil {
		t.Fatal(err)
	}
	img, err := covers.Load("folder:ab")
	if err != nil || img == nil {
		t.Fatalf("load: %v %v", img, err)
	}
	if b := img.Bounds(); b.Dx() != coverWidth || b.Dy() != 540 {
		t.Errorf("cover is %v, want it scaled to %d wide", b, coverWidth)
	}
	if err := covers.FromPicture("folder:ab", strings.NewReader("not a picture")); err == nil {
		t.Error("text was taken as a picture")
	}
	if img, err := covers.Load("farmtv/none"); img != nil || err != nil {
		t.Errorf("a missing cover should be nil without an error: %v %v", img, err)
	}
}

func TestCoversFromFrame(t *testing.T) {
	var gotAt time.Duration
	covers := Covers{
		Dir: filepath.Join(t.TempDir(), "covers"),
		Grab: func(_ context.Context, video, out string, at time.Duration) error {
			gotAt = at
			return os.WriteFile(out, []byte("jpeg"), 0o644)
		},
	}
	if err := covers.FromFrame(context.Background(), "farmtv/abc", "/videos/abc.mp4", 90*time.Second); err != nil {
		t.Fatal(err)
	}
	if gotAt != 90*time.Second || !covers.Has("farmtv/abc") {
		t.Errorf("grabbed at %v, has %v", gotAt, covers.Has("farmtv/abc"))
	}

	covers.Grab = func(context.Context, string, string, time.Duration) error { return errors.New("no") }
	if err := covers.FromFrame(context.Background(), "farmtv/def", "/videos/def.mp4", 0); err == nil {
		t.Error("a failed grab was not reported")
	}
	if covers.Has("farmtv/def") {
		t.Error("a failed grab left a cover")
	}
}
