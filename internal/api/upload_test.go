package api

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeUploads records what the handler hands over.
type fakeUploads struct {
	name string
	body string
	err  error
}

func (f *fakeUploads) Accept(name string, body io.Reader) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	f.name, f.body = name, string(data)
	return "stored " + name, nil
}

func uploadServer(up Uploads, pin string) http.Handler {
	return New(Deps{Uploads: up, UploadPIN: pin})
}

func rawUpload(pin, name, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(body))
	if pin != "" {
		r.Header.Set("X-Upload-PIN", pin)
	}
	if name != "" {
		r.Header.Set("X-Filename", name)
	}
	return r
}

func TestUploadIsOffWithoutAPIN(t *testing.T) {
	up := &fakeUploads{}
	rec := httptest.NewRecorder()
	uploadServer(up, "").ServeHTTP(rec, rawUpload("1234", "IMG_0001.MOV", "clip"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d with no PIN configured, want 404", rec.Code)
	}
	if up.name != "" {
		t.Error("a clip was stored with uploads off")
	}
}

func TestUploadRefusesGET(t *testing.T) {
	rec := httptest.NewRecorder()
	uploadServer(&fakeUploads{}, "6635").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/upload", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET answered %d, want 405", rec.Code)
	}
}

func TestUploadNeedsTheRightPIN(t *testing.T) {
	for _, pin := range []string{"", "0000", "66355"} {
		up := &fakeUploads{}
		rec := httptest.NewRecorder()
		uploadServer(up, "6635").ServeHTTP(rec, rawUpload(pin, "IMG_0001.MOV", "clip"))
		if rec.Code != http.StatusForbidden {
			t.Errorf("PIN %q answered %d, want 403", pin, rec.Code)
		}
		if up.name != "" {
			t.Errorf("PIN %q stored a clip", pin)
		}
	}
}

// TestShortcutUploadIsAccepted is the request an iOS Shortcut sends: the clip
// as the raw body, named by a header, with the PIN in a header.
func TestShortcutUploadIsAccepted(t *testing.T) {
	up := &fakeUploads{}
	rec := httptest.NewRecorder()
	uploadServer(up, "6635").ServeHTTP(rec, rawUpload("6635", "IMG_0001.MOV", "the clip's bytes"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202: %s", rec.Code, rec.Body)
	}
	if up.name != "IMG_0001.MOV" || up.body != "the clip's bytes" {
		t.Errorf("stored %q with %q, want the named clip and its bytes", up.name, up.body)
	}
	if !strings.Contains(rec.Body.String(), "stored IMG_0001.MOV") {
		t.Errorf("the response %s does not say what was stored", rec.Body)
	}
}

// TestUploadWithoutAFilenameIsNamedFromItsType is the Shortcut that did not
// send X-Filename: the clip is still taken, named for its content type.
func TestUploadWithoutAFilenameIsNamedFromItsType(t *testing.T) {
	for contentType, want := range map[string]string{
		"video/quicktime": "Home video.mov",
		"video/mp4":       "Home video.mp4",
		"":                "Home video.mov",
	} {
		up := &fakeUploads{}
		r := rawUpload("6635", "", "clip")
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		uploadServer(up, "6635").ServeHTTP(rec, r)
		if rec.Code != http.StatusAccepted || up.name != want {
			t.Errorf("%q: status %d, stored as %q, want 202 and %q", contentType, rec.Code, up.name, want)
		}
	}
}

func TestMultipartUploadIsAccepted(t *testing.T) {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	if err := form.WriteField("pin", "6635"); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("file", "Beach.mov")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("beach clip")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	r.Header.Set("Content-Type", form.FormDataContentType())

	up := &fakeUploads{}
	rec := httptest.NewRecorder()
	uploadServer(up, "6635").ServeHTTP(rec, r)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202: %s", rec.Code, rec.Body)
	}
	if up.name != "Beach.mov" || up.body != "beach clip" {
		t.Errorf("stored %q with %q, want the form's file", up.name, up.body)
	}
}

func TestRejectedClipIsABadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	up := &fakeUploads{err: errors.New(`"notes.txt" is not a video file`)}
	uploadServer(up, "6635").ServeHTTP(rec, rawUpload("6635", "notes.txt", "text"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not a video") {
		t.Errorf("status %d, %s, want 400 saying why", rec.Code, rec.Body)
	}
}

// TestEverythingElseStaysReadOnly checks the exception did not leak: the
// guide's endpoints still refuse a POST.
func TestEverythingElseStaysReadOnly(t *testing.T) {
	for _, path := range []string{"/api/now", "/api/guide", "/api/channels", "/"} {
		rec := httptest.NewRecorder()
		uploadServer(&fakeUploads{}, "6635").ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s answered %d, want 405", path, rec.Code)
		}
	}
}

// TestTextBodyIsRefusedWithAHint is a Shortcut whose File body held the clip's
// name rather than the clip.
func TestTextBodyIsRefusedWithAHint(t *testing.T) {
	for _, contentType := range []string{"text/plain", "application/json", "application/x-www-form-urlencoded"} {
		up := &fakeUploads{}
		r := rawUpload("6635", "", "IMG_1234")
		r.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		uploadServer(up, "6635").ServeHTTP(rec, r)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "set the request body to File") {
			t.Errorf("%s: status %d, %s, want 400 with the fix", contentType, rec.Code, rec.Body)
		}
		if up.name != "" {
			t.Errorf("%s: a text body was stored", contentType)
		}
	}
}

// slowBody hands out its bytes a chunk at a time with a pause before each, the
// way a video arrives over Wi-Fi.
type slowBody struct {
	chunks int
	pause  time.Duration
}

func (b *slowBody) Read(p []byte) (int, error) {
	if b.chunks == 0 {
		return 0, io.EOF
	}
	time.Sleep(b.pause)
	b.chunks--
	return copy(p, "video bytes "), nil
}

// TestSlowUploadOutlastsTheServerTimeout is the bug a phone hit: serve gives
// every request ten seconds to arrive, and a video over Wi-Fi takes longer.
// The upload handler extends its own deadline.
func TestSlowUploadOutlastsTheServerTimeout(t *testing.T) {
	up := &fakeUploads{}
	srv := httptest.NewUnstartedServer(uploadServer(up, "6635"))
	srv.Config.ReadTimeout = 200 * time.Millisecond
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/upload", &slowBody{chunks: 6, pause: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Upload-PIN", "6635")
	req.Header.Set("X-Filename", "IMG_0001.MOV")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, msg)
	}
	if up.body != strings.Repeat("video bytes ", 6) {
		t.Errorf("stored %q", up.body)
	}
}
