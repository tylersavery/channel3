package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tylersavery/channel3/internal/shelf"
)

const grownupPIN = "4217"

// fakeGrownup records what the endpoints ask of the worker.
type fakeGrownup struct {
	added   []string
	deleted []string
}

func (f *fakeGrownup) Add(url string) (Download, error) {
	if !strings.HasPrefix(url, "https://") {
		return Download{}, errors.New("paste a link that starts with https://")
	}
	f.added = append(f.added, url)
	return Download{Key: len(f.added), URL: url, State: DownloadQueued}, nil
}

func (f *fakeGrownup) Downloads() []Download {
	return []Download{{Key: 1, URL: "https://youtu.be/x", State: DownloadDone, ID: "x"}}
}

func (f *fakeGrownup) Delete(id string) error {
	if id != "x" {
		return ErrNoSuchVideo
	}
	f.deleted = append(f.deleted, id)
	return nil
}

// grownupDeps is shelfDeps with the grown-up shelf and its worker on.
func grownupDeps(t *testing.T) (Deps, *fakeGrownup) {
	t.Helper()
	deps, _, _ := shelfDeps(t, "6635")
	deps.GrownupShelf = shelf.NewStoreAt(shelf.GrownupPath(t.TempDir()))
	deps.GrownupPIN = grownupPIN
	f := &fakeGrownup{}
	deps.Grownup = f
	return deps, f
}

func TestEachShelfTakesItsOwnCode(t *testing.T) {
	deps, _ := grownupDeps(t)
	body := `{"revision":0,"entries":[{"item":{"channel":"trains","id":"abc"}}]}`

	if rec := send(t, deps, http.MethodPut, "/api/shelf?shelf=grownup", "6635", body); rec.Code != http.StatusForbidden {
		t.Errorf("Lane's code saved the grown-up shelf: %d", rec.Code)
	}
	if rec := send(t, deps, http.MethodPut, "/api/shelf", grownupPIN, body); rec.Code != http.StatusForbidden {
		t.Errorf("the grown-up code saved Lane's shelf: %d", rec.Code)
	}
	if rec := send(t, deps, http.MethodPut, "/api/shelf?shelf=grownup", grownupPIN, body); rec.Code != http.StatusOK {
		t.Fatalf("saving the grown-up shelf: %d %s", rec.Code, rec.Body)
	}

	var lane, grown shelfResponse
	decode(t, get(t, deps, "/api/shelf"), &lane)
	decode(t, get(t, deps, "/api/shelf?shelf=grownup"), &grown)
	if len(lane.Entries) != 0 || len(grown.Entries) != 1 {
		t.Errorf("Lane's has %d, grown-up %d; want the save on the grown-up shelf only", len(lane.Entries), len(grown.Entries))
	}

	var lib struct {
		Channels []struct {
			ID    string `json:"id"`
			Items []struct {
				ID      string `json:"id"`
				OnShelf bool   `json:"on_shelf"`
			} `json:"items"`
		} `json:"channels"`
	}
	decode(t, get(t, deps, "/api/library?shelf=grownup"), &lib)
	for _, ch := range lib.Channels {
		for _, it := range ch.Items {
			if it.OnShelf != (ch.ID == "trains" && it.ID == "abc") {
				t.Errorf("%s/%s on_shelf %v against the grown-up shelf", ch.ID, it.ID, it.OnShelf)
			}
		}
	}
}

func TestShelfParameter(t *testing.T) {
	deps, _, _ := shelfDeps(t, "6635")
	if rec := get(t, deps, "/api/shelf?shelf=grownup"); rec.Code != http.StatusNotFound {
		t.Errorf("a box without a grown-up shelf answered %d, want 404", rec.Code)
	}
	if rec := get(t, deps, "/api/shelf?shelf=nosuch"); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown shelf answered %d, want 400", rec.Code)
	}
	if rec := get(t, deps, "/api/grownup/downloads"); rec.Code != http.StatusNotFound {
		t.Errorf("downloads without a grown-up shelf answered %d, want 404", rec.Code)
	}
}

func TestPastingALink(t *testing.T) {
	deps, f := grownupDeps(t)
	link := `{"url":"https://www.youtube.com/watch?v=x"}`
	if rec := send(t, deps, http.MethodPost, "/api/grownup/downloads", "6635", link); rec.Code != http.StatusForbidden {
		t.Errorf("Lane's code pasted a link: %d", rec.Code)
	}
	rec := send(t, deps, http.MethodPost, "/api/grownup/downloads", grownupPIN, link)
	if rec.Code != http.StatusAccepted || len(f.added) != 1 {
		t.Fatalf("pasting: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, deps, http.MethodPost, "/api/grownup/downloads", grownupPIN, `{"url":"nope"}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "https://") {
		t.Errorf("a bad link: %d %s, want 400 with the reason", rec.Code, rec.Body)
	}
	if rec := send(t, deps, http.MethodPost, "/api/grownup/downloads", grownupPIN, `not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("a body that is not JSON: %d", rec.Code)
	}

	var list downloadsResponse
	decode(t, get(t, deps, "/api/grownup/downloads"), &list)
	if len(list.Downloads) != 1 || list.Downloads[0].State != DownloadDone {
		t.Errorf("downloads %+v", list.Downloads)
	}
}

func TestDeletingAVideo(t *testing.T) {
	deps, f := grownupDeps(t)
	if rec := send(t, deps, http.MethodDelete, "/api/grownup/videos?id=x", "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a delete without the code: %d", rec.Code)
	}
	if rec := send(t, deps, http.MethodDelete, "/api/grownup/videos?id=y", grownupPIN, ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleting a video that is not there: %d", rec.Code)
	}
	if rec := send(t, deps, http.MethodGet, "/api/grownup/videos?id=x", grownupPIN, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("a GET: %d", rec.Code)
	}
	if rec := send(t, deps, http.MethodDelete, "/api/grownup/videos?id=x", grownupPIN, ""); rec.Code != http.StatusOK || len(f.deleted) != 1 {
		t.Errorf("deleting: %d %s", rec.Code, rec.Body)
	}
}
