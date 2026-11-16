package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tylersavery/channel3/internal/api"
	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/shelf"
)

// grownupChannelID is the hidden channel pasted links are downloaded into.
// Its YAML has hidden: true and no sources; the videos arrive from the guide
// page, never from the channel file.
const grownupChannelID = "grownup"

// Limits on the worker's memory: how many pastes may wait, and how many are
// remembered for the page to show.
const (
	maxWaiting    = 32
	keptDownloads = 30
)

// grownupWorker downloads pasted links onto the grown-up shelf, one at a time
// and at the lowest priority, beside the broadcast. It is the one ingest that
// runs while serve does, and it only ever writes the grown-up channel's
// directory and the grown-up shelf.
type grownupWorker struct {
	root     string
	store    *shelf.Store
	covers   shelf.Covers
	runner   library.Runner
	prober   library.Prober
	measurer library.Measurer
	// channels loads the channel config, so a grown-up channel added after
	// serve started is found.
	channels func() ([]library.Channel, error)
	// rescan asks the station to read the library again; saved asks for
	// covers. Either may be nil.
	rescan func()
	saved  func()
	log    *slog.Logger

	jobs chan int
	mu   sync.Mutex
	next int
	// recent is newest first.
	recent []api.Download
}

func newGrownupWorker(root string, store *shelf.Store, covers shelf.Covers, log *slog.Logger) *grownupWorker {
	return &grownupWorker{
		root:     root,
		store:    store,
		covers:   covers,
		runner:   library.YTDLP{Nice: true},
		prober:   library.FFProbe{Path: "ffprobe"},
		measurer: library.FFmpegLoudness{Path: "ffmpeg", Nice: true},
		channels: func() ([]library.Channel, error) { return library.LoadChannels(library.ChannelsDir(root)) },
		log:      log,
		jobs:     make(chan int, maxWaiting),
	}
}

// pastedURL is a pasted link made into one video's address: trimmed, http or
// https, and for YouTube stripped of the playlist and time it was copied with,
// so pasting from a playlist downloads the one video showing.
func pastedURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("paste a link that starts with https://")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	switch {
	case host == "youtube.com" && u.Path == "/watch":
		v := u.Query().Get("v")
		if v == "" {
			return "", errors.New("that YouTube link names no video")
		}
		return "https://www.youtube.com/watch?v=" + url.QueryEscape(v), nil
	case host == "youtube.com" && u.Path == "/playlist":
		return "", errors.New("that is a playlist; paste the link of one video in it")
	case host == "youtu.be":
		return "https://youtu.be" + u.EscapedPath(), nil
	}
	return u.String(), nil
}

// Add queues a link. Pasting a link that is already waiting or downloading
// answers with that one rather than queueing it twice.
func (w *grownupWorker) Add(raw string) (api.Download, error) {
	link, err := pastedURL(raw)
	if err != nil {
		return api.Download{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range w.recent {
		if d.URL == link && (d.State == api.DownloadQueued || d.State == api.DownloadDownloading) {
			return d, nil
		}
	}
	w.next++
	d := api.Download{Key: w.next, URL: link, State: api.DownloadQueued}
	select {
	case w.jobs <- d.Key:
	default:
		return api.Download{}, fmt.Errorf("%d links are already waiting; try again once some are in", maxWaiting)
	}
	w.recent = append([]api.Download{d}, w.recent...)
	w.trim()
	return d, nil
}

// trim forgets the oldest finished downloads past keptDownloads. One still
// waiting or downloading is never forgotten. The caller holds mu.
func (w *grownupWorker) trim() {
	for i := len(w.recent) - 1; i >= 0 && len(w.recent) > keptDownloads; i-- {
		if s := w.recent[i].State; s == api.DownloadDone || s == api.DownloadFailed {
			w.recent = slices.Delete(w.recent, i, i+1)
		}
	}
}

// Downloads is what was pasted recently, newest first.
func (w *grownupWorker) Downloads() []api.Download {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Never nil, so the page is sent an empty list rather than null.
	return append([]api.Download{}, w.recent...)
}

// update changes one download in place.
func (w *grownupWorker) update(key int, change func(*api.Download)) api.Download {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.recent {
		if w.recent[i].Key == key {
			change(&w.recent[i])
			return w.recent[i]
		}
	}
	return api.Download{}
}

// Run downloads until ctx ends.
func (w *grownupWorker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-w.jobs:
			d := w.update(key, func(d *api.Download) { d.State = api.DownloadDownloading })
			title, id, err := w.download(d.URL, func(title, id string) {
				w.update(key, func(d *api.Download) { d.Title, d.ID = title, id })
			})
			w.update(key, func(d *api.Download) {
				d.Title, d.ID = title, id
				if err != nil {
					d.State, d.Error = api.DownloadFailed, err.Error()
				} else {
					d.State = api.DownloadDone
				}
			})
			if err != nil {
				w.log.Warn("grown-up download failed", "url", d.URL, "error", err)
			} else {
				w.log.Info("grown-up video downloaded", "url", d.URL, "id", id, "title", title)
			}
		}
	}
}

// download fetches one link into the grown-up channel and puts it at the top
// of the grown-up shelf. found is told the title and id as soon as they are
// known, before the download itself.
func (w *grownupWorker) download(link string, found func(title, id string)) (string, string, error) {
	ch, err := w.channel()
	if err != nil {
		return "", "", err
	}
	entries, err := w.runner.Expand(link)
	if err != nil {
		return "", "", fmt.Errorf("YouTube would not say what that link is: %s", firstLine(err.Error()))
	}
	if len(entries) != 1 {
		return "", "", fmt.Errorf("that link is %d videos; paste the link of one", len(entries))
	}
	entry := entries[0]
	if !library.ValidItemID(entry.ID) {
		return entry.Title, "", fmt.Errorf("the video's id %q cannot be a file name", entry.ID)
	}
	found(entry.Title, entry.ID)

	ch.Sources = []library.Source{{URL: link}}
	if _, err := library.Ingest(library.IngestOptions{
		Root:               w.root,
		Channels:           []library.Channel{ch},
		ChannelID:          ch.ID,
		Runner:             w.runner,
		Prober:             w.prober,
		Measurer:           w.measurer,
		AlongsideBroadcast: true,
		Out:                io.Discard,
	}); err != nil {
		return entry.Title, entry.ID, err
	}
	sidecar, err := library.ReadSidecar(filepath.Join(library.ChannelDir(w.root, ch.ID), entry.ID+".json"))
	if err != nil {
		return entry.Title, entry.ID, err
	}
	if sidecar.Status != library.StatusOK {
		return entry.Title, entry.ID, fmt.Errorf("the download failed: %s", firstLine(sidecar.Error))
	}
	title := cmp.Or(sidecar.Title, entry.Title)

	if err := w.edit(func(s shelf.Shelf) (shelf.Shelf, bool) {
		ref := shelf.Ref{Channel: ch.ID, ID: entry.ID}
		if s.Contains(ref) {
			return s, false
		}
		s.Entries = append([]shelf.Entry{{Item: &shelf.Item{Channel: ref.Channel, ID: ref.ID}}}, s.Entries...)
		return s, true
	}); err != nil {
		return title, entry.ID, fmt.Errorf("downloaded, but could not put it on the shelf: %w", err)
	}
	// The cover is made here rather than left to the cover maker, which
	// looks videos up in the station's library and would find this one only
	// after the rescan.
	w.makeCover(shelf.Ref{Channel: ch.ID, ID: entry.ID}, filepath.Join(library.ChannelDir(w.root, ch.ID), sidecar.File), sidecar.Duration)
	if w.rescan != nil {
		w.rescan()
	}
	if w.saved != nil {
		w.saved()
	}
	return title, entry.ID, nil
}

// makeCover grabs a new video's first cover. A video without one still
// plays, so a failure is only logged.
func (w *grownupWorker) makeCover(ref shelf.Ref, video string, duration float64) {
	key := shelf.ItemKey(ref)
	if w.covers.Has(key) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	at := shelf.DefaultFrame(time.Duration(duration * float64(time.Second)))
	if err := w.covers.FromFrame(ctx, key, video, at); err != nil {
		w.log.Warn("could not make a cover for a grown-up video", "key", key, "error", err)
	}
}

// channel is the grown-up channel's config, which must exist and be hidden.
func (w *grownupWorker) channel() (library.Channel, error) {
	channels, err := w.channels()
	if err != nil {
		return library.Channel{}, fmt.Errorf("the channel config does not load: %w", err)
	}
	for _, ch := range channels {
		if ch.ID != grownupChannelID {
			continue
		}
		if !ch.Hidden {
			return library.Channel{}, fmt.Errorf("the %s channel must have hidden: true, so its videos stay off the dial", grownupChannelID)
		}
		return ch, nil
	}
	return library.Channel{}, fmt.Errorf("there is no %s channel; add channels/%s.yaml with hidden: true", grownupChannelID, grownupChannelID)
}

// edit changes the grown-up shelf, reading it again when another save got in
// first. change reports whether it changed anything.
func (w *grownupWorker) edit(change func(shelf.Shelf) (shelf.Shelf, bool)) error {
	for range 3 {
		current, err := w.store.Read()
		if err != nil {
			return err
		}
		next, changed := change(current)
		if !changed {
			return nil
		}
		if _, err := w.store.Write(next); !errors.Is(err, shelf.ErrStale) {
			return err
		}
	}
	return shelf.ErrStale
}

// Delete takes a video off the grown-up shelf, wherever it is on it, and
// removes its file, sidecar and cover from the grown-up channel. Nothing
// outside that channel is ever deleted.
func (w *grownupWorker) Delete(id string) error {
	if !library.ValidItemID(id) {
		return api.ErrNoSuchVideo
	}
	ref := shelf.Ref{Channel: grownupChannelID, ID: id}
	onShelf := false
	if err := w.edit(func(s shelf.Shelf) (shelf.Shelf, bool) {
		next, removed := withoutRef(s, ref)
		onShelf = onShelf || removed
		return next, removed
	}); err != nil {
		return err
	}

	dir := library.ChannelDir(w.root, grownupChannelID)
	files, err := filepath.Glob(filepath.Join(dir, id+".*"))
	if err != nil {
		return err
	}
	if !onShelf && len(files) == 0 {
		return api.ErrNoSuchVideo
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	if cover, err := w.covers.File(shelf.ItemKey(ref)); err == nil {
		if err := os.Remove(cover); err != nil && !errors.Is(err, os.ErrNotExist) {
			w.log.Warn("could not remove a deleted video's cover", "id", id, "error", err)
		}
	}
	// A deleted video is no longer "on the shelf", so the page stops saying so.
	w.mu.Lock()
	w.recent = slices.DeleteFunc(w.recent, func(d api.Download) bool { return d.ID == id && d.State == api.DownloadDone })
	w.mu.Unlock()
	if w.rescan != nil {
		w.rescan()
	}
	return nil
}

// withoutRef is s with every place ref is on it removed, at the top or in a
// folder, and whether there was one.
func withoutRef(s shelf.Shelf, ref shelf.Ref) (shelf.Shelf, bool) {
	removed := false
	entries := make([]shelf.Entry, 0, len(s.Entries))
	for _, e := range s.Entries {
		if e.Item != nil {
			if e.Item.Ref() == ref {
				removed = true
				continue
			}
			entries = append(entries, e)
			continue
		}
		f := *e.Folder
		f.Items = slices.DeleteFunc(slices.Clone(f.Items), func(it shelf.Item) bool { return it.Ref() == ref })
		removed = removed || len(f.Items) != len(e.Folder.Items)
		entries = append(entries, shelf.Entry{Folder: &f})
	}
	s.Entries = entries
	return s, removed
}

// firstLine is s up to its first line break: a tool's error can run to
// pages, and the page has room for a sentence.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
