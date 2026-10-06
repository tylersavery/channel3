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

func TestUploadWithoutAFilenameIsRejected(t *testing.T) {
	rec := httptest.NewRecorder()
	uploadServer(&fakeUploads{}, "6635").ServeHTTP(rec, rawUpload("6635", "", "clip"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d with no filename, want 400", rec.Code)
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
