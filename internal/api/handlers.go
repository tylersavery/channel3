package api

import (
	"cmp"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/tylersavery/channel3/internal/schedule"
)

// Guide horizons. hours is clamped to a range rather than trusted, because the
// query string is the one part of this service a stranger on the network can
// choose, and a horizon of a year would walk the schedule a million times.
const (
	defaultHours = 6
	minHours     = 1
	maxHours     = 48
)

// channelJSON identifies a channel in every response.
type channelJSON struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Name   string `json:"name"`
}

// channelSummary is one entry of GET /api/channels.
type channelSummary struct {
	channelJSON
	// Items is how many videos the channel draws on, not how many air today.
	Items int `json:"items"`
}

// channelsResponse is GET /api/channels.
type channelsResponse struct {
	Channels []channelSummary `json:"channels"`
}

// airingJSON is one slot of GET /api/now: what is on, and how far into it we
// are.
//
// Duration is always the item's own length. End is where this airing stops,
// which is earlier than Start plus Duration for an item cut short by the 04:00
// rollover, so a client that wants a progress bar must use Duration and Offset
// and never End minus Start.
type airingJSON struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Start    string  `json:"start"`
	End      string  `json:"end"`
	Duration float64 `json:"duration"`
	Offset   float64 `json:"offset"`
}

// slotJSON is one slot of GET /api/guide. It is an airing without the offset,
// which only means anything for what is on right now.
type slotJSON struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Start    string  `json:"start"`
	End      string  `json:"end"`
	Duration float64 `json:"duration"`
}

// nowChannel is one channel of GET /api/now.
type nowChannel struct {
	channelJSON
	Now  *airingJSON `json:"now"`
	Next *airingJSON `json:"next"`
}

// nowResponse is GET /api/now.
type nowResponse struct {
	Time string `json:"time"`
	// Tuned is null when nothing is on air, which is what the API reports
	// before the station has tuned and after the tuned channel has gone.
	Tuned    *string      `json:"tuned"`
	Channels []nowChannel `json:"channels"`
}

// guideChannel is one channel of GET /api/guide.
type guideChannel struct {
	channelJSON
	// Slots is never null. A channel with nothing playable is an empty list,
	// so a client can iterate it without checking.
	Slots []slotJSON `json:"slots"`
}

// guideResponse is GET /api/guide.
type guideResponse struct {
	From     string         `json:"from"`
	To       string         `json:"to"`
	Channels []guideChannel `json:"channels"`
}

// errorResponse is every 4xx and 5xx body.
type errorResponse struct {
	Error string `json:"error"`
}

// handleChannels lists the station.
func (s *server) handleChannels(w http.ResponseWriter, r *http.Request) {
	channels := s.sorted()
	out := channelsResponse{Channels: make([]channelSummary, 0, len(channels))}
	for _, ch := range channels {
		out.Channels = append(out.Channels, channelSummary{
			channelJSON: identify(ch),
			Items:       len(ch.Items),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNow reports what every channel is airing and what follows it.
func (s *server) handleNow(w http.ResponseWriter, r *http.Request) {
	now := s.deps.Now()
	channels := s.sorted()

	out := nowResponse{
		Time:     s.stamp(now),
		Channels: make([]nowChannel, 0, len(channels)),
	}
	if tuned := s.deps.Tuned(); tuned != "" {
		out.Tuned = &tuned
	}
	for _, ch := range channels {
		current, next := s.airings(ch, now)
		out.Channels = append(out.Channels, nowChannel{
			channelJSON: identify(ch),
			Now:         current,
			Next:        next,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGuide reports the next few hours of every channel.
func (s *server) handleGuide(w http.ResponseWriter, r *http.Request) {
	hours, err := parseHours(r.URL.Query().Get("hours"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	from := s.deps.Now()
	horizon := time.Duration(hours) * time.Hour
	channels := s.sorted()

	out := guideResponse{
		From:     s.stamp(from),
		To:       s.stamp(from.Add(horizon)),
		Channels: make([]guideChannel, 0, len(channels)),
	}
	for _, ch := range channels {
		slots := schedule.Guide(ch, from, horizon, s.deps.Clock)
		entry := guideChannel{
			channelJSON: identify(ch),
			Slots:       make([]slotJSON, 0, len(slots)),
		}
		for _, slot := range slots {
			entry.Slots = append(entry.Slots, slotJSON{
				ID:       slot.Item.ID,
				Title:    slot.Item.Title,
				Start:    s.stamp(slot.Start),
				End:      s.stamp(slot.End),
				Duration: seconds(slot.Item.Duration),
			})
		}
		out.Channels = append(out.Channels, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUnknownAPI answers an endpoint that does not exist in JSON, so a client
// parsing the body finds the error it expects rather than a page of HTML.
func handleUnknownAPI(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, fmt.Sprintf("no such endpoint: %s", r.URL.Path))
}

// airings returns what ch is on now and what follows it.
//
// Both come from schedule.Guide, which is schedule.At with an airing that
// crosses the 04:00 rollover cut short there. The second call starts at the end
// of the first, which is an item boundary, so the following slot always carries
// an offset of zero. A channel with nothing playable gives two nils.
func (s *server) airings(ch schedule.Channel, now time.Time) (*airingJSON, *airingJSON) {
	current := schedule.Guide(ch, now, 0, s.deps.Clock)
	if len(current) == 0 {
		return nil, nil
	}
	airing := s.airing(current[0])

	var following *airingJSON
	if next := schedule.Guide(ch, current[0].End, 0, s.deps.Clock); len(next) > 0 {
		following = s.airing(next[0])
	}
	return airing, following
}

// airing renders one slot of GET /api/now.
func (s *server) airing(slot schedule.Slot) *airingJSON {
	return &airingJSON{
		ID:       slot.Item.ID,
		Title:    slot.Item.Title,
		Start:    s.stamp(slot.Start),
		End:      s.stamp(slot.End),
		Duration: seconds(slot.Item.Duration),
		Offset:   seconds(slot.Offset),
	}
}

// sorted is the channel list by number, lowest first, as every response wants
// it.
//
// It sorts a copy. Whatever the station hands back belongs to the station, and
// a handler that reordered it in place would be writing to the broadcast loop's
// data from an HTTP goroutine.
func (s *server) sorted() []schedule.Channel {
	channels := slices.Clone(s.deps.Channels())
	slices.SortStableFunc(channels, func(a, b schedule.Channel) int {
		return cmp.Compare(a.Number, b.Number)
	})
	return channels
}

// stamp formats an instant as the API reports it: RFC 3339 in the broadcast
// clock's zone, whole seconds.
func (s *server) stamp(t time.Time) string {
	return t.In(s.location()).Format(time.RFC3339)
}

// identify is the id, number and name every response repeats.
func identify(ch schedule.Channel) channelJSON {
	return channelJSON{ID: ch.ID, Number: ch.Number, Name: ch.Name}
}

// seconds renders a duration the way the contract asks: seconds as a float.
func seconds(d time.Duration) float64 {
	return d.Seconds()
}

// parseHours reads the guide horizon from the query string.
func parseHours(raw string) (int, error) {
	if raw == "" {
		return defaultHours, nil
	}
	hours, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("hours must be a whole number of hours between %d and %d, got %q", minHours, maxHours, raw)
	}
	if hours < minHours || hours > maxHours {
		return 0, fmt.Errorf("hours must be between %d and %d, got %d", minHours, maxHours, hours)
	}
	return hours, nil
}

// writeJSON sends a response body.
//
// The body is built before anything is written, so a value that cannot be
// encoded becomes a 500 with a JSON error rather than a 200 with a truncated
// body that a client would try to parse.
func writeJSON(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		slog.Error("could not encode an API response", "error", err)
		writeError(w, http.StatusInternalServerError, "could not encode the response")
		return
	}
	encoded = append(encoded, '\n')

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// The guide is the clock. Nothing about it may be cached.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := w.Write(encoded); err != nil {
		// The client hung up mid-response. There is nothing to send an error
		// to, so it is logged and dropped.
		slog.Debug("could not write an API response", "error", err)
	}
}

// writeError sends an error body. It never recurses into writeJSON, because the
// one thing that must always encode is the error itself.
func writeError(w http.ResponseWriter, status int, message string) {
	encoded, err := json.Marshal(errorResponse{Error: message})
	if err != nil {
		// errorResponse is a struct of one string and cannot fail to encode.
		// Handling it anyway keeps the client from getting an empty 200.
		slog.Error("could not encode an API error", "error", err)
		encoded = []byte(`{"error":"could not encode the error"}`)
	}
	encoded = append(encoded, '\n')

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := w.Write(encoded); err != nil {
		slog.Debug("could not write an API error", "error", err)
	}
}
