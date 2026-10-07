package movie

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tylersavery/channel3/internal/homevideo"
)

// The largest picture a prepared movie keeps. The television today is 720p;
// 1080p is kept for the next one.
const (
	maxWidth  = 1920
	maxHeight = 1080
)

// playsAsIs are the video codecs the Pi decodes without help: H.264 from most
// rips, MPEG-2 from DVDs, and 8 bit HEVC, which the Pi 5 has a hardware
// decoder for. Anything else is re-encoded once rather than risked on air.
var playsAsIs = map[string]bool{"h264": true, "mpeg2video": true, "hevc": true}

// eightBit are the pixel formats a remuxed movie may keep.
var eightBit = map[string]bool{"yuv420p": true, "yuvj420p": true}

// keptSubtitles are the subtitle codecs Matroska holds as they are.
var keptSubtitles = map[string]bool{
	"subrip": true, "ass": true, "ssa": true, "webvtt": true,
	"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true,
}

// convertedSubtitles are text subtitle codecs Matroska cannot hold, rewritten
// as SubRip. mov_text is what an MP4 carries.
var convertedSubtitles = map[string]bool{"mov_text": true}

// Plan is how one movie is prepared.
type Plan struct {
	// Encode says the video is re-encoded; otherwise every stream is copied.
	Encode bool
	// Reason says why it is re-encoded, for the log.
	Reason string
	// Audio and Subtitles are the kept tracks in the order they are written.
	// The first audio track and, when there is one, the first subtitle track
	// are the defaults mpv picks.
	Audio     []Track
	Subtitles []Track
	// DefaultSubtitle says the first subtitle track is a full English one
	// and is marked to show by default.
	DefaultSubtitle bool
}

// MakePlan decides how src is prepared. external are the .srt files dropped
// beside it.
func MakePlan(src Source, external []Track) Plan {
	p := Plan{Audio: orderAudio(src.Audio)}
	p.Encode, p.Reason = needsEncode(src.Video)

	var subs []Track
	for _, t := range src.Subtitles {
		if keptSubtitles[t.Codec] || convertedSubtitles[t.Codec] {
			subs = append(subs, t)
		}
	}
	subs = append(subs, external...)
	p.Subtitles, p.DefaultSubtitle = orderSubtitles(subs)
	return p
}

// needsEncode says whether v must be re-encoded to play well, and why.
func needsEncode(v Video) (bool, string) {
	switch {
	case !playsAsIs[v.Codec]:
		return true, v.Codec + " video"
	case v.Width > maxWidth || v.Height > maxHeight:
		return true, fmt.Sprintf("%dx%d is larger than 1080p", v.Width, v.Height)
	case v.HDR():
		return true, "HDR"
	case !eightBit[v.PixFmt]:
		return true, v.PixFmt + " is not 8 bit"
	}
	return false, ""
}

// orderAudio puts the track that should play first at the front: the first
// English track that is not a commentary, else the file's default, else the
// first that is not a commentary. The rest keep their order.
func orderAudio(tracks []Track) []Track {
	pick := -1
	for _, rule := range []func(Track) bool{
		func(t Track) bool { return t.English() && !t.Comment },
		func(t Track) bool { return t.Default && !t.Comment },
		func(t Track) bool { return !t.Comment },
	} {
		if pick = slices.IndexFunc(tracks, rule); pick >= 0 {
			break
		}
	}
	return moveToFront(tracks, pick)
}

// orderSubtitles puts the full English track first and marks it default, with
// a forced English track, which only covers foreign-language scenes, right
// behind it. Hearing-impaired (SDH) tracks are passed over for the default
// when a plain one exists. The rest keep their order.
func orderSubtitles(tracks []Track) ([]Track, bool) {
	full := func(t Track) bool { return t.English() && !t.Forced && !t.Comment }
	plain := slices.IndexFunc(tracks, func(t Track) bool { return full(t) && !sdh(t) })
	if plain < 0 {
		plain = slices.IndexFunc(tracks, full)
	}
	ordered := moveToFront(tracks, plain)
	start := 0
	if plain >= 0 {
		start = 1
	}
	if forced := slices.IndexFunc(ordered[start:], func(t Track) bool { return t.English() && t.Forced }); forced >= 0 {
		rest := moveToFront(ordered[start:], forced)
		ordered = append(ordered[:start:start], rest...)
	}
	return ordered, plain >= 0
}

// sdh reports a subtitle track for the deaf and hard of hearing.
func sdh(t Track) bool {
	title := strings.ToLower(t.Title)
	return strings.Contains(title, "sdh") || strings.Contains(title, "hearing")
}

// moveToFront returns a copy of tracks with tracks[i] first. A negative i
// returns the tracks as they are.
func moveToFront(tracks []Track, i int) []Track {
	out := make([]Track, 0, len(tracks))
	if i >= 0 {
		out = append(out, tracks[i])
	}
	for j, t := range tracks {
		if j != i {
			out = append(out, t)
		}
	}
	return out
}

// VideoFilter scales a re-encoded movie down to fit 1080p, never up, and tone
// maps HDR.
func VideoFilter(v Video) string {
	scale := fmt.Sprintf("scale=w='min(%d,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2,format=yuv420p",
		maxWidth, maxHeight)
	if v.HDR() {
		return homevideo.ToneMap + "," + scale
	}
	return scale
}

// Args returns ffmpeg's arguments to prepare src into dst, a Matroska file,
// following p. title is written as the file's title.
func Args(src Source, p Plan, srcPath, dst, title string, threads int) []string {
	args := []string{"-hide_banner", "-v", "error", "-nostdin", "-y", "-i", srcPath}
	input := map[string]int{}
	for _, t := range p.Subtitles {
		if t.External == "" {
			continue
		}
		if !utf8Text(t.External) {
			// Older .srt files are Windows-1252. ffmpeg reads UTF-8 only
			// unless told otherwise, and Matroska stores UTF-8.
			args = append(args, "-sub_charenc", "CP1252")
		}
		input[t.External] = len(input) + 1
		args = append(args, "-i", t.External)
	}

	args = append(args, "-map", "0:"+strconv.Itoa(src.Video.Index))
	for _, t := range p.Audio {
		args = append(args, "-map", "0:"+strconv.Itoa(t.Index))
	}
	for _, t := range p.Subtitles {
		if t.External != "" {
			args = append(args, "-map", strconv.Itoa(input[t.External])+":0")
		} else {
			args = append(args, "-map", "0:"+strconv.Itoa(t.Index))
		}
	}
	if src.Attachments > 0 {
		args = append(args, "-map", "0:t")
	}

	args = append(args, "-c", "copy")
	if p.Encode {
		args = append(args,
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-profile:v", "high",
			"-vf", VideoFilter(src.Video),
			"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709",
		)
	}

	for i := range p.Audio {
		disposition := "0"
		if i == 0 {
			disposition = "default"
		}
		args = append(args, "-disposition:a:"+strconv.Itoa(i), disposition)
	}
	for i, t := range p.Subtitles {
		stream := strconv.Itoa(i)
		if t.External != "" || convertedSubtitles[t.Codec] {
			args = append(args, "-c:s:"+stream, "srt")
		}
		if t.External != "" {
			if t.Language != "" {
				args = append(args, "-metadata:s:s:"+stream, "language="+t.Language)
			}
			args = append(args, "-metadata:s:s:"+stream, "title="+t.Title)
		}
		var disposition []string
		if i == 0 && p.DefaultSubtitle {
			disposition = append(disposition, "default")
		}
		if t.Forced {
			disposition = append(disposition, "forced")
		}
		value := "0"
		if len(disposition) > 0 {
			value = strings.Join(disposition, "+")
		}
		args = append(args, "-disposition:s:"+stream, value)
	}

	args = append(args, "-metadata", "title="+title)
	if threads > 0 {
		args = append(args, "-threads", strconv.Itoa(threads))
	}
	return append(args, "-f", "matroska", dst)
}

// utf8Text reports whether a subtitle file reads as UTF-8. A file that cannot
// be read is reported as UTF-8, so ffmpeg is the one to say what is wrong.
func utf8Text(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	return utf8.Valid(data)
}

// languageCodes turns the two-letter codes and names people put in subtitle
// file names into the three-letter codes Matroska uses.
var languageCodes = map[string]string{
	"en": "eng", "eng": "eng", "english": "eng",
	"fr": "fre", "fre": "fre", "fra": "fre", "french": "fre",
	"es": "spa", "spa": "spa", "spanish": "spa",
	"de": "ger", "ger": "ger", "deu": "ger", "german": "ger",
	"it": "ita", "ita": "ita", "italian": "ita",
	"ja": "jpn", "jpn": "jpn", "japanese": "jpn",
	"pt": "por", "por": "por", "portuguese": "por",
	"nl": "dut", "dut": "dut", "nld": "dut", "dutch": "dut",
	"zh": "chi", "chi": "chi", "zho": "chi", "chinese": "chi",
	"ko": "kor", "kor": "kor", "korean": "kor",
}

// ExternalSubtitles finds the .srt files beside the movie at moviePath:
// "Up (2009).srt", "Up (2009).en.srt", "Up (2009).eng.forced.srt",
// "Up (2009).en.sdh.srt". A file with no language is taken to be English,
// because that is what a subtitle file dropped beside a movie in this house is.
func ExternalSubtitles(moviePath string) ([]Track, error) {
	dir := filepath.Dir(moviePath)
	base := strings.TrimSuffix(filepath.Base(moviePath), filepath.Ext(moviePath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var tracks []Track
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".srt") {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if stem != base && !strings.HasPrefix(stem, base+".") {
			continue
		}
		t := Track{Codec: "subrip", Language: "eng", External: filepath.Join(dir, name)}
		var notes []string
		for _, part := range strings.Split(strings.TrimPrefix(stem, base), ".")[1:] {
			switch p := strings.ToLower(part); {
			case p == "forced":
				t.Forced = true
				notes = append(notes, "forced")
			case p == "sdh" || p == "cc" || p == "hi":
				notes = append(notes, "SDH")
			case languageCodes[p] != "":
				t.Language = languageCodes[p]
			}
		}
		t.Title = languageTitle(t.Language)
		if len(notes) > 0 {
			t.Title += " (" + strings.Join(notes, ", ") + ")"
		}
		tracks = append(tracks, t)
	}
	slices.SortFunc(tracks, func(a, b Track) int { return strings.Compare(a.External, b.External) })
	return tracks, nil
}

// languageTitle names a three-letter language code for a track's title.
func languageTitle(code string) string {
	names := map[string]string{
		"eng": "English", "fre": "French", "spa": "Spanish", "ger": "German", "ita": "Italian",
		"jpn": "Japanese", "por": "Portuguese", "dut": "Dutch", "chi": "Chinese", "kor": "Korean",
	}
	if n, ok := names[code]; ok {
		return n
	}
	return code
}
