package library

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Item is one playable video: what the schedule orders and what mpv loads.
type Item struct {
	ID       string
	Title    string
	Artist   string // a song's artist; empty for video
	Path     string // absolute path to the video file
	Duration time.Duration
	Source   string
}

// Index holds the playable items of every channel it was scanned for.
type Index struct {
	items map[string][]Item
}

// Items returns one channel's playable items, ordered by item id. A channel that
// was not scanned, or has nothing playable, returns nil.
//
// The result is a copy. Callers shuffle it into a schedule, and an in-place
// shuffle of the index's own slice would make the next scan's order depend on
// what the last caller did with it.
func (ix Index) Items(channelID string) []Item {
	return slices.Clone(ix.items[channelID])
}

// LibraryDir returns the directory holding downloaded media under root.
func LibraryDir(root string) string {
	return filepath.Join(root, "library")
}

// ChannelDir returns the directory holding one channel's media and sidecars.
func ChannelDir(root, channelID string) string {
	return filepath.Join(LibraryDir(root), channelID)
}

// Scan reads the sidecars of every channel under root and returns the items that
// can actually be played right now.
//
// An item is excluded, with a logged reason, when its status is not ok, when the
// file it names is missing, or when its duration is not positive. A channel
// directory that does not exist yields zero items rather than an error, because
// a channel with nothing ingested yet is a normal state that shows the Please
// Stand By card. Only a filesystem failure is an error.
//
// The order is stable across scans so that the schedule's seeded shuffle is
// reproducible for a given library.
func Scan(root string, channels []Channel) (Index, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Index{}, fmt.Errorf("resolve root %q: %w", root, err)
	}

	ix := Index{items: make(map[string][]Item, len(channels))}
	for _, ch := range channels {
		items, err := scanChannel(absRoot, ch)
		if err != nil {
			return Index{}, err
		}
		ix.items[ch.ID] = items
	}
	return ix, nil
}

// scanChannel collects one channel's playable items.
func scanChannel(absRoot string, ch Channel) ([]Item, error) {
	dir := ChannelDir(absRoot, ch.ID)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		slog.Info("channel has nothing ingested yet", "channel", ch.ID, "dir", dir)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read library directory for channel %s: %w", ch.ID, err)
	}

	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		sidecar, err := ReadSidecar(path)
		if err != nil {
			slog.Warn("excluding item: sidecar is unreadable", "channel", ch.ID, "sidecar", path, "err", err)
			continue
		}
		item, ok := itemFromSidecar(absRoot, ch.ID, dir, sidecar)
		if !ok {
			continue
		}
		items = append(items, item)
	}

	slices.SortStableFunc(items, func(a, b Item) int { return strings.Compare(a.ID, b.ID) })
	return items, nil
}

// itemFromSidecar turns one sidecar into a playable item, or logs why it cannot.
func itemFromSidecar(absRoot, channelID, dir string, s Sidecar) (Item, bool) {
	if s.Status != StatusOK {
		slog.Warn("excluding item: status is not ok",
			"channel", channelID, "id", s.ID, "status", string(s.Status), "reason", s.Error)
		return Item{}, false
	}
	if s.File == "" {
		slog.Warn("excluding item: sidecar names no file", "channel", channelID, "id", s.ID)
		return Item{}, false
	}

	path := s.File
	if !filepath.IsAbs(path) {
		resolved, ok := insideRoot(absRoot, dir, path)
		if !ok {
			slog.Warn("excluding item: sidecar file escapes the library root",
				"channel", channelID, "id", s.ID, "file", s.File, "root", absRoot)
			return Item{}, false
		}
		path = resolved
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		slog.Warn("excluding item: file is missing", "channel", channelID, "id", s.ID, "path", path)
		return Item{}, false
	case err != nil:
		slog.Warn("excluding item: file cannot be read",
			"channel", channelID, "id", s.ID, "path", path, "err", err)
		return Item{}, false
	case !info.Mode().IsRegular():
		slog.Warn("excluding item: file is not a regular file", "channel", channelID, "id", s.ID, "path", path)
		return Item{}, false
	}

	if s.Duration <= 0 {
		slog.Warn("excluding item: duration is not positive",
			"channel", channelID, "id", s.ID, "duration", s.Duration)
		return Item{}, false
	}

	return Item{
		ID:       s.ID,
		Title:    s.Title,
		Artist:   s.Artist,
		Path:     path,
		Duration: durationFromSeconds(s.Duration),
		Source:   s.Source,
	}, true
}

// insideRoot resolves a relative sidecar file against the sidecar's own
// directory and reports whether it stayed inside the library root.
//
// The root rather than the channel directory is the boundary, because a local
// file under <root>/local is recorded as ../../local/steam-engines.mp4 so that
// the library can be copied between machines. A sidecar is a file on disk and
// its "file" field is not validated by anything upstream, so a hand written or
// damaged one climbing past the root would otherwise put an arbitrary file on a
// children's television. An absolute path, which is what a file:// source
// outside the root records, never reaches here: those are deliberate and point
// at media that was never copied into the library.
func insideRoot(absRoot, dir, file string) (string, bool) {
	resolved := filepath.Join(dir, file)
	rel, err := filepath.Rel(absRoot, resolved)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return resolved, true
}

// durationFromSeconds converts a sidecar's float seconds to a Duration, rounding
// to the nearest nanosecond so 612.437 does not land a nanosecond short.
func durationFromSeconds(seconds float64) time.Duration {
	return time.Duration(math.Round(seconds * float64(time.Second)))
}
