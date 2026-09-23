package api

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// indexFile is the page every path that is not a file resolves to.
const indexFile = "index.html"

// assetsDir is where Vite writes the files it stamps with a content hash.
// Nothing outside it is ever cached for long, whatever its name looks like.
const assetsDir = "assets"

// hashedAsset matches a file name a build tool has stamped with a content hash,
// such as index-DkJ2f8Qa.js.
//
// The hash is bounded at eight to twelve characters and the extension has to be
// one a build actually emits, because plenty of hand written names have a dash
// in them: logo-horizontal.svg is not a content hash and must not be cached for
// a year. A name that slips past this is served with no-cache, which is slower
// and never wrong.
var hashedAsset = regexp.MustCompile(`-[0-9A-Za-z_-]{8,12}\.(?:js|css|woff2?|ttf|png|jpe?g|svg|webp|gif|ico)$`)

// Cache lifetimes for the web interface.
const (
	// immutableCache is a year, the longest max-age worth sending.
	immutableCache = "public, max-age=31536000, immutable"
	// noCache lets a browser keep a copy and makes it ask before using it,
	// which is what everything whose name does not change needs.
	noCache = "no-cache"
)

// fallbackPage is what the server serves when no web interface has been built.
//
// It is a 200 rather than a 404 on purpose. The API behind it is up and the
// page says so, which is the difference between "the guide is broken" and "the
// guide has not been built yet" for whoever opens it on their phone.
const fallbackPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Channel Three</title>
</head>
<body>
<h1>Channel Three</h1>
<p>The web interface has not been built. The API is running.</p>
<ul>
<li><a href="/api/channels">/api/channels</a></li>
<li><a href="/api/now">/api/now</a></li>
<li><a href="/api/guide?hours=6">/api/guide?hours=6</a></li>
</ul>
<p>Build the page with <code>npm --prefix web run build</code>, or point <code>channel3 serve --ui-dir</code> at a build on disk.</p>
</body>
</html>
`

// ui returns the handler for the web interface.
func (s *server) ui() http.Handler {
	return http.HandlerFunc(s.serveUI)
}

// serveUI serves one file out of the embedded or on-disk web build.
//
// Directories are never listed and nothing outside the FS can be reached: the
// path is cleaned to a relative name, rejected if it is not a valid FS path,
// and an fs.FS cannot be walked upwards in any case.
func (s *server) serveUI(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = indexFile
	}
	if s.deps.UI == nil || !fs.ValidPath(name) {
		s.missing(w, r, name)
		return
	}

	file, err := s.deps.UI.Open(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Error("could not open a web interface file", "file", name, "error", err)
		}
		s.missing(w, r, name)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Debug("could not close a web interface file", "file", name, "error", err)
		}
	}()

	info, err := file.Stat()
	if err != nil {
		slog.Error("could not stat a web interface file", "file", name, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the web interface")
		return
	}
	if info.IsDir() {
		// A directory is not a page and its listing is nobody's business.
		s.missing(w, r, name)
		return
	}

	content, err := readSeeker(file)
	if err != nil {
		slog.Error("could not read a web interface file", "file", name, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the web interface")
		return
	}

	w.Header().Set("Cache-Control", cacheControl(name))
	// An embedded file has no useful modification time, so ServeContent is
	// given a zero one and sends no Last-Modified. It still sets the content
	// type from the extension and still answers a range request.
	http.ServeContent(w, r, name, time.Time{}, content)
}

// missing answers a path the build does not have.
//
// A request for the page itself gets the fallback, which is the whole point of
// the fallback: a browser pointed at the Pi sees something. A request for an
// asset gets a 404, because a JavaScript file that is answered with HTML is a
// far more confusing failure than one that is answered with nothing.
func (s *server) missing(w http.ResponseWriter, r *http.Request, name string) {
	if name != indexFile {
		writeError(w, http.StatusNotFound, "no such file in the web interface")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", noCache)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.WriteString(w, fallbackPage); err != nil {
		slog.Debug("could not write the fallback page", "error", err)
	}
}

// cacheControl decides how long a file may be held.
//
// Only a hashed file under assets/ is cached for a year, because only those
// names change when the bytes do. Everything else, index.html included, is
// revalidated on every load: a stale favicon or web manifest that a browser
// refuses to let go of for a year is not worth the request it saves.
func cacheControl(name string) string {
	dir := path.Dir(name)
	if dir != assetsDir && !strings.HasPrefix(dir, assetsDir+"/") {
		return noCache
	}
	if hashedAsset.MatchString(path.Base(name)) {
		return immutableCache
	}
	return noCache
}

// readSeeker gives ServeContent something it can seek.
//
// Both FS implementations this server is given, embed.FS and os.DirFS, return
// files that already seek. Anything else is read into memory, which is a build
// artefact of a few hundred kilobytes at worst.
func readSeeker(file fs.File) (io.ReadSeeker, error) {
	if seeker, ok := file.(io.ReadSeeker); ok {
		return seeker, nil
	}
	buf, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(buf), nil
}
