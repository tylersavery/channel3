package movie

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Source is what ffprobe says about a dropped movie file.
type Source struct {
	Duration  float64 // seconds
	Title     string  // the container's title tag, which MakeMKV fills
	Video     Video
	Audio     []Track
	Subtitles []Track
	// Attachments are fonts that styled (ASS) subtitles draw with.
	Attachments int
}

// Video is the movie's first video stream.
type Video struct {
	Index         int
	Codec         string
	Width, Height int
	PixFmt        string
	ColorTransfer string
	FieldOrder    string
}

// HDR reports PQ or HLG video, which needs tone mapping for an SDR screen.
func (v Video) HDR() bool {
	return v.ColorTransfer == "smpte2084" || v.ColorTransfer == "arib-std-b67"
}

// Interlaced reports video stored as fields, as DVD video often is. mpv
// deinterlaces it on playback.
func (v Video) Interlaced() bool {
	switch v.FieldOrder {
	case "", "progressive", "unknown":
		return false
	}
	return true
}

// Track is one audio or subtitle stream.
type Track struct {
	// Index is the stream's index in its file.
	Index    int    `json:"-"`
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"` // ISO 639-2, such as "eng"
	Title    string `json:"title,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	// Comment marks a commentary track, from its disposition or its title.
	Comment bool `json:"commentary,omitempty"`
	// External names the .srt file beside the movie this track came from.
	External string `json:"-"`
}

// English reports a track marked as English.
func (t Track) English() bool {
	switch strings.ToLower(t.Language) {
	case "eng", "en", "en-us", "en-gb":
		return true
	}
	return false
}

// Picture reports a subtitle track drawn as images, as DVD and Blu-ray
// subtitles are, rather than as text.
func (t Track) Picture() bool {
	switch t.Codec {
	case "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle":
		return true
	}
	return false
}

// probeOutput is the part of ffprobe's JSON that Prepare reads.
type probeOutput struct {
	Streams []struct {
		Index         int    `json:"index"`
		CodecType     string `json:"codec_type"`
		CodecName     string `json:"codec_name"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		PixFmt        string `json:"pix_fmt"`
		ColorTransfer string `json:"color_transfer"`
		FieldOrder    string `json:"field_order"`
		Channels      int    `json:"channels"`
		Disposition   struct {
			Default     int `json:"default"`
			Forced      int `json:"forced"`
			Comment     int `json:"comment"`
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		Tags struct {
			Language string `json:"language"`
			Title    string `json:"title"`
		} `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		Tags     struct {
			Title string `json:"title"`
		} `json:"tags"`
	} `json:"format"`
}

// parseProbe turns ffprobe's JSON into a Source.
func parseProbe(data []byte) (Source, error) {
	var p probeOutput
	if err := json.Unmarshal(data, &p); err != nil {
		return Source{}, fmt.Errorf("read ffprobe output: %w", err)
	}
	var src Source
	found := false
	for _, s := range p.Streams {
		track := Track{
			Index:    s.Index,
			Codec:    s.CodecName,
			Language: s.Tags.Language,
			Title:    s.Tags.Title,
			Channels: s.Channels,
			Default:  s.Disposition.Default == 1,
			Forced:   s.Disposition.Forced == 1,
			Comment:  s.Disposition.Comment == 1 || strings.Contains(strings.ToLower(s.Tags.Title), "commentary"),
		}
		switch s.CodecType {
		case "video":
			// Cover art in a container is a video stream with one picture.
			if found || s.Disposition.AttachedPic == 1 {
				continue
			}
			found = true
			src.Video = Video{
				Index: s.Index, Codec: s.CodecName,
				Width: s.Width, Height: s.Height,
				PixFmt: s.PixFmt, ColorTransfer: s.ColorTransfer, FieldOrder: s.FieldOrder,
			}
		case "audio":
			src.Audio = append(src.Audio, track)
		case "subtitle":
			src.Subtitles = append(src.Subtitles, track)
		case "attachment":
			src.Attachments++
		}
	}
	if !found || src.Video.Width <= 0 || src.Video.Height <= 0 {
		return Source{}, errors.New("no video stream")
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		src.Duration = d
	}
	src.Title = p.Format.Tags.Title
	return src, nil
}
