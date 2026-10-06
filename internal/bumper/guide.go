package bumper

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// GuideRow is one channel on the guide.
type GuideRow struct {
	Number int
	Name   string
	// Icon and Color are the channel's card; a channel without a card gets a
	// plain grey tile with no icon.
	Icon  []byte
	Color color.RGBA
	// Now is what is on, and NowUntil when it ends, already formatted. Next
	// and NextAt are what follows. Any of them may be empty.
	Now, NowUntil string
	Next, NextAt  string
	// Highlight marks the channel the viewer came from.
	Highlight bool
}

// GuidePage is one screen of the guide.
type GuidePage struct {
	Clock       string
	Rows        []GuideRow
	Music       string // the song playing behind the guide, or ""
	Page, Pages int
}

// GuideRowsPerPage is how many channels fit on one page.
const GuideRowsPerPage = 7

// Guide colours: a deep night background, the retro green of the channel
// number, and quieter tones for what comes next.
var (
	guideBackground = color.RGBA{0x0B, 0x10, 0x20, 0xff}
	guideBar        = color.RGBA{0x14, 0x1C, 0x36, 0xff}
	guideHighlight  = color.RGBA{0x24, 0x31, 0x5C, 0xff}
	guideGreen      = color.RGBA{0x33, 0xFF, 0x33, 0xff}
	guideWhite      = color.RGBA{0xF2, 0xF2, 0xF2, 0xff}
	guideDim        = color.RGBA{0x9A, 0xA4, 0xBF, 0xff}
	guideNoCard     = color.RGBA{0x4A, 0x50, 0x60, 0xff}
)

// RenderGuide draws one page of the guide for a screen of the given size. The
// layout is in fractions of the screen, so it is the same on the 720p
// television as on a 1080p one.
func RenderGuide(page GuidePage, width, height int) (*image.RGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("guide: screen size %dx%d", width, height)
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("guide: font: %w", err)
	}
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("guide: font: %w", err)
	}
	faces := newFaceCache(bold, regular)
	defer faces.close()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(guideBackground), image.Point{}, draw.Src)

	w, h := float64(width), float64(height)
	margin := int(w * 0.04)
	headerH := int(h * 0.11)
	footerH := int(h * 0.09)
	rowsTop := headerH + int(h*0.015)
	rowH := (height - rowsTop - footerH - int(h*0.015)) / GuideRowsPerPage

	// Header.
	draw.Draw(img, image.Rect(0, 0, width, headerH), image.NewUniform(guideBar), image.Point{}, draw.Src)
	title := faces.get(true, float64(headerH)*0.42)
	drawText(img, title, "CHANNEL THREE GUIDE", margin, baselineIn(title, 0, headerH), guideGreen)
	clockW := font.MeasureString(title, page.Clock).Ceil()
	drawText(img, title, page.Clock, width-margin-clockW, baselineIn(title, 0, headerH), guideWhite)

	// Rows.
	numberW := int(w * 0.07)
	tile := int(float64(rowH) * 0.78)
	nameW := int(w * 0.20)
	for i, row := range page.Rows {
		top := rowsTop + i*rowH
		if row.Highlight {
			// Drawn on its own image and pasted in, because fillRoundedRect
			// draws from the origin of the image it is given.
			band := image.NewRGBA(image.Rect(0, 0, width-margin, rowH-int(float64(rowH)*0.06)))
			fillRoundedRect(band, guideHighlight, float64(rowH)*0.18)
			draw.Draw(img, band.Bounds().Add(image.Pt(margin/2, top)), band, image.Point{}, draw.Over)
		}
		mid := top + rowH/2

		numFace := faces.get(true, float64(rowH)*0.42)
		num := fmt.Sprint(row.Number)
		numX := margin + numberW - font.MeasureString(numFace, num).Ceil() - int(w*0.012)
		drawText(img, numFace, num, numX, baselineIn(numFace, top, top+rowH), guideGreen)

		tileX := margin + numberW
		tileY := mid - tile/2
		tileImg := image.NewRGBA(image.Rect(0, 0, tile, tile))
		bg := row.Color
		if bg.A == 0 {
			bg = guideNoCard
		}
		fillRoundedRect(tileImg, bg, float64(tile)*0.22)
		if len(row.Icon) > 0 {
			pad := int(float64(tile) * 0.14)
			// A card's icon that will not draw leaves the tile plain; the
			// guide is no place to fail over one picture.
			_ = drawIcon(tileImg, row.Icon, pad, pad, tile-2*pad)
		}
		draw.Draw(img, image.Rect(tileX, tileY, tileX+tile, tileY+tile), tileImg, image.Point{}, draw.Over)

		nameX := tileX + tile + int(w*0.015)
		nameFace := faces.get(true, float64(rowH)*0.30)
		drawText(img, nameFace, ellipsize(nameFace, row.Name, nameW), nameX, mid-int(float64(rowH)*0.04), guideWhite)

		infoX := nameX + nameW + int(w*0.015)
		infoW := width - margin - infoX
		nowFace := faces.get(true, float64(rowH)*0.27)
		nextFace := faces.get(false, float64(rowH)*0.22)
		now := row.Now
		if now == "" {
			now = "Please Stand By"
		}
		until := ""
		if row.NowUntil != "" {
			until = "  until " + row.NowUntil
		}
		untilW := font.MeasureString(nextFace, until).Ceil()
		drawText(img, nowFace, ellipsize(nowFace, now, infoW-untilW), infoX, mid-int(float64(rowH)*0.04), guideWhite)
		nowEnd := infoX + min(font.MeasureString(nowFace, ellipsize(nowFace, now, infoW-untilW)).Ceil(), infoW-untilW)
		drawText(img, nextFace, until, nowEnd, mid-int(float64(rowH)*0.04), guideDim)
		if row.Next != "" {
			next := "Next " + row.NextAt + "  " + row.Next
			drawText(img, nextFace, ellipsize(nextFace, next, infoW), infoX, mid+int(float64(rowH)*0.30), guideDim)
		}
	}

	// Footer.
	footTop := height - footerH
	draw.Draw(img, image.Rect(0, footTop, width, height), image.NewUniform(guideBar), image.Point{}, draw.Src)
	foot := faces.get(false, float64(footerH)*0.36)
	hint := "Press a channel number to watch"
	drawText(img, foot, hint, margin, baselineIn(foot, footTop, height), guideWhite)
	right := ""
	if page.Pages > 1 {
		right = fmt.Sprintf("%d / %d", page.Page, page.Pages)
	}
	rightW := font.MeasureString(foot, right).Ceil()
	drawText(img, foot, right, width-margin-rightW, baselineIn(foot, footTop, height), guideDim)
	if page.Music != "" {
		musicX := margin + font.MeasureString(foot, hint).Ceil() + int(w*0.05)
		musicW := width - margin - rightW - int(w*0.03) - musicX
		drawText(img, foot, ellipsize(foot, "♪ "+page.Music, musicW), musicX, baselineIn(foot, footTop, height), guideDim)
	}
	return img, nil
}

// faceCache keeps one face per weight and size for a render.
type faceCache struct {
	bold, regular *opentype.Font
	faces         map[string]font.Face
}

func newFaceCache(bold, regular *opentype.Font) *faceCache {
	return &faceCache{bold: bold, regular: regular, faces: make(map[string]font.Face)}
}

// get returns a face of the weight and size.
func (c *faceCache) get(isBold bool, size float64) font.Face {
	key := fmt.Sprintf("%v/%.0f", isBold, size)
	if f, ok := c.faces[key]; ok {
		return f
	}
	src := c.regular
	if isBold {
		src = c.bold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: math.Max(size, 6), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		// Only a broken built-in font gets here. The bitmap face is small but
		// draws, which beats a guide with no words on it.
		return basicfont.Face7x13
	}
	c.faces[key] = f
	return f
}

func (c *faceCache) close() {
	for _, f := range c.faces {
		f.Close()
	}
}

// baselineIn is the baseline that centres face's capitals between top and
// bottom.
func baselineIn(face font.Face, top, bottom int) int {
	m := face.Metrics()
	return (top+bottom)/2 + (m.Ascent.Ceil()-m.Descent.Ceil())/2
}

// drawText writes s at x on baseline y.
func drawText(img *image.RGBA, face font.Face, s string, x, y int, c color.Color) {
	if s == "" {
		return
	}
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// ellipsize shortens s with an ellipsis until it fits maxWidth.
func ellipsize(face font.Face, s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if font.MeasureString(face, s).Ceil() <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if font.MeasureString(face, candidate).Ceil() <= maxWidth {
			return candidate
		}
	}
	return ""
}
