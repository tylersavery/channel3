// Package bumper draws a channel's card: a rounded panel in the channel's
// colour with its icon and name, shown in the lower left for a moment after
// tuning so a child who cannot read the guide can still tell where they are.
//
// Everything is drawn in Go, the SVG icon included, so the Pi needs nothing
// beyond the one static binary. The result is an ordinary image; putting it on
// screen, and the byte order mpv wants, are the player's business.
package bumper

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Proportions of the card, as fractions of the screen. They are fractions so
// the card is the same size on a 720p panel as on a 1080p one.
const (
	cardWidthOfScreen  = 0.42 // the card's width
	cardHeightOfScreen = 0.19 // the card's height
	marginOfScreen     = 0.05 // gap from the left and bottom edges, of the height
	paddingOfCard      = 0.14 // space inside the card, of its height
	cornerOfCard       = 0.22 // corner radius, of its height
	textOfCard         = 0.34 // largest text size, of its height
)

// Card is what one channel's bumper shows.
type Card struct {
	// Name is the channel's name, drawn to the right of the icon.
	Name string
	// Icon is the SVG source of the icon, or nil for a card with the name
	// alone.
	Icon []byte
	// Color is the panel's background.
	Color color.RGBA
}

// Placed is a rendered card and where its top left corner goes on a screen of
// the size it was rendered for.
type Placed struct {
	Image *image.RGBA
	X, Y  int
}

// Render draws card for a screen of the given size.
//
// An icon that does not parse is an error rather than a card without a
// picture, so the caller decides whether to show the name alone and can say
// why in the log.
func Render(card Card, screenWidth, screenHeight int) (Placed, error) {
	if screenWidth <= 0 || screenHeight <= 0 {
		return Placed{}, fmt.Errorf("bumper: screen size %dx%d", screenWidth, screenHeight)
	}
	w := int(math.Round(float64(screenWidth) * cardWidthOfScreen))
	h := int(math.Round(float64(screenHeight) * cardHeightOfScreen))
	margin := int(math.Round(float64(screenHeight) * marginOfScreen))
	pad := int(math.Round(float64(h) * paddingOfCard))

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRoundedRect(img, card.Color, float64(h)*cornerOfCard)

	textLeft := pad
	if len(card.Icon) > 0 {
		side := h - 2*pad
		if err := drawIcon(img, card.Icon, pad, pad, side); err != nil {
			return Placed{}, err
		}
		textLeft = pad + side + pad
	}
	if err := drawName(img, card.Name, textLeft, w-pad, h, inkFor(card.Color)); err != nil {
		return Placed{}, err
	}

	return Placed{Image: img, X: margin, Y: screenHeight - h - margin}, nil
}

// fillRoundedRect paints the whole of img in c with rounded corners of radius
// r, anti-aliased by drawing the outline through the same rasteriser the icons
// use.
func fillRoundedRect(img *image.RGBA, c color.RGBA, r float64) {
	b := img.Bounds()
	scanner := rasterx.NewScannerGV(b.Dx(), b.Dy(), img, b)
	filler := rasterx.NewFiller(b.Dx(), b.Dy(), scanner)
	filler.SetColor(c)
	rasterx.AddRoundRect(0, 0, float64(b.Dx()), float64(b.Dy()), r, r, 0, rasterx.RoundGap, filler)
	filler.Draw()
}

// drawIcon renders the SVG source into the side by side square at x, y.
func drawIcon(img *image.RGBA, svg []byte, x, y, side int) error {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svg), oksvg.WarnErrorMode)
	if err != nil {
		return fmt.Errorf("bumper: icon: %w", err)
	}
	icon.SetTarget(float64(x), float64(y), float64(side), float64(side))
	b := img.Bounds()
	scanner := rasterx.NewScannerGV(b.Dx(), b.Dy(), img, b)
	icon.Draw(rasterx.NewDasher(b.Dx(), b.Dy(), scanner), 1)
	return nil
}

// drawName writes name between left and right, centred on the card's height,
// at the largest size up to the cap that fits the width.
func drawName(img *image.RGBA, name string, left, right, cardHeight int, ink color.Color) error {
	parsed, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return fmt.Errorf("bumper: font: %w", err)
	}
	room := right - left
	if room <= 0 || name == "" {
		return nil
	}

	size := float64(cardHeight) * textOfCard
	var face font.Face
	for {
		face, err = opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return fmt.Errorf("bumper: font face: %w", err)
		}
		if font.MeasureString(face, name).Ceil() <= room || size <= 8 {
			break
		}
		face.Close()
		size *= 0.92
	}
	defer face.Close()

	metrics := face.Metrics()
	textHeight := (metrics.Ascent + metrics.Descent).Ceil()
	baseline := (cardHeight-textHeight)/2 + metrics.Ascent.Ceil()
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(ink),
		Face: face,
		Dot:  fixed.P(left, baseline),
	}
	d.DrawString(name)
	return nil
}

// inkFor picks white or near black text, whichever reads better on bg, using
// the WCAG relative luminance.
func inkFor(bg color.RGBA) color.Color {
	lum := 0.2126*linear(bg.R) + 0.7152*linear(bg.G) + 0.0722*linear(bg.B)
	if lum > 0.4 {
		return color.RGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}
	}
	return color.White
}

// linear converts one sRGB channel to linear light.
func linear(c uint8) float64 {
	v := float64(c) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}
