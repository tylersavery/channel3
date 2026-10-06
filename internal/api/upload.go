package api

import (
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
)

// Uploads receives home video clips. cmd/channel3 satisfies it with the inbox
// its background worker prepares clips from.
type Uploads interface {
	// Accept stores one clip, reading body to the end, and returns the name
	// it was stored under. A name that is not a video is an error.
	Accept(name string, body io.Reader) (string, error)
}

// maxUpload is the largest clip accepted. A ten minute 4K clip from an iPhone
// is about 4 GB; past this the upload is cut off rather than filling the disk.
const maxUpload = 8 << 30

// handleUpload stores a home video sent from a phone.
//
// This is the one request that changes anything, and all it changes is the
// Home Movies channel's inbox: nothing here tunes, pauses or seeks. It answers
// only when serve was given a PIN, and only to a request that carries it, so a
// guest on the home Wi-Fi cannot put a clip on the television.
//
// The clip is the raw request body, named by an X-Filename header, which is
// what an iOS Shortcut's Get Contents of URL sends with a File body. A
// multipart form with a file field also works. The PIN travels in X-Upload-PIN,
// or a pin form field.
func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Every attempt is logged with why it was refused, because the phone's
	// Shortcut shows the person sending little more than that it failed.
	reject := func(status int, message string) {
		slog.Warn("upload refused", "from", r.RemoteAddr, "status", status, "reason", message,
			"content type", r.Header.Get("Content-Type"), "filename", r.Header.Get("X-Filename"),
			"length", r.ContentLength)
		writeError(w, status, message)
	}
	slog.Info("upload attempt", "from", r.RemoteAddr, "method", r.Method,
		"content type", r.Header.Get("Content-Type"), "length", r.ContentLength)
	if s.deps.Uploads == nil || s.deps.UploadPIN == "" {
		reject(http.StatusNotFound, "uploads are off on this box")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		reject(http.StatusMethodNotAllowed, "send the clip with POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)

	name, body, pin, err := uploadParts(r)
	if err != nil {
		reject(http.StatusBadRequest, err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(pin), []byte(s.deps.UploadPIN)) != 1 {
		reject(http.StatusForbidden, "wrong PIN")
		return
	}

	stored, err := s.deps.Uploads.Accept(name, body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reject(http.StatusRequestEntityTooLarge, "the clip is larger than 8 GB")
			return
		}
		reject(http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"stored": stored,
		"status": "preparing; it will be on Home Movies in a few minutes",
	})
}

// uploadParts finds the clip's name, its bytes and the PIN in either form of
// request.
func uploadParts(r *http.Request) (string, io.Reader, string, error) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		name := strings.TrimSpace(r.Header.Get("X-Filename"))
		if name == "" {
			return "", nil, "", errors.New("name the clip in an X-Filename header")
		}
		return name, r.Body, r.Header.Get("X-Upload-PIN"), nil
	}

	reader, err := r.MultipartReader()
	if err != nil {
		return "", nil, "", err
	}
	pin := r.Header.Get("X-Upload-PIN")
	for {
		part, err := reader.NextPart()
		if err != nil {
			return "", nil, "", errors.New("the form has no file field")
		}
		switch part.FormName() {
		case "pin":
			value, err := io.ReadAll(io.LimitReader(part, 64))
			if err != nil {
				return "", nil, "", err
			}
			pin = strings.TrimSpace(string(value))
		case "file":
			// The file has to be the last field read, since its bytes are
			// streamed rather than buffered: a PIN field must come first.
			return part.FileName(), part, pin, nil
		}
	}
}
