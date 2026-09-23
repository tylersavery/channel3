// Command standbycard draws the Please Stand By card that mpv shows when a
// channel has nothing to play.
//
// The card is a build-time artefact: it is generated once, committed under
// internal/player/assets, and embedded in the binary. Run it with the Makefile
// target standby-card.
//
// The text is drawn from a small bitmap font defined in this file rather than
// from a system typeface. ffmpeg on this project's machines is built without
// drawtext and ImageMagick is not installed, and a real font would mean either
// a new module dependency or a font file whose licence has to travel with the
// repository. Fifteen characters of blocky capitals need neither.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// The card is rendered at broadcast resolution so mpv never scales it up.
const (
	cardWidth  = 1920
	cardHeight = 1080
	cardText   = "PLEASE STAND BY"
)

// glyphWidth and glyphHeight are the bitmap font's cell size, in font pixels.
const (
	glyphWidth  = 5
	glyphHeight = 7
)

// background and foreground are a dark card with off-white text, chosen to be
// dim enough for a dark room and far from the black a dead HDMI input shows.
var (
	background = color.RGBA{R: 0x10, G: 0x14, B: 0x1A, A: 0xFF}
	foreground = color.RGBA{R: 0xE8, G: 0xE8, B: 0xEA, A: 0xFF}
)

// font holds the characters the card needs and nothing else.
var font = map[rune][glyphHeight]string{
	'P': {
		"####.",
		"#...#",
		"#...#",
		"####.",
		"#....",
		"#....",
		"#....",
	},
	'L': {
		"#....",
		"#....",
		"#....",
		"#....",
		"#....",
		"#....",
		"#####",
	},
	'E': {
		"#####",
		"#....",
		"#....",
		"####.",
		"#....",
		"#....",
		"#####",
	},
	'A': {
		".###.",
		"#...#",
		"#...#",
		"#####",
		"#...#",
		"#...#",
		"#...#",
	},
	'S': {
		".####",
		"#....",
		"#....",
		".###.",
		"....#",
		"....#",
		"####.",
	},
	'T': {
		"#####",
		"..#..",
		"..#..",
		"..#..",
		"..#..",
		"..#..",
		"..#..",
	},
	'N': {
		"#...#",
		"##..#",
		"##..#",
		"#.#.#",
		"#..##",
		"#..##",
		"#...#",
	},
	'D': {
		"####.",
		"#...#",
		"#...#",
		"#...#",
		"#...#",
		"#...#",
		"####.",
	},
	'B': {
		"####.",
		"#...#",
		"#...#",
		"####.",
		"#...#",
		"#...#",
		"####.",
	},
	'Y': {
		"#...#",
		"#...#",
		".#.#.",
		"..#..",
		"..#..",
		"..#..",
		"..#..",
	},
	' ': {
		".....",
		".....",
		".....",
		".....",
		".....",
		".....",
		".....",
	},
}

func main() {
	out := flag.String("o", "internal/player/assets/standby.png", "where to write the card")
	scale := flag.Int("scale", 14, "font pixels to image pixels")
	flag.Parse()

	if *scale < 1 {
		fmt.Fprintf(os.Stderr, "standbycard: --scale must be at least 1\n")
		os.Exit(1)
	}
	img, err := draw(cardText, *scale)
	if err != nil {
		fmt.Fprintf(os.Stderr, "standbycard: %v\n", err)
		os.Exit(1)
	}
	if err := write(*out, img); err != nil {
		fmt.Fprintf(os.Stderr, "standbycard: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *out)
}

// draw renders text centred on the card.
func draw(text string, scale int) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, cardWidth, cardHeight))
	for y := range cardHeight {
		for x := range cardWidth {
			img.Set(x, y, background)
		}
	}

	// One font pixel of tracking between glyphs keeps the capitals apart at
	// this weight without a kerning table.
	const tracking = 1
	cells := len(text)*(glyphWidth+tracking) - tracking
	width := cells * scale
	height := glyphHeight * scale
	if width > cardWidth {
		return nil, fmt.Errorf("%q at scale %d is %d pixels wide, wider than the %d pixel card", text, scale, width, cardWidth)
	}

	originX := (cardWidth - width) / 2
	originY := (cardHeight - height) / 2
	for i, ch := range text {
		glyph, ok := font[ch]
		if !ok {
			return nil, fmt.Errorf("no glyph for %q", ch)
		}
		left := originX + i*(glyphWidth+tracking)*scale
		if err := blit(img, glyph, left, originY, scale); err != nil {
			return nil, err
		}
	}
	return img, nil
}

// blit paints one glyph with its top left corner at left, top.
func blit(img *image.RGBA, glyph [glyphHeight]string, left, top, scale int) error {
	for row, line := range glyph {
		if len(line) != glyphWidth {
			return fmt.Errorf("glyph row %q is %d columns, want %d", line, len(line), glyphWidth)
		}
		for col, cell := range line {
			if cell != '#' {
				continue
			}
			for y := range scale {
				for x := range scale {
					img.Set(left+col*scale+x, top+row*scale+y, foreground)
				}
			}
		}
	}
	return nil
}

// write encodes the card as a PNG.
func write(path string, img *image.RGBA) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(file, img); err != nil {
		file.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
