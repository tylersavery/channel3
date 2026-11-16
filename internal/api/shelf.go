package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/tylersavery/channel3/internal/shelf"
)

// The On Demand shelf endpoints. Reading is open like the rest of the guide.
// Saving the shelf and changing a cover carry the upload PIN and exist only
// when the box has one, like /api/upload. None of them changes what plays:
// they change what the On Demand menu offers on channel 0, and nothing else.

// ShelfStore is the shelf file. cmd/channel3 satisfies it with shelf.Store.
type ShelfStore interface {
	Read() (shelf.Shelf, error)
	Write(shelf.Shelf) (shelf.Shelf, error)
}

// maxShelfBody is the largest shelf a client may save. A thousand items is
// well under this.
const maxShelfBody = 1 << 20

// maxCoverBody is the largest picture accepted as a cover.
const maxCoverBody = 32 << 20

// libraryItem is one video of GET /api/library.
type libraryItem struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist,omitempty"`
	Duration float64 `json:"duration"`
	OnShelf  bool    `json:"on_shelf"`
}

// libraryChannel is one channel of GET /api/library.
type libraryChannel struct {
	channelJSON
	// Hidden marks a channel off the dial, whose videos the shelf can still
	// offer.
	Hidden bool          `json:"hidden,omitempty"`
	Items  []libraryItem `json:"items"`
}

// libraryResponse is GET /api/library.
type libraryResponse struct {
	Channels []libraryChannel `json:"channels"`
}

// shelfItemJSON is one item of GET /api/shelf. A client saving the shelf
// sends channel, id and title back and the rest is ignored.
type shelfItemJSON struct {
	Channel string `json:"channel"`
	ID      string `json:"id"`
	// Title is the shelf's own title, empty when it uses the library's.
	Title        string  `json:"title,omitempty"`
	LibraryTitle string  `json:"library_title"`
	Duration     float64 `json:"duration"`
	// Missing says the video has left the library. It stays on the shelf
	// and comes back to the menu if the video returns.
	Missing bool `json:"missing"`
	// Cover is the cover's URL, empty while it is being made.
	Cover string `json:"cover"`
}

type shelfFolderJSON struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Cover string          `json:"cover"`
	Items []shelfItemJSON `json:"items"`
}

type shelfEntryJSON struct {
	Item   *shelfItemJSON   `json:"item,omitempty"`
	Folder *shelfFolderJSON `json:"folder,omitempty"`
}

// shelfResponse is GET /api/shelf, and the answer to a save.
type shelfResponse struct {
	Revision int              `json:"revision"`
	Entries  []shelfEntryJSON `json:"entries"`
	// Editable says the box has a PIN, so the shelf can be saved.
	Editable bool `json:"editable"`
}

// handleLibrary lists every video the station can play, by channel, and
// whether each is on the shelf.
func (s *server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	var current shelf.Shelf
	if s.deps.Shelf != nil {
		var err error
		if current, err = s.deps.Shelf.Read(); err != nil {
			slog.Error("could not read the shelf", "error", err)
			writeError(w, http.StatusInternalServerError, "could not read the shelf")
			return
		}
	}
	channels := s.sorted()
	out := libraryResponse{Channels: make([]libraryChannel, 0, len(channels))}
	for _, ch := range channels {
		entry := libraryChannel{channelJSON: identify(ch), Hidden: ch.Hidden, Items: make([]libraryItem, 0, len(ch.Items))}
		for _, it := range ch.Items {
			entry.Items = append(entry.Items, libraryItem{
				ID:       it.ID,
				Title:    it.Title,
				Artist:   it.Artist,
				Duration: seconds(it.Duration),
				OnShelf:  current.Contains(shelf.Ref{Channel: ch.ID, ID: it.ID}),
			})
		}
		out.Channels = append(out.Channels, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleShelf reads the shelf, or saves it with the PIN.
func (s *server) handleShelf(w http.ResponseWriter, r *http.Request) {
	if s.deps.Shelf == nil {
		writeError(w, http.StatusNotFound, "there is no shelf on this box")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		current, err := s.deps.Shelf.Read()
		if err != nil {
			slog.Error("could not read the shelf", "error", err)
			writeError(w, http.StatusInternalServerError, "could not read the shelf")
			return
		}
		writeJSON(w, http.StatusOK, s.shelfJSON(current))
	case http.MethodPut:
		if !s.shelfWriter(w, r) {
			return
		}
		var next shelf.Shelf
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxShelfBody))
		if err := dec.Decode(&next); err != nil {
			writeError(w, http.StatusBadRequest, "the body is not a shelf: "+err.Error())
			return
		}
		saved, err := s.deps.Shelf.Write(next)
		if errors.Is(err, shelf.ErrStale) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Info("shelf saved", "from", r.RemoteAddr, "revision", saved.Revision, "entries", len(saved.Entries))
		if s.deps.ShelfSaved != nil {
			s.deps.ShelfSaved()
		}
		writeJSON(w, http.StatusOK, s.shelfJSON(saved))
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeError(w, http.StatusMethodNotAllowed, "read the shelf with GET, save it with PUT")
	}
}

// handleCover serves a cover, or replaces it with an uploaded picture.
func (s *server) handleCover(w http.ResponseWriter, r *http.Request) {
	if s.deps.Shelf == nil || s.deps.Covers == nil {
		writeError(w, http.StatusNotFound, "there is no shelf on this box")
		return
	}
	key := r.URL.Query().Get("key")
	path, err := s.deps.Covers.File(key)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "that cover is not made yet")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read the cover")
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read the cover")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		// The URL carries the cover's time, so a changed cover is a new URL
		// and the old one may be kept.
		w.Header().Set("Cache-Control", "max-age=86400")
		http.ServeContent(w, r, "", info.ModTime(), f)
	case http.MethodPut:
		if !s.shelfWriter(w, r) || !s.onShelf(w, key) {
			return
		}
		err := s.deps.Covers.FromPicture(key, http.MaxBytesReader(w, r.Body, maxCoverBody))
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "the picture is larger than 32 MB")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Info("cover replaced with a picture", "from", r.RemoteAddr, "key", key)
		writeJSON(w, http.StatusOK, map[string]string{"cover": s.coverURL(key)})
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeError(w, http.StatusMethodNotAllowed, "read a cover with GET, replace it with PUT")
	}
}

// handleCoverFrame makes an item's cover from another frame of its video.
func (s *server) handleCoverFrame(w http.ResponseWriter, r *http.Request) {
	if s.deps.Shelf == nil || s.deps.Covers == nil {
		writeError(w, http.StatusNotFound, "there is no shelf on this box")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "ask for another frame with POST")
		return
	}
	key := r.URL.Query().Get("key")
	ref, folder, err := shelf.ParseKey(key)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if folder != "" {
		writeError(w, http.StatusBadRequest, "a folder has no video to take a frame from; upload a picture instead")
		return
	}
	at, err := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
	if err != nil || at < 0 {
		writeError(w, http.StatusBadRequest, "at must be a number of seconds into the video")
		return
	}
	if !s.shelfWriter(w, r) || !s.onShelf(w, key) {
		return
	}
	video, ok := shelf.NewLibrary(s.deps.Channels())[ref]
	if !ok {
		writeError(w, http.StatusNotFound, "that video is not in the library")
		return
	}
	offset := min(time.Duration(at*float64(time.Second)), max(video.Duration-time.Second, 0))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := s.deps.Covers.FromFrame(ctx, key, video.Path, offset); err != nil {
		slog.Warn("could not grab a frame for a cover", "key", key, "at", offset, "error", err)
		writeError(w, http.StatusInternalServerError, "could not take a frame there")
		return
	}
	slog.Info("cover replaced with a frame", "from", r.RemoteAddr, "key", key, "at", offset.Round(time.Second))
	writeJSON(w, http.StatusOK, map[string]string{"cover": s.coverURL(key)})
}

// shelfWriter checks a request that changes the shelf carries the PIN, and
// answers it when not.
func (s *server) shelfWriter(w http.ResponseWriter, r *http.Request) bool {
	if s.deps.UploadPIN == "" {
		writeError(w, http.StatusNotFound, "editing the shelf is off on this box")
		return false
	}
	pin := r.Header.Get("X-Upload-PIN")
	if subtle.ConstantTimeCompare([]byte(pin), []byte(s.deps.UploadPIN)) != 1 {
		slog.Warn("shelf change refused: wrong PIN", "from", r.RemoteAddr, "path", r.URL.Path)
		writeError(w, http.StatusForbidden, "wrong PIN")
		return false
	}
	return true
}

// onShelf checks a cover key belongs to something on the shelf, so covers
// cannot be made for anything else, and answers the request when not.
func (s *server) onShelf(w http.ResponseWriter, key string) bool {
	current, err := s.deps.Shelf.Read()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read the shelf")
		return false
	}
	ref, folder, _ := shelf.ParseKey(key)
	for _, e := range current.Entries {
		if folder != "" && e.Folder != nil && e.Folder.ID == folder {
			return true
		}
	}
	if folder == "" && current.Contains(ref) {
		return true
	}
	writeError(w, http.StatusNotFound, "that is not on the shelf")
	return false
}

// shelfJSON is the shelf as GET /api/shelf reports it.
func (s *server) shelfJSON(sh shelf.Shelf) shelfResponse {
	lib := shelf.NewLibrary(s.deps.Channels())
	item := func(it shelf.Item) shelfItemJSON {
		key := shelf.ItemKey(it.Ref())
		out := shelfItemJSON{Channel: it.Channel, ID: it.ID, Title: it.Title, Cover: s.coverURL(key)}
		if v, ok := lib[it.Ref()]; ok {
			out.LibraryTitle, out.Duration = v.Title, seconds(v.Duration)
		} else {
			out.Missing = true
		}
		return out
	}
	out := shelfResponse{Revision: sh.Revision, Entries: make([]shelfEntryJSON, 0, len(sh.Entries)),
		Editable: s.deps.UploadPIN != ""}
	for _, e := range sh.Entries {
		if e.Item != nil {
			it := item(*e.Item)
			out.Entries = append(out.Entries, shelfEntryJSON{Item: &it})
			continue
		}
		f := shelfFolderJSON{ID: e.Folder.ID, Name: e.Folder.Name, Items: make([]shelfItemJSON, 0, len(e.Folder.Items)),
			Cover: s.coverURL(shelf.FolderKey(e.Folder.ID))}
		for _, it := range e.Folder.Items {
			f.Items = append(f.Items, item(it))
		}
		if f.Cover == "" {
			// A folder without a picture of its own shows its first
			// episode's, as the television does.
			for _, it := range f.Items {
				if it.Cover != "" {
					f.Cover = it.Cover
					break
				}
			}
		}
		out.Entries = append(out.Entries, shelfEntryJSON{Folder: &f})
	}
	return out
}

// coverURL is where key's cover is served, with its time so a changed cover
// is a new URL, or empty when it has none.
func (s *server) coverURL(key string) string {
	if s.deps.Covers == nil {
		return ""
	}
	path, err := s.deps.Covers.File(key)
	if err != nil {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("/api/shelf/cover?key=%s&v=%d", url.QueryEscape(key), info.ModTime().UnixMilli())
}
