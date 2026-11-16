// Package shelf is the On Demand shelf: the episodes picked by hand from the
// library to sit beside the movies on channel 0, some on their own and some
// gathered into a folder per show.
//
// The shelf only points at the library, by channel and video id. Nothing is
// copied, and an item whose video has left the library is skipped rather than
// treated as an error, so it comes back if the video returns.
//
// The shelf is written by the service alone, from the web guide, and changes
// only what the On Demand menu offers. Broadcast never reads it.
package shelf

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/schedule"
)

// Item is one video on the shelf: a channel's library item, and optionally a
// title of its own, which wins over the library's.
type Item struct {
	Channel string `json:"channel"`
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
}

// Ref is the library item it points at.
func (it Item) Ref() Ref { return Ref{Channel: it.Channel, ID: it.ID} }

// Folder gathers items under a name, a show's episodes say. ID is stable
// across renames, so a folder's cover stays with it.
type Folder struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Items []Item `json:"items"`
}

// Entry is one place at the top of the shelf: an item or a folder, never both.
type Entry struct {
	Item   *Item   `json:"item,omitempty"`
	Folder *Folder `json:"folder,omitempty"`
}

// Shelf is the whole shelf, in the order the menu shows it.
//
// Revision counts saves. A save names the revision it was based on and is
// refused when the file has moved on, so two phones editing at once cannot
// silently undo each other.
type Shelf struct {
	Revision int     `json:"revision"`
	Entries  []Entry `json:"entries"`
}

// Ref names one library video.
type Ref struct {
	Channel string
	ID      string
}

// Limits on what a save may hold. They are far past anything picked by hand
// and are only there so a bad request cannot write a file of any size.
const (
	maxItems     = 1000
	maxNameRunes = 80
)

// ErrStale is a save based on a revision the file has moved on from.
var ErrStale = errors.New("the shelf was changed somewhere else; reload it and try again")

// Validate checks a shelf a client wants to save. Every item appears once,
// folders hold only items, and every name is short and not blank.
func Validate(s Shelf) error {
	seen := make(map[Ref]bool)
	folders := make(map[string]bool)
	count := 0
	checkItem := func(it Item, where string) error {
		count++
		if count > maxItems {
			return fmt.Errorf("the shelf may hold at most %d videos", maxItems)
		}
		if !validChannelID(it.Channel) || !library.ValidItemID(it.ID) {
			return fmt.Errorf("%s: %q/%q does not name a library video", where, it.Channel, it.ID)
		}
		if seen[it.Ref()] {
			return fmt.Errorf("%s: %s/%s is on the shelf twice", where, it.Channel, it.ID)
		}
		seen[it.Ref()] = true
		if utf8.RuneCountInString(it.Title) > maxNameRunes {
			return fmt.Errorf("%s: a title may be at most %d characters", where, maxNameRunes)
		}
		return nil
	}
	for i, e := range s.Entries {
		where := fmt.Sprintf("entry %d", i+1)
		switch {
		case (e.Item == nil) == (e.Folder == nil):
			return fmt.Errorf("%s must be an item or a folder", where)
		case e.Item != nil:
			if err := checkItem(*e.Item, where); err != nil {
				return err
			}
		default:
			f := e.Folder
			name := strings.TrimSpace(f.Name)
			if name == "" {
				return fmt.Errorf("%s: a folder needs a name", where)
			}
			if utf8.RuneCountInString(name) > maxNameRunes {
				return fmt.Errorf("%s: a folder name may be at most %d characters", where, maxNameRunes)
			}
			if f.ID != "" {
				if !validFolderID(f.ID) {
					return fmt.Errorf("%s: %q is not a folder id", where, f.ID)
				}
				if folders[f.ID] {
					return fmt.Errorf("%s: the folder id %q is used twice", where, f.ID)
				}
				folders[f.ID] = true
			}
			for _, it := range f.Items {
				if err := checkItem(it, where+" ("+name+")"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validChannelID matches a channel id as the channel config accepts it.
func validChannelID(id string) bool {
	if id == "" || id[0] == '-' {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// validFolderID matches the ids newFolderID makes.
func validFolderID(id string) bool {
	if len(id) == 0 || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'f') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// newFolderID is a short random id for a folder a client has just made.
func newFolderID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Path is where the shelf lives under root.
func Path(root string) string { return filepath.Join(root, "shelf.json") }

// GrownupPath is where the grown-up shelf lives under root: a second shelf,
// opened with its own code, in the same format.
func GrownupPath(root string) string { return filepath.Join(root, "shelf-grownup.json") }

// Store reads and writes the shelf file. It is safe to use from several
// goroutines: the API's handlers and the television's menu both read it.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore is the shelf under root.
func NewStore(root string) *Store { return NewStoreAt(Path(root)) }

// NewStoreAt is the shelf file at path.
func NewStoreAt(path string) *Store { return &Store{path: path} }

// Read is the shelf as saved. A shelf never saved is empty at revision 0.
func (s *Store) Read() (Shelf, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *Store) read() (Shelf, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Shelf{Entries: []Entry{}}, nil
	}
	if err != nil {
		return Shelf{}, err
	}
	var sh Shelf
	if err := json.Unmarshal(data, &sh); err != nil {
		return Shelf{}, fmt.Errorf("read %s: %w", filepath.Base(s.path), err)
	}
	if sh.Entries == nil {
		sh.Entries = []Entry{}
	}
	return sh, nil
}

// Write saves next if it is based on the revision on disk, and returns it as
// saved: one revision on, names trimmed, and new folders given ids.
func (s *Store) Write(next Shelf) (Shelf, error) {
	if err := Validate(next); err != nil {
		return Shelf{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.read()
	if err != nil {
		return Shelf{}, err
	}
	if next.Revision != current.Revision {
		return Shelf{}, ErrStale
	}

	saved := Shelf{Revision: current.Revision + 1, Entries: make([]Entry, 0, len(next.Entries))}
	for _, e := range next.Entries {
		if e.Item != nil {
			it := tidy(*e.Item)
			saved.Entries = append(saved.Entries, Entry{Item: &it})
			continue
		}
		f := Folder{ID: e.Folder.ID, Name: strings.TrimSpace(e.Folder.Name), Items: make([]Item, 0, len(e.Folder.Items))}
		if f.ID == "" {
			if f.ID, err = newFolderID(); err != nil {
				return Shelf{}, err
			}
		}
		for _, it := range e.Folder.Items {
			f.Items = append(f.Items, tidy(it))
		}
		saved.Entries = append(saved.Entries, Entry{Folder: &f})
	}

	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return Shelf{}, err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return Shelf{}, err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return Shelf{}, err
	}
	return saved, nil
}

// tidy trims an item's title, so a title of spaces is no title.
func tidy(it Item) Item {
	it.Title = strings.TrimSpace(it.Title)
	return it
}

// Contains reports whether ref is anywhere on the shelf.
func (s Shelf) Contains(ref Ref) bool {
	for _, e := range s.Entries {
		if e.Item != nil && e.Item.Ref() == ref {
			return true
		}
		if e.Folder != nil {
			for _, it := range e.Folder.Items {
				if it.Ref() == ref {
					return true
				}
			}
		}
	}
	return false
}

// Library is every playable video the station has, by reference.
type Library map[Ref]schedule.Item

// NewLibrary indexes the station's channels.
func NewLibrary(channels []schedule.Channel) Library {
	lib := make(Library)
	for _, ch := range channels {
		if ch.Guide {
			// The guide's items are its music channel's, borrowed.
			continue
		}
		for _, it := range ch.Items {
			lib[Ref{Channel: ch.ID, ID: it.ID}] = it
		}
	}
	return lib
}

// Video is a shelf item with its library video, ready to show and play.
type Video struct {
	Item
	// Title is the shelf's own title when it has one, else the library's.
	Title string
	Video schedule.Item
}

// Shown is one place at the top of the menu: a video or a folder of them.
type Shown struct {
	Video  *Video
	Folder *ShownFolder
}

// ShownFolder is a folder with the videos in it that can play.
type ShownFolder struct {
	ID     string
	Name   string
	Videos []Video
}

// Playable is what the menu shows: every item whose video is in lib, and
// every folder with at least one of them. The rest are left out, not lost.
func Playable(s Shelf, lib Library) []Shown {
	var out []Shown
	video := func(it Item) (Video, bool) {
		v, ok := lib[it.Ref()]
		if !ok {
			return Video{}, false
		}
		title := it.Title
		if title == "" {
			title = v.Title
		}
		return Video{Item: it, Title: title, Video: v}, true
	}
	for _, e := range s.Entries {
		if e.Item != nil {
			if v, ok := video(*e.Item); ok {
				out = append(out, Shown{Video: &v})
			}
			continue
		}
		f := ShownFolder{ID: e.Folder.ID, Name: e.Folder.Name}
		for _, it := range e.Folder.Items {
			if v, ok := video(it); ok {
				f.Videos = append(f.Videos, v)
			}
		}
		if len(f.Videos) > 0 {
			out = append(out, Shown{Folder: &f})
		}
	}
	return out
}
