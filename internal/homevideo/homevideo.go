// Package homevideo turns clips from a phone into something the Pi plays well
// and a television shows well.
//
// A phone clip is usually HEVC, often HDR, sometimes 4K, and as often as not
// portrait. Prepare converts each one once, when it is added: H.264 at 1080p in
// standard dynamic range, with the phone's rotation applied. A portrait clip is
// placed in the middle of a 16:9 frame, and the sides are filled with the
// colour of the clip's own edges, averaged over a few seconds so the sides
// drift rather than flicker. Nothing is done while the channel plays.
package homevideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tylersavery/channel3/internal/proc"
)

// The frame every prepared clip is made to fit.
const (
	frameWidth  = 1920
	frameHeight = 1080
)

// The look of a portrait clip's sides.
const (
	// edgeStrip is how many pixels at each side of the clip the side colour
	// is taken from.
	edgeStrip = 24
	// sideBands is how many colour bands run down each side, so a sky over a
	// lawn gives blue above and green below rather than one muddy colour.
	sideBands = 6
	// sideSmoothing is how long the side colour averages over. Longer is
	// calmer, shorter follows a cut sooner.
	sideSmoothing = 3 * time.Second
	// sideBlur softens the bands into one gradient.
	sideBlur = 60
	// sideBrightness and sideSaturation keep the sides from competing with the
	// clip itself.
	sideBrightness = -0.06
	sideSaturation = 0.9
)

// Clip is what ffprobe says about a source clip.
type Clip struct {
	// Width and Height are as displayed, after the phone's rotation.
	Width, Height int
	// FPS is the video's frame rate.
	FPS float64
	// HDR says the video is PQ or HLG and needs tone mapping to SDR.
	HDR bool
	// HasAudio says there is an audio stream to keep.
	HasAudio bool
	// Recorded is when the phone recorded it, or zero when the file does not
	// say.
	Recorded time.Time
}

// Portrait reports whether the clip is taller than it is wide.
func (c Clip) Portrait() bool { return c.Height > c.Width }

// Tools is where ffmpeg and ffprobe are.
type Tools struct {
	FFmpeg  string
	FFprobe string
	// Threads caps ffmpeg's encoder threads. Zero lets ffmpeg decide, which is
	// right for a batch with nothing else running and wrong beside playback.
	Threads int
	// Nice runs ffmpeg at the lowest scheduling priority, so a conversion in
	// the background yields the CPU to mpv decoding the broadcast.
	Nice bool
}

// Probe reads the facts Prepare needs from path.
func (t Tools) Probe(ctx context.Context, path string) (Clip, error) {
	cmd := exec.CommandContext(ctx, t.FFprobe, "-v", "error", "-print_format", "json",
		"-show_streams", "-show_format", "--", path)
	proc.Harden(cmd)
	out, err := cmd.Output()
	if err != nil {
		return Clip{}, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), toolError(err))
	}
	return parseProbe(out)
}

// probeOutput is the part of ffprobe's JSON that Prepare reads.
type probeOutput struct {
	Streams []struct {
		CodecType     string `json:"codec_type"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		AvgFrameRate  string `json:"avg_frame_rate"`
		ColorTransfer string `json:"color_transfer"`
		Tags          struct {
			Rotate string `json:"rotate"`
		} `json:"tags"`
		SideData []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Tags struct {
			CreationTime string `json:"creation_time"`
			AppleDate    string `json:"com.apple.quicktime.creationdate"`
		} `json:"tags"`
	} `json:"format"`
}

// parseProbe turns ffprobe's JSON into a Clip.
func parseProbe(data []byte) (Clip, error) {
	var p probeOutput
	if err := json.Unmarshal(data, &p); err != nil {
		return Clip{}, fmt.Errorf("read ffprobe output: %w", err)
	}
	var clip Clip
	foundVideo := false
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if foundVideo {
				continue
			}
			foundVideo = true
			clip.Width, clip.Height = s.Width, s.Height
			if quarterTurn(rotation(s.Tags.Rotate, s.SideData)) {
				clip.Width, clip.Height = clip.Height, clip.Width
			}
			clip.FPS = frameRate(s.AvgFrameRate)
			clip.HDR = s.ColorTransfer == "smpte2084" || s.ColorTransfer == "arib-std-b67"
		case "audio":
			clip.HasAudio = true
		}
	}
	if !foundVideo || clip.Width <= 0 || clip.Height <= 0 {
		return Clip{}, errors.New("no video stream")
	}
	clip.Recorded = recorded(p.Format.Tags.AppleDate, p.Format.Tags.CreationTime)
	return clip, nil
}

// rotation is the display rotation in degrees, from the side data newer files
// carry or the rotate tag older ones do.
func rotation(tag string, sideData []struct {
	Rotation float64 `json:"rotation"`
}) float64 {
	for _, sd := range sideData {
		if sd.Rotation != 0 {
			return sd.Rotation
		}
	}
	if tag != "" {
		if v, err := strconv.ParseFloat(tag, 64); err == nil {
			return v
		}
	}
	return 0
}

// quarterTurn reports whether a rotation swaps width and height.
func quarterTurn(degrees float64) bool {
	r := math.Mod(math.Abs(math.Round(degrees)), 180)
	return r == 90
}

// frameRate reads ffprobe's fraction form, such as 30000/1001.
func frameRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return v
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return 0
	}
	return n / d
}

// recorded prefers the iPhone's own creation date, which carries the local
// time zone, over the container's UTC creation_time.
func recorded(apple, container string) time.Time {
	for _, v := range []string{apple, container} {
		if v == "" {
			continue
		}
		for _, layout := range []string{"2006-01-02T15:04:05-0700", time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// Filter returns the ffmpeg filter graph that turns clip into a prepared
// 1080p frame, ending in the label [v].
func Filter(clip Clip) string {
	var b strings.Builder
	b.WriteString("[0:v]")
	if clip.HDR {
		// Linearise, map BT.2020 to BT.709, tone map, and come back to
		// limited range 8 bit. Hable keeps faces natural on a phone clip.
		b.WriteString("zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709," +
			"tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,")
	}

	if !clip.Portrait() {
		fmt.Fprintf(&b, "scale=w=%d:h=%d:force_original_aspect_ratio=decrease:force_divisible_by=2,"+
			"setsar=1,format=yuv420p[v]", frameWidth, frameHeight)
		return b.String()
	}

	fgWidth := evenRound(float64(frameHeight) * float64(clip.Width) / float64(clip.Height))
	if fgWidth > frameWidth {
		fgWidth = frameWidth
	}
	left := evenRound(float64(frameWidth-fgWidth) / 2)
	right := frameWidth - fgWidth - left
	frames := smoothingFrames(clip.FPS)

	fmt.Fprintf(&b, "scale=%d:%d,setsar=1,split=3[fg][l][r];", fgWidth, frameHeight)
	b.WriteString(side("l", fmt.Sprintf("crop=%d:ih:0:0", edgeStrip), left, frames))
	b.WriteString(side("r", fmt.Sprintf("crop=%d:ih:iw-%d:0", edgeStrip, edgeStrip), right, frames))
	b.WriteString("[L][fg][R]hstack=3,format=yuv420p[v]")
	return b.String()
}

// side is the filter for one side: a strip of the clip's edge, squeezed to one
// column of bands, averaged over time, stretched to the side's width and
// blurred into a gradient.
func side(in, crop string, width, frames int) string {
	out := strings.ToUpper(in)
	return fmt.Sprintf("[%s]%s,scale=1:%d:flags=area,tmix=frames=%d,scale=%d:%d:flags=bicubic,"+
		"gblur=sigma=%d,eq=brightness=%.2f:saturation=%.2f,setsar=1[%s];",
		in, crop, sideBands, frames, width, frameHeight, sideBlur, sideBrightness, sideSaturation, out)
}

// smoothingFrames is sideSmoothing in frames at fps, within what tmix allows.
func smoothingFrames(fps float64) int {
	if fps <= 0 || math.IsNaN(fps) {
		fps = 30
	}
	n := int(math.Round(fps * sideSmoothing.Seconds()))
	return min(max(n, 2), 1024)
}

// evenRound rounds to the nearest even number, which every yuv420p dimension
// has to be.
func evenRound(v float64) int {
	return int(math.Round(v/2)) * 2
}

// Args returns ffmpeg's arguments to prepare src into dst.
func (t Tools) Args(clip Clip, src, dst string) []string {
	args := []string{"-hide_banner", "-v", "error", "-nostdin", "-y",
		"-i", src,
		"-filter_complex", Filter(clip),
		"-map", "[v]",
	}
	if clip.HasAudio {
		args = append(args, "-map", "0:a:0", "-c:a", "aac", "-b:a", "160k", "-ac", "2")
	}
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "21", "-profile:v", "high",
		"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709",
		"-movflags", "+faststart",
		"-map_metadata", "0",
	)
	if t.Threads > 0 {
		args = append(args, "-threads", strconv.Itoa(t.Threads))
	}
	return append(args, "-f", "mp4", dst)
}

// Prepare converts src into dstDir and returns the prepared file's path.
//
// The output is named after when the clip was recorded, for example
// "2025-06-14 14.03.mp4", which is also its title in the guide; a clip with no
// recording time is named fallback, or after src when fallback is empty. It is written under a temporary name and
// renamed into place only once ffmpeg has finished, so a channel never sees
// half a clip. An existing output is never overwritten: a second clip from the
// same minute gets " 2" and so on.
func (t Tools) Prepare(ctx context.Context, src, dstDir, fallback string) (string, error) {
	clip, err := t.Probe(ctx, src)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dstDir, err)
	}
	dst, err := freeName(dstDir, baseName(clip, src, fallback))
	if err != nil {
		return "", err
	}
	tmp := dst + ".part"
	name, args := t.FFmpeg, t.Args(clip, src, tmp)
	if t.Nice {
		name, args = "nice", append([]string{"-n", "19", t.FFmpeg}, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	proc.Harden(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("ffmpeg %s: %w: %s", filepath.Base(src), err, strings.TrimSpace(firstLines(string(out), 6)))
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("place %s: %w", filepath.Base(dst), err)
	}
	return dst, nil
}

// baseName is the prepared file's name without its extension.
func baseName(clip Clip, src, fallback string) string {
	if !clip.Recorded.IsZero() {
		return clip.Recorded.Format("2006-01-02 15.04")
	}
	if fallback != "" {
		return strings.TrimSuffix(fallback, filepath.Ext(fallback))
	}
	return strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
}

// freeName returns dir/base.mp4, or dir/base N.mp4 for the first N that is not
// taken.
func freeName(dir, base string) (string, error) {
	for n := 1; n < 1000; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s %d", base, n)
		}
		path := filepath.Join(dir, name+".mp4")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("check %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("no free name for %s in %s", base, dir)
}

// VideoExts are the file extensions Prepare picks up from a folder.
var VideoExts = map[string]bool{".mov": true, ".mp4": true, ".m4v": true, ".mkv": true, ".3gp": true}

// IsVideo reports whether path has a video file extension.
func IsVideo(path string) bool {
	return VideoExts[strings.ToLower(filepath.Ext(path))]
}

// toolError adds a tool's stderr to its exit error.
func toolError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(firstLines(string(exit.Stderr), 4)))
	}
	return err
}

// firstLines keeps the start of a tool's output for an error message.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
