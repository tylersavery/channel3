package shelf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // uploaded covers
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/proc"
)

// A cover is the 16:9 picture an item or folder shows on the menu. An item's
// starts as a frame of its video; either kind can be replaced with a picture
// sent from a phone.

// coverWidth is the widest a cover is kept. A tile on a 1080p menu is about
// 400 pixels wide, so this leaves room for a sharper screen.
const coverWidth = 960

// maxPictureSide is the largest picture accepted as a cover. A 48 megapixel
// phone photo is 8064 pixels across.
const maxPictureSide = 9000

// grabTimeout bounds one frame grab. A seek and one frame take a second or
// two on the Pi.
const grabTimeout = time.Minute

// ItemKey names an item's cover.
func ItemKey(r Ref) string { return r.Channel + "/" + r.ID }

// FolderKey names a folder's cover.
func FolderKey(id string) string { return "folder:" + id }

// ParseKey reads a cover key. Exactly one of ref and folder is set.
func ParseKey(key string) (ref Ref, folder string, err error) {
	if id, ok := strings.CutPrefix(key, "folder:"); ok {
		if !validFolderID(id) {
			return Ref{}, "", fmt.Errorf("%q is not a folder", key)
		}
		return Ref{}, id, nil
	}
	channel, id, ok := strings.Cut(key, "/")
	if !ok || !validChannelID(channel) || !library.ValidItemID(id) {
		return Ref{}, "", fmt.Errorf("%q is not a cover key: use channel/id or folder:id", key)
	}
	return Ref{Channel: channel, ID: id}, "", nil
}

// CoversDir is where covers live under root.
func CoversDir(root string) string { return filepath.Join(root, "shelf-covers") }

// Covers keeps the cover pictures.
type Covers struct {
	Dir string
	// Grab writes one frame of video at at to out as a JPEG. Nil uses ffmpeg
	// from PATH at the lowest priority.
	Grab func(ctx context.Context, video, out string, at time.Duration) error
}

// File is where key's cover is kept, whether or not it exists yet. Channel
// ids never hold an underscore and never start with one, so the two kinds of
// name cannot collide.
func (c Covers) File(key string) (string, error) {
	ref, folder, err := ParseKey(key)
	if err != nil {
		return "", err
	}
	if folder != "" {
		return filepath.Join(c.Dir, "_folder_"+folder+".jpg"), nil
	}
	return filepath.Join(c.Dir, ref.Channel+"_"+ref.ID+".jpg"), nil
}

// Has reports whether key has a cover.
func (c Covers) Has(key string) bool {
	path, err := c.File(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// FromFrame makes key's cover from the frame of video at at.
func (c Covers) FromFrame(ctx context.Context, key, video string, at time.Duration) error {
	path, err := c.File(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	grab := c.Grab
	if grab == nil {
		grab = ffmpegFrame
	}
	ctx, cancel := context.WithTimeout(ctx, grabTimeout)
	defer cancel()
	tmp := path + ".tmp.jpg"
	if err := grab(ctx, video, tmp, max(at, 0)); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("grab a frame of %s: %w", filepath.Base(video), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// FromPicture makes key's cover from an uploaded JPEG or PNG, scaled down to
// coverWidth and kept as a JPEG whatever it came as.
func (c Covers) FromPicture(key string, r io.Reader) error {
	path, err := c.File(key)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	// The size is checked before the pixels are decoded, so a small file
	// claiming to be enormous cannot take the box's memory.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("the picture is not a JPEG or PNG: %w", err)
	}
	if cfg.Width > maxPictureSide || cfg.Height > maxPictureSide {
		return fmt.Errorf("the picture is %dx%d; send one under %d pixels a side", cfg.Width, cfg.Height, maxPictureSide)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("the picture is not a JPEG or PNG: %w", err)
	}
	b := src.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return errors.New("the picture is empty")
	}
	w, h := b.Dx(), b.Dy()
	if w > coverWidth {
		w, h = coverWidth, max(b.Dy()*coverWidth/b.Dx(), 1)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)

	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp.jpg"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = jpeg.Encode(f, dst, &jpeg.Options{Quality: 85})
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Load reads key's cover for drawing, or nil when it has none.
func (c Covers) Load(key string) (image.Image, error) {
	path, err := c.File(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// DefaultFrame is where an item's first cover is taken from: a quarter of the
// way in, past the title card and into the show.
func DefaultFrame(length time.Duration) time.Duration { return length / 4 }

// ffmpegFrame grabs one frame with ffmpeg at the lowest priority, so a cover
// made while the television plays never costs it a frame.
func ffmpegFrame(ctx context.Context, video, out string, at time.Duration) error {
	args := []string{"-n", "19", "ffmpeg", "-hide_banner", "-v", "error", "-nostdin", "-y",
		"-ss", strconv.FormatFloat(at.Seconds(), 'f', 1, 64), "-i", video, "-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", coverWidth), "-q:v", "3", "-update", "1", out}
	cmd := exec.CommandContext(ctx, "nice", args...)
	proc.Harden(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(out); err != nil {
		// ffmpeg exits cleanly with no frame when the seek is past the end.
		return errors.New("ffmpeg wrote no frame there")
	}
	return nil
}
