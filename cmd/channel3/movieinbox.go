package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tylersavery/channel3/internal/movie"
)

// Where dropped rips arrive, where prepared movies live, and where a rip that
// would not prepare is set aside so it is not tried on every start.
func movieInboxDir(root string) string  { return filepath.Join(root, "movies-inbox") }
func moviesDir(root string) string      { return filepath.Join(root, "local", "movies") }
func movieFailedDir(root string) string { return filepath.Join(root, "movies-failed") }

// movieInbox prepares the rips deploy/ingest.sh moves onto the Pi.
//
// It runs one pass when serve starts, which is when new rips arrive: ingest.sh
// moves them in and then restarts the service. Each rip is prepared at low
// priority beside the broadcast; once its movie is checked and in place, the
// rip and the files dropped with it are deleted, since the drop zone on the
// Mac is a copy of the owner's own collection. A pass cut short by a restart
// starts that rip again next time; ffmpeg's half-written file is discarded.
type movieInbox struct {
	root  string
	tools movie.Tools
	log   *slog.Logger
	now   func() time.Time
}

func newMovieInbox(root string, tools movie.Tools, log *slog.Logger) *movieInbox {
	return &movieInbox{root: root, tools: tools, log: log, now: time.Now}
}

// Run prepares every rip waiting in the inbox, then returns.
func (m *movieInbox) Run(ctx context.Context) {
	m.removePartials()
	rips := m.waiting()
	if len(rips) > 0 {
		m.log.Info("movies to prepare", "count", len(rips))
	}
	for _, src := range rips {
		if ctx.Err() != nil {
			return
		}
		m.prepare(ctx, src)
	}
	m.removeEmptyFolders()
}

// prepare makes one movie and clears its rip out of the inbox.
func (m *movieInbox) prepare(ctx context.Context, src string) {
	name := relName(movieInboxDir(m.root), src)
	start := m.now()
	m.log.Info("preparing a movie", "file", name)
	res, err := m.tools.Prepare(ctx, src, moviesDir(m.root))
	if err != nil {
		if ctx.Err() != nil {
			m.log.Info("movie preparation stopped; it starts again next time", "file", name)
			return
		}
		m.log.Error("could not prepare a movie, setting it aside", "file", name, "error", err)
		m.setAside(src)
		return
	}
	for _, w := range res.Warnings {
		m.log.Warn("movie prepared with a problem", "file", name, "problem", w)
	}
	how := "copied"
	if res.Movie.Encoded != "" {
		how = "re-encoded: " + res.Movie.Encoded
	}
	m.log.Info("movie prepared", "file", name, "as", res.Movie.Name(), "how", how,
		"audio", len(res.Movie.Audio), "subtitles", len(res.Movie.Subtitles),
		"took", m.now().Sub(start).Round(time.Second))
	for _, f := range append(movie.Companions(src), src) {
		if err := os.Remove(f); err != nil {
			m.log.Warn("could not delete a prepared rip", "file", f, "error", err)
		}
	}
}

// waiting lists the rips in the inbox and its folders, in name order. Hidden
// files are skipped, which is also what an rsync still in flight looks like.
func (m *movieInbox) waiting() []string {
	var rips []string
	err := filepath.WalkDir(movieInboxDir(m.root), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && path != movieInboxDir(m.root) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && movie.IsMovie(path) {
			rips = append(rips, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.log.Error("could not read the movie inbox", "error", err)
	}
	slices.Sort(rips)
	return rips
}

// removePartials deletes movies ffmpeg was writing when the service stopped.
func (m *movieInbox) removePartials() {
	partials, _ := filepath.Glob(filepath.Join(moviesDir(m.root), "*.part"))
	for _, p := range partials {
		if err := os.Remove(p); err != nil {
			m.log.Warn("could not remove an unfinished movie", "file", p, "error", err)
		}
	}
}

// setAside moves a rip that would not prepare, with its companions, out of
// the inbox.
func (m *movieInbox) setAside(src string) {
	if err := os.MkdirAll(movieFailedDir(m.root), 0o755); err != nil {
		m.log.Error("could not set aside a rip that will not prepare; it will be tried again", "file", src, "error", err)
		return
	}
	for _, f := range append(movie.Companions(src), src) {
		if err := os.Rename(f, filepath.Join(movieFailedDir(m.root), filepath.Base(f))); err != nil {
			m.log.Error("could not set aside a rip that will not prepare; it will be tried again", "file", f, "error", err)
		}
	}
}

// removeEmptyFolders tidies the folders a rip came in once it has gone.
// Removing a folder that is not empty fails, which is the point.
func (m *movieInbox) removeEmptyFolders() {
	var dirs []string
	_ = filepath.WalkDir(movieInboxDir(m.root), func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != movieInboxDir(m.root) {
			dirs = append(dirs, path)
		}
		return nil
	})
	slices.Reverse(dirs)
	for _, d := range dirs {
		_ = os.Remove(d)
	}
}

// relName is path relative to dir, for the log.
func relName(dir, path string) string {
	if rel, err := filepath.Rel(dir, path); err == nil {
		return rel
	}
	return filepath.Base(path)
}
