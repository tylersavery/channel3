package movie

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// blurayProbe is a trimmed ffprobe of a MakeMKV Blu-ray rip: H.264 video, a
// commentary ahead of the English 5.1, French audio, and PGS subtitles with a
// forced English track and an SDH one ahead of the plain English one.
const blurayProbe = `{
 "streams": [
  {"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"pix_fmt":"yuv420p","field_order":"progressive","disposition":{"default":1}},
  {"index":1,"codec_type":"audio","codec_name":"ac3","channels":2,"tags":{"language":"eng","title":"Director's Commentary"}},
  {"index":2,"codec_type":"audio","codec_name":"truehd","channels":6,"disposition":{"default":1},"tags":{"language":"eng","title":"Surround 5.1"}},
  {"index":3,"codec_type":"audio","codec_name":"ac3","channels":6,"tags":{"language":"fre"}},
  {"index":4,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","disposition":{"forced":1},"tags":{"language":"eng","title":"Forced"}},
  {"index":5,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"eng","title":"English SDH"}},
  {"index":6,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"eng"}},
  {"index":7,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"fre"}},
  {"index":8,"codec_type":"subtitle","codec_name":"eia_608"}
 ],
 "format": {"duration":"5412.480000","tags":{"title":"THE_IRON_GIANT"}}
}`

func TestParseProbe(t *testing.T) {
	src, err := parseProbe([]byte(blurayProbe))
	if err != nil {
		t.Fatal(err)
	}
	if src.Duration != 5412.48 || src.Title != "THE_IRON_GIANT" {
		t.Errorf("duration %v title %q", src.Duration, src.Title)
	}
	if src.Video.Codec != "h264" || src.Video.Width != 1920 || src.Video.Interlaced() || src.Video.HDR() {
		t.Errorf("video %+v", src.Video)
	}
	if len(src.Audio) != 3 || !src.Audio[0].Comment || !src.Audio[1].Default {
		t.Errorf("audio %+v", src.Audio)
	}
	if len(src.Subtitles) != 5 || !src.Subtitles[0].Forced || !src.Subtitles[0].Picture() {
		t.Errorf("subtitles %+v", src.Subtitles)
	}
}

func TestParseProbeSkipsCoverArt(t *testing.T) {
	src, err := parseProbe([]byte(`{"streams":[
		{"index":0,"codec_type":"video","codec_name":"mjpeg","width":600,"height":900,"disposition":{"attached_pic":1}},
		{"index":1,"codec_type":"video","codec_name":"h264","width":1280,"height":720,"pix_fmt":"yuv420p"}],
		"format":{"duration":"60"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if src.Video.Index != 1 {
		t.Errorf("picked stream %d, want the film at 1", src.Video.Index)
	}
}

func TestParseProbeWithoutVideoIsAnError(t *testing.T) {
	if _, err := parseProbe([]byte(`{"streams":[{"index":0,"codec_type":"audio"}],"format":{}}`)); err == nil {
		t.Error("want an error")
	}
}

func TestNeedsEncode(t *testing.T) {
	tests := []struct {
		name   string
		video  Video
		encode bool
	}{
		{"bluray h264", Video{Codec: "h264", Width: 1920, Height: 1080, PixFmt: "yuv420p"}, false},
		{"dvd mpeg2", Video{Codec: "mpeg2video", Width: 720, Height: 480, PixFmt: "yuv420p", FieldOrder: "tt"}, false},
		{"hevc 8 bit", Video{Codec: "hevc", Width: 1920, Height: 800, PixFmt: "yuv420p"}, false},
		{"4k hevc hdr", Video{Codec: "hevc", Width: 3840, Height: 2160, PixFmt: "yuv420p10le", ColorTransfer: "smpte2084"}, true},
		{"1080p hdr", Video{Codec: "hevc", Width: 1920, Height: 1080, PixFmt: "yuv420p10le", ColorTransfer: "smpte2084"}, true},
		{"10 bit sdr", Video{Codec: "h264", Width: 1920, Height: 1080, PixFmt: "yuv420p10le"}, true},
		{"vc1", Video{Codec: "vc1", Width: 1920, Height: 1080, PixFmt: "yuv420p"}, true},
		{"4k sdr h264", Video{Codec: "h264", Width: 3840, Height: 1600, PixFmt: "yuv420p"}, true},
	}
	for _, tt := range tests {
		got, reason := needsEncode(tt.video)
		if got != tt.encode {
			t.Errorf("%s: encode = %v (%s), want %v", tt.name, got, reason, tt.encode)
		}
		if got && reason == "" {
			t.Errorf("%s: no reason given", tt.name)
		}
	}
}

func indexes(tracks []Track) []int {
	var out []int
	for _, t := range tracks {
		out = append(out, t.Index)
	}
	return out
}

func TestOrderAudio(t *testing.T) {
	tests := []struct {
		name   string
		tracks []Track
		want   []int
	}{
		{"english after a commentary", []Track{
			{Index: 1, Language: "eng", Comment: true}, {Index: 2, Language: "eng"}, {Index: 3, Language: "fre"},
		}, []int{2, 1, 3}},
		{"no english, file default", []Track{
			{Index: 1, Language: "jpn"}, {Index: 2, Language: "fre", Default: true},
		}, []int{2, 1}},
		{"untagged, skip commentary", []Track{
			{Index: 1, Comment: true}, {Index: 2}, {Index: 3},
		}, []int{2, 1, 3}},
		{"only a commentary", []Track{{Index: 1, Comment: true}}, []int{1}},
		{"none", nil, nil},
	}
	for _, tt := range tests {
		if got := indexes(orderAudio(tt.tracks)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestOrderSubtitles(t *testing.T) {
	tests := []struct {
		name        string
		tracks      []Track
		want        []int
		wantDefault bool
	}{
		{"plain english, forced next, sdh passed over", []Track{
			{Index: 4, Language: "eng", Forced: true},
			{Index: 5, Language: "eng", Title: "English SDH"},
			{Index: 6, Language: "eng"},
			{Index: 7, Language: "fre"},
		}, []int{6, 4, 5, 7}, true},
		{"only sdh english", []Track{
			{Index: 3, Language: "fre"}, {Index: 4, Language: "eng", Title: "SDH"},
		}, []int{4, 3}, true},
		{"only forced english", []Track{
			{Index: 3, Language: "fre"}, {Index: 4, Language: "eng", Forced: true},
		}, []int{4, 3}, false},
		{"no english", []Track{{Index: 3, Language: "fre"}, {Index: 4, Language: "spa"}}, []int{3, 4}, false},
		{"none", nil, nil, false},
	}
	for _, tt := range tests {
		got, def := orderSubtitles(tt.tracks)
		if !slices.Equal(indexes(got), tt.want) || def != tt.wantDefault {
			t.Errorf("%s: got %v default %v, want %v default %v", tt.name, indexes(got), def, tt.want, tt.wantDefault)
		}
	}
}

func TestMakePlanDropsSubtitlesMatroskaCannotHold(t *testing.T) {
	src, err := parseProbe([]byte(blurayProbe))
	if err != nil {
		t.Fatal(err)
	}
	p := MakePlan(src, nil)
	if p.Encode {
		t.Errorf("a 1080p H.264 rip should be remuxed, got %s", p.Reason)
	}
	if got := indexes(p.Subtitles); !slices.Equal(got, []int{6, 4, 5, 7}) {
		t.Errorf("subtitles %v", got)
	}
	if got := indexes(p.Audio); !slices.Equal(got, []int{2, 1, 3}) {
		t.Errorf("audio %v", got)
	}
}

func TestArgsRemux(t *testing.T) {
	src, err := parseProbe([]byte(blurayProbe))
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(Args(src, MakePlan(src, nil), "in.mkv", "out.mkv.part", "The Iron Giant (1999)", 2), " ")
	for _, want := range []string{
		"-map 0:0 -map 0:2 -map 0:1 -map 0:3 -map 0:6 -map 0:4 -map 0:5 -map 0:7 -c copy",
		"-disposition:a:0 default -disposition:a:1 0",
		"-disposition:s:0 default -disposition:s:1 forced -disposition:s:2 0",
		"-metadata title=The Iron Giant (1999)",
		"-threads 2 -f matroska out.mkv.part",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "libx264") || strings.Contains(args, "0:8") {
		t.Errorf("remux should not encode or keep eia_608:\n%s", args)
	}
}

func TestArgsEncodeHDR(t *testing.T) {
	src := Source{Video: Video{Index: 0, Codec: "hevc", Width: 3840, Height: 2160, PixFmt: "yuv420p10le", ColorTransfer: "smpte2084"},
		Audio: []Track{{Index: 1, Language: "eng"}}}
	args := strings.Join(Args(src, MakePlan(src, nil), "in.mkv", "out", "Up", 0), " ")
	for _, want := range []string{"-c copy -c:v libx264", "tonemap=hable", "min(1920,iw)", "-color_trc bt709"} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "-threads") {
		t.Errorf("threads 0 should leave ffmpeg to decide:\n%s", args)
	}
}

func TestExternalSubtitles(t *testing.T) {
	dir := t.TempDir()
	movie := filepath.Join(dir, "Up (2009).mkv")
	for _, name := range []string{
		"Up (2009).mkv", "Up (2009).srt", "Up (2009).fr.srt", "Up (2009).en.forced.srt",
		"Up (2009).en.sdh.srt", "Up Again.srt", "Up (2009).jpg",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tracks, err := ExternalSubtitles(movie)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tr := range tracks {
		got = append(got, filepath.Base(tr.External)+"="+tr.Language+"/"+tr.Title)
	}
	want := []string{
		"Up (2009).en.forced.srt=eng/English (forced)",
		"Up (2009).en.sdh.srt=eng/English (SDH)",
		"Up (2009).fr.srt=fre/French",
		"Up (2009).srt=eng/English",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}

	// The plain English file is the default, ahead of the SDH one.
	p := MakePlan(Source{Video: Video{Codec: "h264", Width: 1280, Height: 720, PixFmt: "yuv420p"}}, tracks)
	if !p.DefaultSubtitle || filepath.Base(p.Subtitles[0].External) != "Up (2009).srt" || !p.Subtitles[1].Forced {
		t.Errorf("plan subtitles %+v", p.Subtitles)
	}
	args := strings.Join(Args(Source{}, p, movie, "out", "Up (2009)", 0), " ")
	if !strings.Contains(args, "-c:s:0 srt -metadata:s:s:0 language=eng -metadata:s:s:0 title=English -disposition:s:0 default") {
		t.Errorf("external subtitle args:\n%s", args)
	}
}

func TestArgsReadsOldSubtitleFilesAsWindows1252(t *testing.T) {
	dir := t.TempDir()
	srt := filepath.Join(dir, "Up.srt")
	if err := os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:02,000\nCaf\xe9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Plan{Subtitles: []Track{{Codec: "subrip", Language: "eng", External: srt}}}
	args := strings.Join(Args(Source{}, p, "Up.mkv", "out", "Up", 0), " ")
	if !strings.Contains(args, "-sub_charenc CP1252 -i "+srt) {
		t.Errorf("args:\n%s", args)
	}
}
