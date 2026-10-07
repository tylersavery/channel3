package movie

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Name is a movie's title and year, read from its file name.
type Name struct {
	Title string
	Year  int // 0 when the file name has none
}

// String is the name the prepared files are stored under and the menu shows:
// "The Iron Giant (1999)", or the title alone without a year.
func (n Name) String() string {
	if n.Year == 0 {
		return n.Title
	}
	return fmt.Sprintf("%s (%d)", n.Title, n.Year)
}

var (
	// yearToken is a plausible release year standing on its own, with or
	// without brackets.
	yearToken = regexp.MustCompile(`[\(\[]?\b(19\d{2}|20\d{2})\b[\)\]]?`)
	// makeMKVTitle is the "_t00" MakeMKV puts on the end of every title it
	// rips.
	makeMKVTitle = regexp.MustCompile(`_t\d{2}$`)
	// releaseJunk is where a scene-style name stops being the title.
	releaseJunk = regexp.MustCompile(`(?i)\b(2160p|1080p|1080i|720p|576p|480p|4k|uhd|bluray|blu-ray|bdrip|brrip|remux|dvdrip|dvd|web-?dl|webrip|hdr|x264|x265|h\.?264|h\.?265|hevc|avc|aac|ac3|dts|truehd|atmos)\b`)
	// separators are the characters file names use for spaces.
	separators = strings.NewReplacer(".", " ", "_", " ")
	// spaces collapses runs of spaces.
	spaces = regexp.MustCompile(`\s+`)
)

// ParseName reads a title and year from a movie file's name.
//
// It understands the names people give their rips, "The Iron Giant (1999).mkv"
// and "The.Iron.Giant.1999.1080p.BluRay.x264.mkv", and MakeMKV's
// "THE_IRON_GIANT_t00.mkv". The year is the last one in the name that is not
// its first word, so "1917 (2019)" is 1917 from 2019 and "2001 A Space Odyssey"
// keeps its title. Everything after the year, or after the first release tag
// such as 1080p when there is no year, is dropped. A name in capitals, as
// MakeMKV writes them, is put in title case.
func ParseName(path string) Name {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = makeMKVTitle.ReplaceAllString(base, "")
	clean := strings.TrimSpace(spaces.ReplaceAllString(separators.Replace(base), " "))

	var n Name
	title := clean
	if locs := yearToken.FindAllStringSubmatchIndex(clean, -1); len(locs) > 0 {
		for i := len(locs) - 1; i >= 0; i-- {
			loc := locs[i]
			if loc[0] == 0 {
				continue
			}
			n.Year, _ = strconv.Atoi(clean[loc[2]:loc[3]])
			title = clean[:loc[0]]
			break
		}
	}
	if loc := releaseJunk.FindStringIndex(title); loc != nil && loc[0] > 0 {
		title = title[:loc[0]]
	}
	title = strings.TrimRight(strings.TrimSpace(title), " -([")
	if title == "" {
		title = clean
	}
	if title == strings.ToUpper(title) && title != strings.ToLower(title) {
		title = titleCase(title)
	}
	n.Title = title
	return n
}

// smallWords stay lower case inside a title.
var smallWords = map[string]bool{
	"a": true, "an": true, "and": true, "at": true, "by": true, "for": true, "in": true,
	"of": true, "on": true, "or": true, "the": true, "to": true, "with": true,
}

// titleCase turns "THE IRON GIANT" into "The Iron Giant".
func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if i > 0 && smallWords[w] {
			continue
		}
		r := []rune(w)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
