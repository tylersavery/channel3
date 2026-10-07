package main

import (
	"crypto/subtle"
	"fmt"
	"image"
	_ "image/jpeg" // posters
	_ "image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tylersavery/channel3/internal/bumper"
	"github.com/tylersavery/channel3/internal/input"
	"github.com/tylersavery/channel3/internal/movie"
	"github.com/tylersavery/channel3/internal/player"
)

// Movie Mode is the one place Channel Three has controls. Channel 0 asks for
// the code, then shows a menu of the prepared movies; a movie plays with
// pause, seek, subtitles and audio on the number keys. Channel up and down
// do nothing while a movie plays. Leaving tunes back to the channel that was
// on, which has carried on by the clock all along.
//
// Where each movie was stopped is kept in one small file so it can offer to
// resume, which is the one piece of playback state the owner chose to keep.

// movieOverlay is the overlay id Movie Mode's screens are drawn under, above
// the bumper and the guide.
const movieOverlay = 3

const (
	// pinTimeout is how long the code screen waits for the next digit before
	// it gives up and goes back to the channel.
	pinTimeout = 10 * time.Second
	// menuIdle is how long the menu, or the resume question, waits for a
	// press before Movie Mode closes and the channel comes back.
	menuIdle = 10 * time.Minute
	// theEndShown is how long The End stays up before the menu.
	theEndShown = 5 * time.Second
	// controlShown is how long the time and bar stay up after a press, and a
	// track's name after a change.
	controlShown = 3 * time.Second
	// pausedShown keeps the time up for as long as the movie is paused.
	pausedShown = 24 * time.Hour
	// progressCells is how wide the progress bar is.
	progressCells = 24
)

// seeks are the number keys that move through a movie, and by how much.
var seeks = map[int]time.Duration{
	4: -10 * time.Second, 6: 10 * time.Second,
	1: -time.Minute, 3: time.Minute,
	7: -5 * time.Minute, 9: 5 * time.Minute,
}

// movieShelf is where Movie Mode finds its movies and keeps its positions.
type movieShelf struct {
	dir       string
	positions string
	pin       string
}

func newMovieShelf(root, pin string) *movieShelf {
	return &movieShelf{
		dir:       moviesDir(root),
		positions: filepath.Join(root, "movie-positions.json"),
		pin:       pin,
	}
}

// movieScreen is where in Movie Mode the viewer is.
type movieScreen int

const (
	screenPIN movieScreen = iota
	screenMenu
	screenResume
	screenPlaying
	screenEnded
)

func (m movieScreen) String() string {
	return [...]string{"code", "menu", "resume", "playing", "the end"}[m]
}

// cinema is Movie Mode's state while it is open. The station holds one only
// while Movie Mode is on screen.
type cinema struct {
	screen movieScreen
	// typed is the code so far on the code screen, or a half typed movie
	// number on the menu. digitsAt is when that number is acted on.
	typed    string
	digitsAt time.Time
	// deadline is when the current screen gives up: the code screen's wait,
	// the menu's idle time, The End's moment.
	deadline time.Time

	movies  []movie.Movie
	posters map[string]image.Image
	page    int

	// chosen is the movie picked, an index into movies; resumeAt is where it
	// was stopped.
	chosen   int
	resumeAt time.Duration
	// path, audio, subtitle and paused are the movie playing. audio and
	// subtitle are mpv's track numbers; a subtitle of 0 is off.
	path     string
	audio    int
	subtitle int
	paused   bool
	// lastPos is where the movie was at the last look, which is where it
	// picks up if mpv restarts underneath it.
	lastPos time.Duration
}

// watchingMovies says Movie Mode has taken the screen from the broadcast. On
// the code screen it has not yet: the channel still plays underneath.
func (s *station) watchingMovies() bool {
	return s.cinema != nil && s.cinema.screen != screenPIN
}

// openMovies puts the code screen up over the channel.
func (s *station) openMovies() {
	s.cinema = &cinema{screen: screenPIN, deadline: s.now().Add(pinTimeout)}
	s.trackStartAt, s.trackEndAt, s.trackText = time.Time{}, time.Time{}, ""
	s.log.Info("movie mode: asking for the code")
	s.drawMovies()
}

// closeMovies takes Movie Mode down and gives the screen back to the channel.
func (s *station) closeMovies(reason string) {
	c := s.cinema
	if c == nil {
		return
	}
	s.cinema = nil
	if err := s.player.RemoveOverlay(movieOverlay); err != nil {
		s.log.Warn("could not take the movie screen down", "error", err)
	}
	s.log.Info("movie mode closed", "reason", reason, "channel", s.tuned)
	if c.screen == screenPIN {
		// The channel never stopped.
		return
	}
	// Back to the channel exactly as tuning to it would: its number, what
	// it is airing now, and its card.
	if ch, ok := s.channel(s.tuned); ok {
		s.showNumber(strconv.Itoa(ch.Number))
	}
	s.play()
	s.showBumper(s.tuned)
}

// movieKey acts on a press while Movie Mode is open. Power and volume never
// reach here.
func (s *station) movieKey(k input.Key) {
	c := s.cinema
	switch c.screen {
	case screenPIN:
		s.pinKey(k)
	case screenMenu:
		s.menuKey(k)
	case screenResume:
		s.resumeKey(k)
	case screenPlaying:
		s.playingKey(k)
	case screenEnded:
		s.showMenu()
	}
}

// pinKey takes one digit of the code.
func (s *station) pinKey(k input.Key) {
	c := s.cinema
	if k.Action != input.Digit {
		s.closeMovies("the code was not entered")
		return
	}
	c.typed += strconv.Itoa(k.Digit)
	c.deadline = s.now().Add(pinTimeout)
	if len(c.typed) < len(s.shelf.pin) {
		s.drawMovies()
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.typed), []byte(s.shelf.pin)) != 1 {
		s.log.Warn("movie mode: wrong code")
		s.closeMovies("wrong code")
		s.showText("WRONG CODE", 2*time.Second, volumeScale)
		return
	}
	s.log.Info("movie mode: code accepted")
	s.enterMenu()
}

// enterMenu takes the screen from the broadcast and shows the menu.
func (s *station) enterMenu() {
	c := s.cinema
	movies, skipped, err := movie.Shelf(s.shelf.dir)
	if err != nil {
		s.log.Error("could not read the movies", "dir", s.shelf.dir, "error", err)
	}
	for _, e := range skipped {
		s.log.Warn("leaving a movie off the menu", "error", e)
	}
	c.movies = movies
	c.posters = make(map[string]image.Image)

	// The channel's own pieces come down, and the stand by card goes under
	// the menu so nothing is heard behind it.
	s.hideBumper()
	if s.guideUp {
		s.guideUp, s.guideFlipAt = false, time.Time{}
		if err := s.player.RemoveOverlay(guideOverlay); err != nil {
			s.log.Warn("could not take the guide down", "error", err)
		}
	}
	s.standbyOrLog()
	s.log.Info("movie mode: menu", "movies", len(movies))
	s.showMenu()
}

// showMenu puts the menu up, from wherever Movie Mode was.
func (s *station) showMenu() {
	c := s.cinema
	c.screen = screenMenu
	c.typed, c.digitsAt = "", time.Time{}
	c.deadline = s.now().Add(menuIdle)
	s.drawMovies()
}

// menuKey picks a movie by number or turns the page.
func (s *station) menuKey(k input.Key) {
	c := s.cinema
	c.deadline = s.now().Add(menuIdle)
	switch k.Action {
	case input.ChannelUp, input.ChannelDown:
		pages := menuPages(len(c.movies))
		step := 1
		if k.Action == input.ChannelDown {
			step = pages - 1
		}
		c.page = (c.page + step) % pages
		c.typed, c.digitsAt = "", time.Time{}
		s.drawMovies()
	case input.Digit:
		if c.typed == "" && k.Digit == 0 {
			s.closeMovies("back to TV")
			return
		}
		c.typed += strconv.Itoa(k.Digit)
		if longerMovieNumber(c.typed, len(c.movies)) {
			c.digitsAt = s.now().Add(s.movieDigitWait())
			s.drawMovies()
			return
		}
		s.commitMovieNumber()
	}
}

// movieDigitWait is how long a half typed movie number waits, the same as a
// channel number.
func (s *station) movieDigitWait() time.Duration {
	if s.digitTimeout > 0 {
		return s.digitTimeout
	}
	return input.DigitTimeout
}

// longerMovieNumber reports whether some movie number up to count starts with
// typed and is longer, so another digit is worth waiting for.
func longerMovieNumber(typed string, count int) bool {
	for n := 1; n <= count; n++ {
		number := strconv.Itoa(n)
		if len(number) > len(typed) && strings.HasPrefix(number, typed) {
			return true
		}
	}
	return false
}

// commitMovieNumber picks the movie typed, if there is one with that number.
func (s *station) commitMovieNumber() {
	c := s.cinema
	n, _ := strconv.Atoi(c.typed)
	c.typed, c.digitsAt = "", time.Time{}
	if n < 1 || n > len(c.movies) {
		s.log.Info("movie mode: no movie has that number", "number", n)
		s.drawMovies()
		return
	}
	s.chooseMovie(n - 1)
}

// chooseMovie plays a movie, or asks first when it was stopped partway.
func (s *station) chooseMovie(i int) {
	c := s.cinema
	m := c.movies[i]
	c.chosen = i
	positions, err := movie.ReadPositions(s.shelf.positions)
	if err != nil {
		s.log.Warn("could not read where movies were stopped; starting from the beginning", "error", err)
	}
	if at := positions.ResumeAt(m); at > 0 {
		c.screen, c.resumeAt = screenResume, at
		c.deadline = s.now().Add(menuIdle)
		s.drawMovies()
		return
	}
	s.startMovie(0)
}

// resumeKey answers "Resume or Start over?".
func (s *station) resumeKey(k input.Key) {
	if k.Action != input.Digit {
		return
	}
	switch k.Digit {
	case 1:
		s.startMovie(s.cinema.resumeAt)
	case 2:
		s.startMovie(0)
	case 0:
		s.showMenu()
	}
}

// startMovie plays the chosen movie from offset.
func (s *station) startMovie(offset time.Duration) {
	c := s.cinema
	m := c.movies[c.chosen]
	c.path = filepath.Join(s.shelf.dir, m.File)
	c.audio = 0
	if len(m.Audio) > 0 {
		c.audio = 1
	}
	c.subtitle = defaultSubtitle(m)
	c.paused = false
	c.lastPos = offset
	c.deadline = time.Time{}

	if err := s.player.SetGain(m.Gain()); err != nil {
		s.log.Warn("could not set the movie's loudness correction", "movie", m.Name(), "error", err)
	}
	if err := s.loadMovie(m, offset); err != nil {
		s.log.Error("could not start a movie", "movie", m.Name(), "error", err)
		s.showMenu()
		return
	}
	c.screen = screenPlaying
	if err := s.player.RemoveOverlay(movieOverlay); err != nil {
		s.log.Warn("could not take the movie screen down", "error", err)
	}
	s.log.Info("movie mode: playing", "movie", m.Name(), "from", offset.Round(time.Second),
		"audio", c.audio, "subtitles", c.subtitle)
	s.showProgress(offset, controlShown)
}

// loadMovie hands the playing movie to mpv at offset with its tracks.
func (s *station) loadMovie(m movie.Movie, offset time.Duration) error {
	c := s.cinema
	return s.player.LoadMovie(c.path, offset, player.MovieOptions{
		Audio:       c.audio,
		Subtitle:    subtitleTrack(m, c.subtitle),
		Deinterlace: m.Interlaced,
	})
}

// defaultSubtitle is the subtitle track a movie starts with: the full English
// track that prepare put first, or off.
func defaultSubtitle(m movie.Movie) int {
	if len(m.Subtitles) > 0 && m.Subtitles[0].English() && !m.Subtitles[0].Forced {
		return 1
	}
	return 0
}

// subtitleTrack is the track mpv shows for a choice: the choice itself, or
// with subtitles off the forced English track, which only covers the scenes
// in another language, when there is one.
func subtitleTrack(m movie.Movie, choice int) int {
	if choice > 0 {
		return choice
	}
	for i, t := range m.Subtitles {
		if t.Forced && t.English() {
			return i + 1
		}
	}
	return 0
}

// nextSubtitle is the choice after current: each full track in turn, then
// off. Forced tracks are not in the cycle; off already shows them.
func nextSubtitle(m movie.Movie, current int) int {
	var full []int
	for i, t := range m.Subtitles {
		if !t.Forced {
			full = append(full, i+1)
		}
	}
	at := slices.Index(full, current)
	if at+1 < len(full) {
		return full[at+1]
	}
	return 0
}

// playingKey is the remote while a movie plays.
func (s *station) playingKey(k input.Key) {
	c := s.cinema
	if k.Action != input.Digit {
		// Channel up and down do nothing during a movie.
		return
	}
	m := c.movies[c.chosen]
	switch d := k.Digit; {
	case d == 5:
		c.paused = !c.paused
		if err := s.player.SetPause(c.paused); err != nil {
			s.log.Warn("could not pause", "error", err)
		}
		s.showProgress(s.moviePosition(), s.progressFor())
	case seeks[d] != 0:
		target := s.moviePosition() + seeks[d]
		if err := s.player.Seek(seeks[d]); err != nil {
			s.log.Warn("could not seek", "by", seeks[d], "error", err)
			return
		}
		length := time.Duration(m.Duration * float64(time.Second))
		target = min(max(target, 0), length)
		c.lastPos = target
		s.showProgress(target, s.progressFor())
	case d == 8:
		c.subtitle = nextSubtitle(m, c.subtitle)
		if err := s.player.SetTrack(player.SubtitleTrack, subtitleTrack(m, c.subtitle)); err != nil {
			s.log.Warn("could not change the subtitles", "error", err)
		}
		label := "Subtitles off"
		if c.subtitle > 0 {
			label = "Subtitles: " + m.Subtitles[c.subtitle-1].Label()
		}
		s.showText(label, controlShown, volumeScale)
	case d == 2:
		if len(m.Audio) < 2 {
			s.showText("Audio: "+trackLabel(m.Audio, c.audio), controlShown, volumeScale)
			return
		}
		c.audio = c.audio%len(m.Audio) + 1
		if err := s.player.SetTrack(player.AudioTrack, c.audio); err != nil {
			s.log.Warn("could not change the audio", "error", err)
		}
		s.showText("Audio: "+trackLabel(m.Audio, c.audio), controlShown, volumeScale)
	case d == 0:
		s.saveMoviePosition()
		s.log.Info("movie mode: stopped", "movie", m.Name(), "at", c.lastPos.Round(time.Second))
		s.showText("", time.Millisecond, volumeScale)
		s.standbyOrLog()
		s.showMenu()
	}
}

// trackLabel names track n, numbered from 1, of tracks.
func trackLabel(tracks []movie.Track, n int) string {
	if n < 1 || n > len(tracks) {
		return "none"
	}
	return tracks[n-1].Label()
}

// progressFor is how long the time stays up: for as long as the movie is
// paused, or a moment while it plays.
func (s *station) progressFor() time.Duration {
	if s.cinema.paused {
		return pausedShown
	}
	return controlShown
}

// moviePosition is how far into the movie mpv is, or the last known point
// when mpv cannot say.
func (s *station) moviePosition() time.Duration {
	c := s.cinema
	path, pos, err := s.player.Position()
	if err != nil || path != c.path {
		return c.lastPos
	}
	c.lastPos = pos
	return pos
}

// showProgress puts the time and a bar up, in the channel number's green.
func (s *station) showProgress(at, d time.Duration) {
	c := s.cinema
	m := c.movies[c.chosen]
	length := time.Duration(m.Duration * float64(time.Second))
	state := "▶"
	if c.paused {
		state = "II"
	}
	s.showText(fmt.Sprintf("%s  %s / %s\n%s", state, clockTime(at), clockTime(length), progressBar(at, length)),
		d, volumeScale)
}

// clockTime writes a duration as 1:02:03.
func clockTime(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	h, m, sec := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
}

// progressBar draws how far through at is, in cells like the volume bar.
func progressBar(at, length time.Duration) string {
	filled := 0
	if length > 0 {
		filled = int(int64(at) * progressCells / int64(length))
	}
	filled = min(max(filled, 0), progressCells)
	return strings.Repeat("█", filled) + strings.Repeat("░", progressCells-filled)
}

// saveMoviePosition writes down where the playing movie is. It runs on every
// reconcile tick while a movie plays, so a power cut loses half a minute at
// most, and whenever a movie is stopped.
func (s *station) saveMoviePosition() {
	c := s.cinema
	if c == nil || c.screen != screenPlaying {
		return
	}
	at := s.moviePosition()
	s.writePosition(c.movies[c.chosen].File, at)
}

// writePosition records where a movie is, or forgets it when at is zero.
func (s *station) writePosition(file string, at time.Duration) {
	positions, err := movie.ReadPositions(s.shelf.positions)
	if err != nil {
		s.log.Warn("could not read where movies were stopped; starting the file afresh", "error", err)
		positions = movie.Positions{}
	}
	if at > 0 {
		positions[file] = at
	} else {
		delete(positions, file)
	}
	if err := positions.Write(s.shelf.positions); err != nil {
		s.log.Warn("could not write down where the movie is", "file", file, "error", err)
	}
}

// movieEvent acts on a player event while Movie Mode has the screen.
func (s *station) movieEvent(ev player.Event) {
	c := s.cinema
	switch ev.Kind {
	case player.Restarted:
		s.log.Warn("mpv restarted during movie mode")
		s.applyVolume()
		if c.screen == screenPlaying {
			m := c.movies[c.chosen]
			if err := s.player.SetGain(m.Gain()); err != nil {
				s.log.Warn("could not set the movie's loudness correction", "error", err)
			}
			c.paused = false
			if err := s.loadMovie(m, c.lastPos); err != nil {
				s.log.Error("could not reload the movie after mpv restarted", "error", err)
			}
			return
		}
		// A fresh mpv has no overlays, so the screen is drawn again.
		s.standbyOrLog()
		s.drawMovies()
	case player.EndFile:
		if c.screen != screenPlaying || (ev.Path != "" && ev.Path != c.path) {
			return
		}
		m := c.movies[c.chosen]
		if ev.Reason == player.ReasonError {
			s.log.Error("a movie would not play", "movie", m.Name(), "path", c.path)
			s.standbyOrLog()
			s.showMenu()
			return
		}
		s.log.Info("movie mode: the end", "movie", m.Name())
		s.writePosition(m.File, 0)
		s.standbyOrLog()
		c.screen = screenEnded
		c.deadline = s.now().Add(theEndShown)
		s.drawMovies()
	}
}

// movieTimeout acts on whatever Movie Mode was waiting for.
func (s *station) movieTimeout() {
	c := s.cinema
	if c == nil {
		return
	}
	now := s.now()
	if !c.digitsAt.IsZero() && !now.Before(c.digitsAt) {
		s.commitMovieNumber()
		return
	}
	if c.deadline.IsZero() || now.Before(c.deadline) {
		return
	}
	switch c.screen {
	case screenPIN:
		s.closeMovies("no code entered")
	case screenMenu, screenResume:
		s.closeMovies("nobody chose a movie")
	case screenEnded:
		s.showMenu()
	}
}

// armMovies sets the timer for Movie Mode's next moment, and stops it when
// there is none.
func (s *station) armMovies(timer *time.Timer) {
	c := s.cinema
	if c == nil {
		timer.Stop()
		return
	}
	next := c.deadline
	if !c.digitsAt.IsZero() && (next.IsZero() || c.digitsAt.Before(next)) {
		next = c.digitsAt
	}
	if next.IsZero() {
		timer.Stop()
		return
	}
	timer.Reset(max(next.Sub(s.now()), 0))
}

// drawMovies draws the current Movie Mode screen and puts it up. A playing
// movie has no screen.
func (s *station) drawMovies() {
	c := s.cinema
	if c.screen == screenPlaying {
		return
	}
	w, h, err := s.player.ScreenSize()
	if err != nil {
		s.log.Warn("no screen size for movie mode", "error", err)
		return
	}
	var img *image.RGBA
	switch c.screen {
	case screenPIN:
		img, err = bumper.RenderPIN(len(c.typed), len(s.shelf.pin), w, h)
	case screenMenu:
		img, err = bumper.RenderMenu(s.menuPage(), w, h)
	case screenResume:
		m := c.movies[c.chosen]
		img, err = bumper.RenderNotice("MOVIE NIGHT", m.Name(),
			[]string{"1   Resume from " + clockTime(c.resumeAt), "2   Start over"}, "0 for the menu", w, h)
	case screenEnded:
		m := c.movies[c.chosen]
		img, err = bumper.RenderNotice("MOVIE NIGHT", "The End", []string{m.Name()}, "", w, h)
	}
	if err != nil {
		s.log.Warn("could not draw the movie screen", "screen", c.screen, "error", err)
		return
	}
	if err := s.player.ShowOverlay(movieOverlay, img, 0, 0); err != nil {
		s.log.Warn("could not show the movie screen", "screen", c.screen, "error", err)
	}
}

// menuPages is how many pages count movies fill.
func menuPages(count int) int {
	return max((count+bumper.MoviesPerPage-1)/bumper.MoviesPerPage, 1)
}

// menuPage is the menu's current page, with its posters.
func (s *station) menuPage() bumper.MenuPage {
	c := s.cinema
	pages := menuPages(len(c.movies))
	c.page %= pages
	page := bumper.MenuPage{Page: c.page + 1, Pages: pages, Typed: c.typed}
	first := c.page * bumper.MoviesPerPage
	for i := first; i < min(first+bumper.MoviesPerPage, len(c.movies)); i++ {
		m := c.movies[i]
		page.Movies = append(page.Movies, bumper.MenuMovie{Number: i + 1, Title: m.Name(), Poster: s.poster(m)})
	}
	return page
}

// poster reads a movie's poster once per visit to the menu. A poster that
// will not read leaves a plain tile with the title.
func (s *station) poster(m movie.Movie) image.Image {
	c := s.cinema
	if m.Poster == "" {
		return nil
	}
	if img, ok := c.posters[m.Poster]; ok {
		return img
	}
	var img image.Image
	f, err := os.Open(filepath.Join(s.shelf.dir, m.Poster))
	if err == nil {
		img, _, err = image.Decode(f)
		f.Close()
	}
	if err != nil {
		s.log.Warn("could not read a movie's poster", "movie", m.Name(), "error", err)
		img = nil
	}
	c.posters[m.Poster] = img
	return img
}
