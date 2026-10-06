package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tylersavery/channel3/internal/homevideo"
)

// Where uploaded clips wait, and where one that would not convert is set aside
// so it is not tried again on every pass.
func homeInboxDir(root string) string  { return filepath.Join(root, "local", "home-inbox") }
func homeFailedDir(root string) string { return filepath.Join(root, "local", "home-failed") }

// homeInbox receives uploaded clips and prepares them in the background.
//
// Accept runs on the HTTP server's goroutines and only writes a file and wakes
// the worker. Run is the worker: it prepares each clip at low priority, moves
// the original aside, then ingests the new clips and asks the station to
// rescan, so the clip reaches the Home Movies channel without a restart.
type homeInbox struct {
	root  string
	tools homevideo.Tools
	// ingest makes sidecars for the new clips. serve passes a local-only
	// ingest of every channel.
	ingest func() error
	// rescan asks the station to reread the library.
	rescan func()
	log    *slog.Logger
	now    func() time.Time
	wake   chan struct{}
}

func newHomeInbox(root string, tools homevideo.Tools, ingest func() error, rescan func(), log *slog.Logger) *homeInbox {
	return &homeInbox{
		root: root, tools: tools, ingest: ingest, rescan: rescan, log: log,
		now:  time.Now,
		wake: make(chan struct{}, 1),
	}
}

// unsafeName matches everything a stored file name does not keep.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

// Accept stores an uploaded clip in the inbox and wakes the worker. It returns
// the stored file's name.
//
// The name a phone sends is reduced to its base name and safe characters and
// prefixed with the time it arrived, so two IMG_0001.MOV uploads never collide
// and nothing a client sends can name a path outside the inbox. The file is
// written under a temporary name and renamed only once the body has arrived
// whole, so the worker never picks up half an upload.
func (h *homeInbox) Accept(name string, body io.Reader) (string, error) {
	clean := strings.TrimSpace(unsafeName.ReplaceAllString(filepath.Base(name), "_"))
	if clean == "" || clean == "." || clean == ".." || !homevideo.IsVideo(clean) {
		return "", fmt.Errorf("%q is not a video file", name)
	}
	if err := os.MkdirAll(homeInboxDir(h.root), 0o755); err != nil {
		return "", err
	}
	stored := h.now().UTC().Format("20060102-150405.000") + " " + clean
	path := filepath.Join(homeInboxDir(h.root), stored)
	tmp := path + ".part"

	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(f, body)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("store the upload: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	h.log.Info("home video received", "file", stored)
	select {
	case h.wake <- struct{}{}:
	default:
	}
	return stored, nil
}

// Run prepares clips until ctx ends. It starts with whatever a previous run
// left in the inbox, and removes any upload that was cut off by a restart.
func (h *homeInbox) Run(ctx context.Context) {
	h.removePartials()
	h.processAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.wake:
			h.processAll(ctx)
		}
	}
}

// removePartials deletes uploads that never finished. Run calls it before
// anything can be uploading.
func (h *homeInbox) removePartials() {
	partials, _ := filepath.Glob(filepath.Join(homeInboxDir(h.root), "*.part"))
	for _, p := range partials {
		if err := os.Remove(p); err != nil {
			h.log.Warn("could not remove an unfinished upload", "file", p, "error", err)
		}
	}
}

// processAll prepares every clip waiting in the inbox, then makes them
// playable. Clips that arrive meanwhile are picked up by the next pass.
func (h *homeInbox) processAll(ctx context.Context) {
	clips := h.waiting()
	if len(clips) == 0 {
		return
	}
	prepared := 0
	for _, src := range clips {
		if ctx.Err() != nil {
			return
		}
		start := h.now()
		dst, err := h.tools.Prepare(ctx, src, homeDir(h.root), uploadedName(filepath.Base(src)))
		if err != nil {
			if ctx.Err() != nil {
				return // Shutting down; the clip is still in the inbox for next time.
			}
			h.log.Error("could not prepare a home video, setting it aside", "file", filepath.Base(src), "error", err)
			h.setAside(src)
			continue
		}
		prepared++
		h.log.Info("home video prepared", "file", filepath.Base(src), "as", filepath.Base(dst),
			"took", h.now().Sub(start).Round(time.Second))
		if err := os.MkdirAll(homeOriginalsDir(h.root), 0o755); err != nil {
			h.log.Warn("could not keep the original", "file", src, "error", err)
			continue
		}
		if err := moveOriginal(src, homeOriginalsDir(h.root)); err != nil {
			h.log.Warn("could not keep the original", "file", src, "error", err)
		}
	}
	if prepared == 0 {
		return
	}
	if err := h.ingest(); err != nil {
		h.log.Error("could not add the new home videos to the library", "error", err)
		return
	}
	h.rescan()
}

// arrivalStamp is the prefix Accept puts on a stored upload.
var arrivalStamp = regexp.MustCompile(`^\d{8}-\d{6}\.\d{3} `)

// uploadedName is the name the phone sent, without the arrival time Accept
// added, which is what a clip with no recording date is named after.
func uploadedName(stored string) string {
	return arrivalStamp.ReplaceAllString(stored, "")
}

// waiting lists the finished uploads in arrival order.
func (h *homeInbox) waiting() []string {
	entries, err := os.ReadDir(homeInboxDir(h.root))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			h.log.Error("could not read the home video inbox", "error", err)
		}
		return nil
	}
	var clips []string
	for _, e := range entries {
		if !e.IsDir() && homevideo.IsVideo(e.Name()) {
			clips = append(clips, filepath.Join(homeInboxDir(h.root), e.Name()))
		}
	}
	slices.Sort(clips)
	return clips
}

// setAside moves a clip that would not convert out of the inbox.
func (h *homeInbox) setAside(src string) {
	if err := os.MkdirAll(homeFailedDir(h.root), 0o755); err == nil {
		if err := os.Rename(src, filepath.Join(homeFailedDir(h.root), filepath.Base(src))); err == nil {
			return
		}
	}
	// It cannot be moved, so it is removed: left in place it would fail on
	// every pass for ever.
	if err := os.Remove(src); err != nil {
		h.log.Error("could not set aside or remove a clip that will not convert", "file", src, "error", err)
	}
}
