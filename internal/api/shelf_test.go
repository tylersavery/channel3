package api

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/shelf"
)

// grab records the frame a cover was asked for and writes a stand-in JPEG.
type grab struct {
	video string
	at    time.Duration
}

func shelfDeps(t *testing.T, pin string) (Deps, *grab, *int) {
	t.Helper()
	root := t.TempDir()
	g := &grab{}
	saves := 0
	deps := testDeps()
	deps.UploadPIN = pin
	deps.Shelf = shelf.NewStore(root)
	deps.Covers = &shelf.Covers{
		Dir: shelf.CoversDir(root),
		Grab: func(_ context.Context, video, out string, at time.Duration) error {
			g.video, g.at = video, at
			return os.WriteFile(out, []byte("jpeg"), 0o644)
		},
	}
	deps.ShelfSaved = func() { saves++ }
	return deps, g, &saves
}

// send makes a request with the PIN and returns the response.
func send(t *testing.T, deps Deps, method, target, pin, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if pin != "" {
		r.Header.Set("X-Upload-PIN", pin)
	}
	rec := httptest.NewRecorder()
	New(deps).ServeHTTP(rec, r)
	return rec
}

func TestShelfStartsEmpty(t *testing.T) {
	deps, _, _ := shelfDeps(t, "6635")
	var got shelfResponse
	decode(t, get(t, deps, "/api/shelf"), &got)
	if got.Revision != 0 || got.Entries == nil || len(got.Entries) != 0 || !got.Editable {
		t.Errorf("got %+v", got)
	}
}

func TestShelfSaveAndRead(t *testing.T) {
	deps, _, saves := shelfDeps(t, "6635")
	body := `{"revision":0,"entries":[
		{"item":{"channel":"trains","id":"abc","title":"Steam!"}},
		{"folder":{"name":"Docs","items":[{"channel":"docs","id":"d2"},{"channel":"docs","id":"gone"}]}}
	]}`
	rec := send(t, deps, http.MethodPut, "/api/shelf", "6635", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("save answered %d: %s", rec.Code, rec.Body)
	}
	if *saves != 1 {
		t.Errorf("ShelfSaved called %d times, want once", *saves)
	}

	var got shelfResponse
	decode(t, get(t, deps, "/api/shelf"), &got)
	if got.Revision != 1 || len(got.Entries) != 2 {
		t.Fatalf("got %+v", got)
	}
	it := got.Entries[0].Item
	if it.Title != "Steam!" || it.LibraryTitle != "Steam Engines" || it.Duration != 612.437 || it.Missing {
		t.Errorf("item %+v", it)
	}
	f := got.Entries[1].Folder
	if f.ID == "" || f.Name != "Docs" || len(f.Items) != 2 || f.Items[0].Missing || !f.Items[1].Missing {
		t.Errorf("folder %+v", f)
	}

	// A second phone still on revision 0 is refused.
	if rec := send(t, deps, http.MethodPut, "/api/shelf", "6635", `{"revision":0,"entries":[]}`); rec.Code != http.StatusConflict {
		t.Errorf("a stale save answered %d, want 409", rec.Code)
	}
}

func TestShelfSaveNeedsThePIN(t *testing.T) {
	deps, _, saves := shelfDeps(t, "6635")
	for _, pin := range []string{"", "0000"} {
		if rec := send(t, deps, http.MethodPut, "/api/shelf", pin, `{"revision":0,"entries":[]}`); rec.Code != http.StatusForbidden {
			t.Errorf("PIN %q answered %d, want 403", pin, rec.Code)
		}
	}
	off, _, _ := shelfDeps(t, "")
	if rec := send(t, off, http.MethodPut, "/api/shelf", "6635", `{"revision":0,"entries":[]}`); rec.Code != http.StatusNotFound {
		t.Errorf("a box without a PIN answered %d, want 404", rec.Code)
	}
	if *saves != 0 {
		t.Error("a refused save was reported as saved")
	}
}

func TestShelfSaveRefusesNonsense(t *testing.T) {
	deps, _, _ := shelfDeps(t, "6635")
	for _, body := range []string{`nope`, `{"revision":0,"entries":[{}]}`, `{"revision":0,"entries":[{"item":{"channel":"../x","id":"y"}}]}`} {
		if rec := send(t, deps, http.MethodPut, "/api/shelf", "6635", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", body, rec.Code)
		}
	}
	if rec := send(t, deps, http.MethodDelete, "/api/shelf", "6635", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE answered %d, want 405", rec.Code)
	}
}

func TestLibraryMarksWhatIsOnTheShelf(t *testing.T) {
	deps, _, _ := shelfDeps(t, "6635")
	send(t, deps, http.MethodPut, "/api/shelf", "6635", `{"revision":0,"entries":[{"item":{"channel":"trains","id":"def"}}]}`)
	var got libraryResponse
	decode(t, get(t, deps, "/api/library"), &got)
	if len(got.Channels) != 3 || got.Channels[0].ID != "trains" {
		t.Fatalf("channels %+v", got.Channels)
	}
	for _, it := range got.Channels[0].Items {
		if it.OnShelf != (it.ID == "def") {
			t.Errorf("%s on shelf: %v", it.ID, it.OnShelf)
		}
	}
	if got.Channels[2].Items == nil {
		t.Error("an empty channel's items should be a list")
	}
}

func TestCoverFrame(t *testing.T) {
	deps, g, _ := shelfDeps(t, "6635")
	send(t, deps, http.MethodPut, "/api/shelf", "6635", `{"revision":0,"entries":[{"item":{"channel":"trains","id":"abc"}}]}`)

	if rec := send(t, deps, http.MethodPost, "/api/shelf/cover/frame?key=trains/def&at=10", "6635", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a frame for something not on the shelf answered %d, want 404", rec.Code)
	}
	if rec := send(t, deps, http.MethodPost, "/api/shelf/cover/frame?key=trains/abc&at=10", "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a frame without the PIN answered %d, want 403", rec.Code)
	}

	rec := send(t, deps, http.MethodPost, "/api/shelf/cover/frame?key=trains/abc&at=9999", "6635", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("frame answered %d: %s", rec.Code, rec.Body)
	}
	if g.video != "/lib/trains/steam.mp4" || g.at > 612*time.Second {
		t.Errorf("grabbed %s at %v, want the video's own path inside its length", g.video, g.at)
	}
	var got map[string]string
	decode(t, rec, &got)
	if !strings.HasPrefix(got["cover"], "/api/shelf/cover?key=trains%2Fabc&v=") {
		t.Errorf("cover URL %q", got["cover"])
	}

	img := get(t, deps, got["cover"])
	if img.Code != http.StatusOK || img.Header().Get("Content-Type") != "image/jpeg" || img.Body.String() != "jpeg" {
		t.Errorf("cover answered %d %q %q", img.Code, img.Header().Get("Content-Type"), img.Body)
	}
	if rec := get(t, deps, "/api/shelf/cover?key=trains/def"); rec.Code != http.StatusNotFound {
		t.Errorf("a cover not made answered %d, want 404", rec.Code)
	}
	if rec := get(t, deps, "/api/shelf/cover?key=../../etc/passwd"); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad key answered %d, want 400", rec.Code)
	}
}

func TestCoverPictureForAFolder(t *testing.T) {
	deps, _, _ := shelfDeps(t, "6635")
	rec := send(t, deps, http.MethodPut, "/api/shelf", "6635", `{"revision":0,"entries":[{"folder":{"name":"Docs","items":[{"channel":"docs","id":"d1"}]}}]}`)
	var saved shelfResponse
	decode(t, rec, &saved)
	id := saved.Entries[0].Folder.ID

	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 32, 18))); err != nil {
		t.Fatal(err)
	}
	if rec := send(t, deps, http.MethodPut, "/api/shelf/cover?key=folder:"+id, "6635", pic.String()); rec.Code != http.StatusOK {
		t.Fatalf("picture answered %d: %s", rec.Code, rec.Body)
	}
	if rec := send(t, deps, http.MethodPut, "/api/shelf/cover?key=folder:"+id, "6635", "text"); rec.Code != http.StatusBadRequest {
		t.Errorf("text as a picture answered %d, want 400", rec.Code)
	}
	if rec := send(t, deps, http.MethodPost, "/api/shelf/cover/frame?key=folder:"+id+"&at=1", "6635", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a frame for a folder answered %d, want 400", rec.Code)
	}

	var got shelfResponse
	decode(t, get(t, deps, "/api/shelf"), &got)
	if !strings.Contains(got.Entries[0].Folder.Cover, "folder%3A"+id) {
		t.Errorf("folder cover %q, want its own picture", got.Entries[0].Folder.Cover)
	}
}

func TestFolderBorrowsItsFirstEpisodesCover(t *testing.T) {
	deps, _, _ := shelfDeps(t, "6635")
	send(t, deps, http.MethodPut, "/api/shelf", "6635", `{"revision":0,"entries":[{"folder":{"name":"Docs","items":[{"channel":"docs","id":"d1"},{"channel":"docs","id":"d2"}]}}]}`)
	path, err := deps.Covers.File("docs/d2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got shelfResponse
	decode(t, get(t, deps, "/api/shelf"), &got)
	f := got.Entries[0].Folder
	if f.Items[0].Cover != "" || f.Cover != f.Items[1].Cover || f.Cover == "" {
		t.Errorf("folder cover %q, items %q %q", f.Cover, f.Items[0].Cover, f.Items[1].Cover)
	}
}

func TestShelfIsOffWithoutAStore(t *testing.T) {
	rec := get(t, testDeps(), "/api/shelf")
	if rec.Code != http.StatusNotFound {
		t.Errorf("answered %d, want 404", rec.Code)
	}
	var lib libraryResponse
	decode(t, get(t, testDeps(), "/api/library"), &lib)
	if len(lib.Channels) != 3 {
		t.Errorf("the library should list without a shelf: %+v", lib)
	}
}
