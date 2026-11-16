package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// The grown-up shelf's own endpoints: paste a link to download a video onto
// it, see what is downloading, and delete a video from it. Every change carries
// the grown-up code. None of them changes what broadcast plays: the videos go
// into a hidden channel, and onto the grown-up shelf only.

// Download is one pasted link and how far it has got.
type Download struct {
	// Key tells two pastes of the same link apart.
	Key   int    `json:"key"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	// State is queued, downloading, done or failed.
	State string `json:"state"`
	// Error is why a failed download failed.
	Error string `json:"error,omitempty"`
	// ID is the video's id once it is known.
	ID string `json:"id,omitempty"`
}

// Download states.
const (
	DownloadQueued      = "queued"
	DownloadDownloading = "downloading"
	DownloadDone        = "done"
	DownloadFailed      = "failed"
)

// Grownup downloads pasted links onto the grown-up shelf and deletes them
// again. cmd/channel3 satisfies it with a background worker.
type Grownup interface {
	// Add queues a link. An error is a link that cannot be downloaded,
	// worded for the person who pasted it.
	Add(url string) (Download, error)
	// Downloads is what was pasted recently, newest first.
	Downloads() []Download
	// Delete takes a downloaded video off the grown-up shelf and the disk.
	Delete(id string) error
}

// ErrNoSuchVideo is a Delete of a video that is not on the grown-up shelf or
// in its channel.
var ErrNoSuchVideo = errors.New("that video is not on the grown-up shelf")

// maxLinkBody is the largest add request: a URL and a little JSON.
const maxLinkBody = 16 << 10

type addRequest struct {
	URL string `json:"url"`
}

type downloadsResponse struct {
	Downloads []Download `json:"downloads"`
}

// handleGrownupDownloads lists the recent downloads, or queues a new one.
func (s *server) handleGrownupDownloads(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.grownup(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, http.StatusOK, downloadsResponse{Downloads: s.deps.Grownup.Downloads()})
	case http.MethodPost:
		if !s.shelfWriter(w, r, sh) {
			return
		}
		var req addRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLinkBody)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, `send {"url": "https://…"}`)
			return
		}
		d, err := s.deps.Grownup.Add(req.URL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Info("grown-up link queued", "from", r.RemoteAddr, "url", d.URL)
		writeJSON(w, http.StatusAccepted, d)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeError(w, http.StatusMethodNotAllowed, "list downloads with GET, paste a link with POST")
	}
}

// handleGrownupVideo deletes one downloaded video.
func (s *server) handleGrownupVideo(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.grownup(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		writeError(w, http.StatusMethodNotAllowed, "delete a video with DELETE")
		return
	}
	if !s.shelfWriter(w, r, sh) {
		return
	}
	id := r.URL.Query().Get("id")
	err := s.deps.Grownup.Delete(id)
	if errors.Is(err, ErrNoSuchVideo) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		slog.Error("could not delete a grown-up video", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not delete it: "+err.Error())
		return
	}
	slog.Info("grown-up video deleted", "from", r.RemoteAddr, "id", id)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// grownup is the grown-up shelf for these endpoints, and answers the request
// when the box has none.
func (s *server) grownup(w http.ResponseWriter, _ *http.Request) (chosenShelf, bool) {
	if s.deps.GrownupShelf == nil || s.deps.Grownup == nil {
		writeError(w, http.StatusNotFound, "there is no grown-up shelf on this box")
		return chosenShelf{}, false
	}
	return chosenShelf{name: "grown-up", store: s.deps.GrownupShelf, pin: s.deps.GrownupPIN}, true
}
