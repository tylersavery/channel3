package movie

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Shelf lists the prepared movies in dir, in title order with a leading
// "The", "A" or "An" ignored, as a video shop would shelve them. A sidecar
// that cannot be read, or whose movie file is missing, is reported in skipped
// and left off the shelf rather than failing it.
func Shelf(dir string) (movies []Movie, skipped []error, err error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	for _, p := range paths {
		m, err := ReadSidecar(p)
		if err != nil {
			skipped = append(skipped, err)
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, m.File)); err != nil {
			skipped = append(skipped, fmt.Errorf("%s: %w", filepath.Base(p), err))
			continue
		}
		movies = append(movies, m)
	}
	slices.SortFunc(movies, func(a, b Movie) int {
		if c := strings.Compare(shelfKey(a.Title), shelfKey(b.Title)); c != 0 {
			return c
		}
		return a.Year - b.Year
	})
	return movies, skipped, nil
}

// shelfKey is the title a movie is shelved under.
func shelfKey(title string) string {
	t := strings.ToLower(title)
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(t, article); ok && rest != "" {
			return rest
		}
	}
	return t
}

// Positions is where each movie was stopped, by file name, for "Resume or
// Start over?".
//
// It is the one piece of playback state Channel Three keeps, by the owner's
// decision: broadcast never reads it, and it lives in one small file.
type Positions map[string]time.Duration

// positionsFile is the file's form: seconds, which reads plainly if anyone
// opens it.
type positionsFile map[string]float64

// ReadPositions reads the positions file. A file that does not exist yet is
// no positions, not an error.
func ReadPositions(path string) (Positions, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Positions{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f positionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	p := make(Positions, len(f))
	for name, seconds := range f {
		p[name] = time.Duration(seconds * float64(time.Second))
	}
	return p, nil
}

// Write saves the positions whole or not at all.
func (p Positions) Write(path string) error {
	f := make(positionsFile, len(p))
	for name, d := range p {
		f[name] = d.Round(time.Second).Seconds()
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Resume thresholds: a movie stopped in its first couple of minutes starts
// over, and one stopped in its last few, which is the credits, counts as
// watched.
const (
	resumeAfter  = 2 * time.Minute
	finishedLast = 5 * time.Minute
)

// ResumeAt is where m should offer to resume from, or zero to start over.
func (p Positions) ResumeAt(m Movie) time.Duration {
	at := p[m.File]
	length := time.Duration(m.Duration * float64(time.Second))
	if at < resumeAfter || (length > 0 && at > length-finishedLast) {
		return 0
	}
	return at
}
