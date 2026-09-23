// Package api serves the read-only guide: what every channel is airing now,
// what is on next, and the schedule for the next few hours.
//
// Nothing here changes what plays. There is no endpoint that tunes, pauses or
// seeks, by design: the television is driven by the remote and by the clock,
// and a phone on the home network is only ever allowed to look. The handlers
// read the same pure schedule the broadcast loop reads.
//
// They do not see everything the loop sees. An item the loop has excluded
// because mpv could not open it is still in the schedule these handlers
// compute, so after a load error the guide is ahead of the screen until the
// 04:00 rollover clears the exclusions. docs/integration/guide-api.md records
// this and what fixing it would take.
//
// The package holds no state. Everything it knows arrives through Deps, which
// is what lets the tests hand it a fixed clock and a fixed channel list.
package api

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/tylersavery/channel3/internal/schedule"
)

// Deps is everything the API reads.
//
// Channels and Tuned are called on the HTTP server's goroutines while the
// broadcast loop runs, so whatever supplies them must be safe to call from
// another goroutine. cmd/channel3 satisfies both from the station, which guards
// them with a mutex.
type Deps struct {
	// Channels is the station's channel list. It may be empty, which is a
	// station that has not loaded yet, and it need not be sorted.
	Channels func() []schedule.Channel
	// Now reads the wall clock. Tests substitute a fake.
	Now func() time.Time
	// Tuned is the channel id on air. An empty string means nothing is tuned
	// and is reported as a null.
	Tuned func() string
	// Clock is the broadcast clock: the zone times are reported in and the
	// 04:00 day start the schedule is built on.
	Clock schedule.Clock
	// UI is the built web interface. A nil FS, or one with no index.html,
	// serves the fallback page instead.
	UI fs.FS
}

// server holds the dependencies with their defaults applied.
type server struct {
	deps Deps
}

// New returns the handler for the API and the web interface.
//
// Missing dependencies are filled with harmless defaults rather than rejected,
// because a guide that answers "no channels, nothing tuned" is a far better
// failure on an appliance than a service that will not start.
func New(deps Deps) http.Handler {
	if deps.Channels == nil {
		deps.Channels = func() []schedule.Channel { return nil }
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Tuned == nil {
		deps.Tuned = func() string { return "" }
	}
	s := &server{deps: deps}

	mux := http.NewServeMux()
	mux.Handle("/api/channels", readOnly(http.HandlerFunc(s.handleChannels)))
	mux.Handle("/api/now", readOnly(http.HandlerFunc(s.handleNow)))
	mux.Handle("/api/guide", readOnly(http.HandlerFunc(s.handleGuide)))
	// Anything else under /api/ is a client asking for an endpoint that does
	// not exist, and it wants that as JSON rather than as the UI's index page.
	mux.Handle("/api/", readOnly(http.HandlerFunc(handleUnknownAPI)))
	mux.Handle("/", readOnly(s.ui()))
	return mux
}

// readOnly rejects every method that could be taken as a request to change
// something. Only GET and HEAD reach a handler.
//
// HEAD is allowed because net/http answers it from the GET handler with the
// body dropped, and a client checking whether the guide is up should not have
// to send a GET to find out.
func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "the guide is read only, use GET")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// location is the zone times are reported in.
func (s *server) location() *time.Location {
	if s.deps.Clock.Location == nil {
		return time.Local
	}
	return s.deps.Clock.Location
}
