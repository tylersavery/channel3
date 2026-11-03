package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylersavery/channel3/internal/input"
	"github.com/tylersavery/channel3/internal/movie"
	"github.com/tylersavery/channel3/internal/player"
	"github.com/tylersavery/channel3/internal/schedule"
	"github.com/tylersavery/channel3/internal/shelf"
)

const testPIN = "6635"

// writeShelf puts sidecars and empty movie files for movies under root.
func writeShelf(t *testing.T, root string, movies ...movie.Movie) {
	t.Helper()
	dir := moviesDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, m := range movies {
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, m.Name()+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, m.File), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// upMovie has English audio and a commentary, and full English, forced
// English and French subtitles, in the order prepare writes them.
func upMovie() movie.Movie {
	return movie.Movie{
		Title: "Up", Year: 2009, File: "Up (2009).mkv", Duration: 96 * 60,
		Audio: []movie.Track{
			{Codec: "ac3", Language: "eng", Channels: 6},
			{Codec: "ac3", Language: "eng", Title: "Commentary", Comment: true},
		},
		Subtitles: []movie.Track{
			{Codec: "hdmv_pgs_subtitle", Language: "eng"},
			{Codec: "hdmv_pgs_subtitle", Language: "eng", Forced: true},
			{Codec: "hdmv_pgs_subtitle", Language: "fre"},
		},
	}
}

func dvdMovie() movie.Movie {
	return movie.Movie{
		Title: "Big", Year: 1988, File: "Big (1988).mkv", Duration: 104 * 60, Interlaced: true,
		Audio: []movie.Track{{Codec: "ac3", Language: "eng", Channels: 2}},
	}
}

// movieStation is a station with Movie Mode on, tuned to clips at noon.
func movieStation(t *testing.T, channels []schedule.Channel, movies ...movie.Movie) (*station, *fakePlayer, *fakeClock, string) {
	t.Helper()
	root := t.TempDir()
	writeShelf(t, root, movies...)
	p := newFakePlayer()
	clock := &fakeClock{now: noon()}
	s, err := newStation(stationOptions{
		Player:       p,
		Clock:        schedule.NewClock(time.UTC),
		Now:          clock.Now,
		Reload:       func() ([]schedule.Channel, error) { return channels, nil },
		StartChannel: "clips",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:     time.Hour,
		Movies:       newMovieShelf(root, testPIN),
	})
	if err != nil {
		t.Fatalf("new station: %v", err)
	}
	s.play()
	return s, p, clock, root
}

func press(s *station, digits string) {
	for _, d := range digits {
		s.key(input.Key{Action: input.Digit, Digit: int(d - '0')})
	}
}

func (p *fakePlayer) MovieLoads() []movieLoad {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]movieLoad(nil), p.movieLoads...)
}

func (p *fakePlayer) Tracks() []trackCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]trackCall(nil), p.tracks...)
}

func (p *fakePlayer) Paused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

func (p *fakePlayer) setPosition(path string, at time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.position = loadCall{Path: path, Offset: at}
}

func TestMovieModeNeedsTheCode(t *testing.T) {
	s, p, _, _ := movieStation(t, keyChannels(), upMovie())
	loads := len(p.Loads())

	press(s, "0")
	if s.cinema == nil || s.cinema.screen != screenPIN {
		t.Fatal("0 should open the code screen")
	}
	if got := overlaysWithID(p, movieOverlay); len(got) != 1 {
		t.Fatalf("code screen overlays %v", got)
	}
	press(s, "1111")
	if s.cinema != nil {
		t.Fatal("a wrong code should close Movie Mode")
	}
	if got := lastText(t, p); got.Text != "WRONG CODE" {
		t.Errorf("showed %q", got.Text)
	}
	if len(p.Loads()) != loads || p.Standbys() != 0 {
		t.Error("the channel should have played on under the code screen, untouched")
	}

	press(s, "0"+testPIN)
	if s.cinema == nil || s.cinema.screen != screenMenu {
		t.Fatal("the right code should open the menu")
	}
	if p.Standbys() != 1 {
		t.Error("the menu should silence the channel with the stand by card")
	}
}

func TestCodeScreenGivesUp(t *testing.T) {
	s, _, clock, _ := movieStation(t, keyChannels(), upMovie())
	press(s, "0"+"66")
	clock.Advance(pinTimeout)
	s.movieTimeout()
	if s.cinema != nil {
		t.Error("the code screen should give up after its wait")
	}

	press(s, "0")
	s.key(input.Key{Action: input.ChannelUp})
	if s.cinema != nil {
		t.Error("a channel button should leave the code screen")
	}
	if s.Tuned() != "clips" {
		t.Errorf("tuned %q; leaving the code screen should not change channel", s.Tuned())
	}
}

func TestZeroInsideAChannelNumberIsNotMovieMode(t *testing.T) {
	channels := append(keyChannels(), schedule.Channel{
		ID: "ten", Number: 10, Name: "Ten",
		Items: []schedule.Item{{ID: "t1", Title: "Ten One", Path: "/lib/t1.mp4", Duration: 10 * time.Minute}},
	})
	s, _, _, _ := movieStation(t, channels, upMovie())
	press(s, "10")
	if s.cinema != nil {
		t.Error("1 then 0 is channel 10, not Movie Mode")
	}
	if s.Tuned() != "ten" {
		t.Errorf("tuned %q, want ten", s.Tuned())
	}
}

func TestWithoutAShelfZeroIsJustANumber(t *testing.T) {
	p := newFakePlayer()
	s := newTestStation(t, p, &fakeClock{now: noon()}, "clips", keyChannels())
	s.play()
	press(s, "0")
	if s.cinema != nil {
		t.Error("Movie Mode should be off without a code")
	}
}

func TestPlayingAMovie(t *testing.T) {
	s, p, _, root := movieStation(t, keyChannels(), dvdMovie(), upMovie())
	press(s, "0"+testPIN)

	// Big then Up, by title; 2 is Up.
	press(s, "2")
	loads := p.MovieLoads()
	if len(loads) != 1 {
		t.Fatalf("movie loads %v", loads)
	}
	want := movieLoad{Path: filepath.Join(moviesDir(root), "Up (2009).mkv"), Offset: 0,
		Opts: player.MovieOptions{Audio: 1, Subtitle: 1}}
	if loads[0] != want {
		t.Errorf("loaded %+v, want %+v", loads[0], want)
	}
	if s.cinema.screen != screenPlaying {
		t.Fatalf("screen %v", s.cinema.screen)
	}

	press(s, "5")
	if !p.Paused() || !strings.HasPrefix(lastText(t, p).Text, "II") {
		t.Errorf("5 should pause and say so: paused %v, text %q", p.Paused(), lastText(t, p).Text)
	}
	press(s, "5")
	if p.Paused() {
		t.Error("5 again should play")
	}

	press(s, "6")
	press(s, "7")
	if got := p.seeks; len(got) != 2 || got[0] != 10*time.Second || got[1] != -5*time.Minute {
		t.Errorf("seeks %v", got)
	}

	// Subtitles: English, then French, then off, which shows the forced
	// English track, then English again.
	press(s, "888")
	tracks := p.Tracks()
	wantTracks := []trackCall{{player.SubtitleTrack, 3}, {player.SubtitleTrack, 2}, {player.SubtitleTrack, 1}}
	if len(tracks) != 3 || tracks[0] != wantTracks[0] || tracks[1] != wantTracks[1] || tracks[2] != wantTracks[2] {
		t.Errorf("subtitle tracks %v, want %v", tracks, wantTracks)
	}
	if got := lastText(t, p).Text; got != "Subtitles: English" {
		t.Errorf("showed %q", got)
	}

	press(s, "2")
	if got := p.Tracks(); got[len(got)-1] != (trackCall{player.AudioTrack, 2}) {
		t.Errorf("audio track %v", got[len(got)-1])
	}
	if got := lastText(t, p).Text; got != "Audio: English · Commentary" {
		t.Errorf("showed %q", got)
	}

	broadcastLoads := len(p.Loads())
	s.key(input.Key{Action: input.ChannelUp})
	s.key(input.Key{Action: input.ChannelDown})
	if len(p.Loads()) != broadcastLoads || s.cinema.screen != screenPlaying {
		t.Error("channel buttons should do nothing during a movie")
	}
}

func TestStoppedMoviesResume(t *testing.T) {
	s, p, _, root := movieStation(t, keyChannels(), upMovie())
	path := filepath.Join(moviesDir(root), "Up (2009).mkv")
	press(s, "0"+testPIN+"1")

	p.setPosition(path, 40*time.Minute)
	press(s, "0")
	if s.cinema.screen != screenMenu {
		t.Fatalf("0 during a movie should stop it for the menu, screen %v", s.cinema.screen)
	}
	positions, err := movie.ReadPositions(filepath.Join(root, "movie-positions.json"))
	if err != nil || positions["Up (2009).mkv"] != 40*time.Minute {
		t.Fatalf("positions %v %v", positions, err)
	}

	press(s, "1")
	if s.cinema.screen != screenResume {
		t.Fatalf("a movie stopped partway should ask, screen %v", s.cinema.screen)
	}
	press(s, "1")
	loads := p.MovieLoads()
	if got := loads[len(loads)-1]; got.Offset != 40*time.Minute {
		t.Errorf("resumed at %v, want 40m", got.Offset)
	}

	p.setPosition(path, 50*time.Minute)
	s.reconcile()
	positions, _ = movie.ReadPositions(filepath.Join(root, "movie-positions.json"))
	if positions["Up (2009).mkv"] != 50*time.Minute {
		t.Errorf("the reconcile tick should write the position down: %v", positions)
	}

	press(s, "0")
	press(s, "1")
	press(s, "2")
	loads = p.MovieLoads()
	if got := loads[len(loads)-1]; got.Offset != 0 {
		t.Errorf("start over began at %v", got.Offset)
	}
}

func TestTheEnd(t *testing.T) {
	s, p, clock, root := movieStation(t, keyChannels(), upMovie())
	path := filepath.Join(moviesDir(root), "Up (2009).mkv")
	if err := (movie.Positions{"Up (2009).mkv": 40 * time.Minute}).Write(filepath.Join(root, "movie-positions.json")); err != nil {
		t.Fatal(err)
	}
	press(s, "0"+testPIN+"1"+"1")

	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonEOF, Path: path})
	if s.cinema.screen != screenEnded {
		t.Fatalf("screen %v, want the end", s.cinema.screen)
	}
	positions, _ := movie.ReadPositions(filepath.Join(root, "movie-positions.json"))
	if _, ok := positions["Up (2009).mkv"]; ok {
		t.Error("a movie watched to the end should be forgotten")
	}

	clock.Advance(theEndShown)
	s.movieTimeout()
	if s.cinema.screen != screenMenu {
		t.Fatalf("screen %v, want the menu after The End", s.cinema.screen)
	}

	// Back to TV: the channel comes back where the clock says it is.
	loads := len(p.Loads())
	press(s, "0")
	if s.cinema != nil {
		t.Fatal("0 on the menu should leave Movie Mode")
	}
	if len(p.Loads()) != loads+1 {
		t.Error("leaving should load what the channel is airing")
	}
	if got := p.LastLoad(t); got.Path != wantSlot(t, keyChannels()[1], clock.Now()).Item.Path {
		t.Errorf("loaded %q", got.Path)
	}
}

func TestIdleMenuGoesBackToTV(t *testing.T) {
	s, _, clock, _ := movieStation(t, keyChannels(), upMovie())
	press(s, "0"+testPIN)
	clock.Advance(menuIdle)
	s.movieTimeout()
	if s.cinema != nil {
		t.Error("an idle menu should close Movie Mode")
	}
}

func TestTwoDigitMovieNumbers(t *testing.T) {
	var movies []movie.Movie
	for i := range 12 {
		movies = append(movies, movie.Movie{Title: "Film " + string(rune('A'+i)), File: "f" + string(rune('a'+i)) + ".mkv", Duration: 3600})
	}
	s, p, clock, root := movieStation(t, keyChannels(), movies...)
	press(s, "0"+testPIN)

	press(s, "1")
	if len(p.MovieLoads()) != 0 || s.cinema.typed != "1" {
		t.Fatal("1 of 12 should wait for a second digit")
	}
	press(s, "2")
	if loads := p.MovieLoads(); len(loads) != 1 || loads[0].Path != filepath.Join(moviesDir(root), "fl.mkv") {
		t.Fatalf("12 should play the twelfth: %v", loads)
	}

	press(s, "0")
	press(s, "1")
	clock.Advance(input.DigitTimeout)
	s.movieTimeout()
	if loads := p.MovieLoads(); len(loads) != 2 || loads[1].Path != filepath.Join(moviesDir(root), "fa.mkv") {
		t.Errorf("1 on its own should play the first once the wait is over: %v", loads)
	}
}

func TestMenuPages(t *testing.T) {
	var movies []movie.Movie
	for i := range 12 {
		movies = append(movies, movie.Movie{Title: "Film " + string(rune('A'+i)), File: "f" + string(rune('a'+i)) + ".mkv", Duration: 3600})
	}
	s, _, _, _ := movieStation(t, keyChannels(), movies...)
	press(s, "0"+testPIN)
	if page := s.menuPage(); page.Pages != 2 || len(page.Tiles) != 10 || page.Wide || page.Title != "MOVIE NIGHT" {
		t.Fatalf("page %d of %d with %d movies, wide %v, %q", page.Page, page.Pages, len(page.Tiles), page.Wide, page.Title)
	}
	s.key(input.Key{Action: input.ChannelUp})
	if page := s.menuPage(); page.Page != 2 || len(page.Tiles) != 2 || page.Tiles[0].Number != 11 {
		t.Errorf("second page %+v", page)
	}
	s.key(input.Key{Action: input.ChannelUp})
	if page := s.menuPage(); page.Page != 1 {
		t.Errorf("paging should wrap, on page %d", page.Page)
	}
}

func TestMpvRestartDuringAMovie(t *testing.T) {
	s, p, _, root := movieStation(t, keyChannels(), dvdMovie())
	path := filepath.Join(moviesDir(root), "Big (1988).mkv")
	press(s, "0"+testPIN+"1")
	p.setPosition(path, 20*time.Minute)
	press(s, "6")

	s.handle(player.Event{Kind: player.Restarted})
	loads := p.MovieLoads()
	got := loads[len(loads)-1]
	if got.Path != path || got.Offset < 20*time.Minute || !got.Opts.Deinterlace {
		t.Errorf("after a restart loaded %+v, want Big at 20m10s deinterlaced", got)
	}
}

func TestProgressText(t *testing.T) {
	if got := clockTime(time.Hour + 2*time.Minute + 3*time.Second); got != "1:02:03" {
		t.Errorf("got %q", got)
	}
	if got := progressBar(30*time.Minute, time.Hour); got != strings.Repeat("█", 12)+strings.Repeat("░", 12) {
		t.Errorf("got %q", got)
	}
	if got := progressBar(2*time.Hour, time.Hour); got != strings.Repeat("█", 24) {
		t.Errorf("past the end: %q", got)
	}
}

// putOnShelf saves entries as the shelf under root.
func putOnShelf(t *testing.T, root string, entries ...shelf.Entry) {
	t.Helper()
	if _, err := shelf.NewStore(root).Write(shelf.Shelf{Entries: entries}); err != nil {
		t.Fatal(err)
	}
}

// shelfStation is a movie station with Up, and on the shelf Doc One on its
// own and a Clips folder holding Clip B, Clip A and a video that has gone.
func shelfStation(t *testing.T) (*station, *fakePlayer, *fakeClock, string) {
	t.Helper()
	s, p, clock, root := movieStation(t, keyChannels(), upMovie())
	putOnShelf(t, root,
		shelf.Entry{Item: &shelf.Item{Channel: "docs", ID: "d1", Title: "The Doc"}},
		shelf.Entry{Folder: &shelf.Folder{Name: "Clips", Items: []shelf.Item{
			{Channel: "clips", ID: "b"}, {Channel: "clips", ID: "a"}, {Channel: "clips", ID: "gone"},
		}}},
	)
	return s, p, clock, root
}

func TestOnDemandMenu(t *testing.T) {
	s, _, _, _ := shelfStation(t)
	press(s, "0"+testPIN)
	page := s.menuPage()
	if page.Title != "ON DEMAND" || !page.Wide || page.Back {
		t.Errorf("top %q wide %v back %v", page.Title, page.Wide, page.Back)
	}
	var titles []string
	for _, tile := range page.Tiles {
		titles = append(titles, tile.Title+"|"+tile.Detail)
	}
	want := []string{"Movies|1 movie", "The Doc|", "Clips|2 episodes"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Errorf("tiles %v, want %v", titles, want)
	}

	press(s, "3")
	page = s.menuPage()
	if page.Title != "CLIPS" || !page.Back || len(page.Tiles) != 2 || page.Tiles[0].Title != "Clip B" {
		t.Errorf("inside Clips: %+v", page)
	}
	press(s, "0")
	if s.cinema == nil || s.cinema.inside != nil {
		t.Fatal("0 inside a folder should go back up, not leave")
	}

	press(s, "1")
	page = s.menuPage()
	if page.Title != "MOVIE NIGHT" || page.Wide || len(page.Tiles) != 1 || page.Tiles[0].Title != "Up (2009)" {
		t.Errorf("inside Movies: %+v", page)
	}
	press(s, "00")
	if s.cinema != nil {
		t.Error("0 at the top should leave On Demand")
	}
}

func TestPlayingAnEpisode(t *testing.T) {
	s, p, clock, root := shelfStation(t)
	press(s, "0"+testPIN+"3"+"2")
	loads := p.MovieLoads()
	if len(loads) != 1 {
		t.Fatalf("loads %v", loads)
	}
	want := movieLoad{Path: "/lib/a.mp4", Opts: player.MovieOptions{Audio: 1}}
	if loads[0] != want {
		t.Errorf("loaded %+v, want %+v", loads[0], want)
	}

	tracks := len(p.Tracks())
	press(s, "82")
	if len(p.Tracks()) != tracks {
		t.Error("an episode has no tracks to change")
	}

	// Stopped four minutes into a ten minute clip, it offers to resume.
	p.setPosition("/lib/a.mp4", 4*time.Minute)
	press(s, "0")
	positions, err := movie.ReadPositions(filepath.Join(root, "movie-positions.json"))
	if err != nil || positions["clips/a"] != 4*time.Minute {
		t.Fatalf("positions %v %v", positions, err)
	}
	if page := s.menuPage(); page.Title != "CLIPS" {
		t.Errorf("stopping should go back to the folder, on %q", page.Title)
	}
	press(s, "2")
	if s.cinema.screen != screenResume {
		t.Fatalf("screen %v, want the resume question", s.cinema.screen)
	}
	press(s, "1")
	if got := p.MovieLoads(); got[len(got)-1].Offset != 4*time.Minute {
		t.Errorf("resumed at %v", got[len(got)-1].Offset)
	}

	s.handle(player.Event{Kind: player.EndFile, Reason: player.ReasonEOF, Path: "/lib/a.mp4"})
	clock.Advance(theEndShown)
	s.movieTimeout()
	if s.cinema.screen != screenMenu || s.menuPage().Title != "CLIPS" {
		t.Errorf("after The End: screen %v on %q, want the folder", s.cinema.screen, s.menuPage().Title)
	}
}

func TestEmptyShelfIsTheMovieMenu(t *testing.T) {
	s, _, _, root := movieStation(t, keyChannels(), upMovie())
	// Everything on the shelf has left the library.
	putOnShelf(t, root, shelf.Entry{Item: &shelf.Item{Channel: "clips", ID: "gone"}})
	press(s, "0"+testPIN)
	if page := s.menuPage(); page.Title != "MOVIE NIGHT" || page.Wide || len(page.Tiles) != 1 {
		t.Errorf("page %+v, want the movies alone", page)
	}
}

func TestBuildMenuWithoutMovies(t *testing.T) {
	top := buildMenu(nil, "/movies", []shelf.Shown{{Video: &shelf.Video{
		Item: shelf.Item{Channel: "clips", ID: "a"}, Title: "Clip A",
		Video: schedule.Item{ID: "a", Path: "/lib/a.mp4", Duration: time.Minute},
	}}})
	if top.title != "ON DEMAND" || len(top.tiles) != 1 || top.tiles[0].play.key != "clips/a" {
		t.Errorf("top %+v", top)
	}
}
