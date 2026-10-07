package bumper

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// Movie Mode's screens share the guide's look: the night background, the
// retro green and the same fonts, so the two feel like one television.

// MenuMovie is one film on the menu.
type MenuMovie struct {
	Number int
	Title  string
	// Poster is the film's picture, or nil for a plain tile with the title.
	Poster image.Image
}

// MenuPage is one screen of the movie menu.
type MenuPage struct {
	Movies      []MenuMovie
	Page, Pages int
	// Typed is a half typed movie number, shown until it is complete.
	Typed string
}

// MenuColumns and MenuRows are how many posters fit on one page.
const (
	MenuColumns   = 5
	MenuRows      = 2
	MoviesPerPage = MenuColumns * MenuRows
)

var (
	posterTile = color.RGBA{0x24, 0x31, 0x5C, 0xff}
)

// screen is a blank Movie Mode screen with its header and footer, and the
// faces to draw on it.
type screen struct {
	img             *image.RGBA
	faces           *faceCache
	w, h            float64
	margin          int
	headerH, footer int
}

func newScreen(title, hint, right string, width, height int) (*screen, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("movies: screen size %dx%d", width, height)
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("movies: font: %w", err)
	}
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("movies: font: %w", err)
	}
	s := &screen{
		img:   image.NewRGBA(image.Rect(0, 0, width, height)),
		faces: newFaceCache(bold, regular),
		w:     float64(width), h: float64(height),
	}
	s.margin = int(s.w * 0.04)
	s.headerH = int(s.h * 0.11)
	s.footer = int(s.h * 0.09)
	draw.Draw(s.img, s.img.Bounds(), image.NewUniform(guideBackground), image.Point{}, draw.Src)

	draw.Draw(s.img, image.Rect(0, 0, width, s.headerH), image.NewUniform(guideBar), image.Point{}, draw.Src)
	head := s.faces.get(true, float64(s.headerH)*0.42)
	drawText(s.img, head, title, s.margin, baselineIn(head, 0, s.headerH), guideGreen)

	footTop := height - s.footer
	draw.Draw(s.img, image.Rect(0, footTop, width, height), image.NewUniform(guideBar), image.Point{}, draw.Src)
	foot := s.faces.get(false, float64(s.footer)*0.36)
	drawText(s.img, foot, hint, s.margin, baselineIn(foot, footTop, height), guideWhite)
	rightW := font.MeasureString(foot, right).Ceil()
	drawText(s.img, foot, right, width-s.margin-rightW, baselineIn(foot, footTop, height), guideDim)
	return s, nil
}

// centred writes s centred across the screen on baseline y.
func (s *screen) centred(face font.Face, text string, y int, c color.Color) {
	text = ellipsize(face, text, int(s.w)-2*s.margin)
	x := (int(s.w) - font.MeasureString(face, text).Ceil()) / 2
	drawText(s.img, face, text, x, y, c)
}

// RenderPIN draws the code screen with typed of length digits entered.
func RenderPIN(typed, length, width, height int) (*image.RGBA, error) {
	s, err := newScreen("MOVIE NIGHT", "Enter the code to watch a movie", "", width, height)
	if err != nil {
		return nil, err
	}
	defer s.faces.close()
	big := s.faces.get(true, s.h*0.07)
	s.centred(big, "Enter code", int(s.h*0.40), guideWhite)

	box := int(s.h * 0.12)
	gap := int(s.h * 0.03)
	total := length*box + (length-1)*gap
	x := (int(s.w) - total) / 2
	y := int(s.h * 0.50)
	dot := s.faces.get(true, float64(box)*0.6)
	for i := range length {
		tile := image.NewRGBA(image.Rect(0, 0, box, box))
		fillRoundedRect(tile, guideBar, float64(box)*0.18)
		bx := x + i*(box+gap)
		draw.Draw(s.img, image.Rect(bx, y, bx+box, y+box), tile, image.Point{}, draw.Over)
		if i < typed {
			mark := "●"
			mw := font.MeasureString(dot, mark).Ceil()
			drawText(s.img, dot, mark, bx+(box-mw)/2, baselineIn(dot, y, y+box), guideGreen)
		}
	}
	return s.img, nil
}

// RenderNotice draws a screen with a large headline and a few lines under
// it, which is the resume question, The End and an empty shelf.
func RenderNotice(title, headline string, lines []string, hint string, width, height int) (*image.RGBA, error) {
	s, err := newScreen(title, hint, "", width, height)
	if err != nil {
		return nil, err
	}
	defer s.faces.close()
	big := s.faces.get(true, s.h*0.09)
	s.centred(big, headline, int(s.h*0.42), guideGreen)
	body := s.faces.get(false, s.h*0.05)
	for i, line := range lines {
		s.centred(body, line, int(s.h*(0.56+0.08*float64(i))), guideWhite)
	}
	return s.img, nil
}

// RenderMenu draws one page of the movie menu: a grid of posters, each with
// its number, and the title under it.
func RenderMenu(page MenuPage, width, height int) (*image.RGBA, error) {
	right := ""
	if page.Pages > 1 {
		right = fmt.Sprintf("Ch ▲▼ more  ·  %d / %d", page.Page, page.Pages)
	}
	hint := "Press a movie's number  ·  0 to go back to TV"
	if page.Typed != "" {
		hint = "Movie " + page.Typed + "-"
	}
	s, err := newScreen("MOVIE NIGHT", hint, right, width, height)
	if err != nil {
		return nil, err
	}
	defer s.faces.close()

	top := s.headerH + int(s.h*0.04)
	bottom := height - s.footer - int(s.h*0.02)
	gapX := int(s.w * 0.025)
	cellW := (width - 2*s.margin - (MenuColumns-1)*gapX) / MenuColumns
	cellH := (bottom - top) / MenuRows
	titleH := int(float64(cellH) * 0.22)
	// Posters are 2:3, as long as that fits the cell.
	tileH := cellH - titleH - int(float64(cellH)*0.03)
	tileW := min(cellW, tileH*2/3)
	tileH = tileW * 3 / 2

	numFace := s.faces.get(true, float64(tileW)*0.20)
	titleFace := s.faces.get(true, float64(titleH)*0.30)
	plainFace := s.faces.get(true, float64(tileW)*0.13)

	for i, m := range page.Movies {
		if i >= MoviesPerPage {
			break
		}
		col, row := i%MenuColumns, i/MenuColumns
		cellX := s.margin + col*(cellW+gapX)
		cx := cellX + (cellW-tileW)/2
		cy := top + row*cellH

		tile := image.NewRGBA(image.Rect(0, 0, tileW, tileH))
		fillRoundedRect(tile, posterTile, float64(tileW)*0.06)
		if m.Poster != nil {
			cover(tile, m.Poster)
			roundCorners(tile, float64(tileW)*0.06)
		} else {
			// No picture at all: the title, large, on the tile.
			lines := wrap(plainFace, m.Title, tileW-tileW/6, 4)
			lineH := plainFace.Metrics().Height.Ceil()
			y := (tileH-lineH*len(lines))/2 + lineH*3/4
			for j, line := range lines {
				lw := font.MeasureString(plainFace, line).Ceil()
				drawText(tile, plainFace, line, (tileW-lw)/2, y+j*lineH, guideWhite)
			}
		}
		draw.Draw(s.img, image.Rect(cx, cy, cx+tileW, cy+tileH), tile, image.Point{}, draw.Over)

		// The number sits in a badge at the poster's top left.
		num := fmt.Sprint(m.Number)
		badgeH := numFace.Metrics().Height.Ceil() + tileW/20
		badgeW := max(font.MeasureString(numFace, num).Ceil()+tileW/8, badgeH)
		badge := image.NewRGBA(image.Rect(0, 0, badgeW, badgeH))
		fillRoundedRect(badge, guideBackground, float64(badgeH)*0.3)
		drawText(badge, numFace, num, (badgeW-font.MeasureString(numFace, num).Ceil())/2, baselineIn(numFace, 0, badgeH), guideGreen)
		bx, by := cx-tileW/24, cy-tileW/24
		draw.Draw(s.img, image.Rect(bx, by, bx+badgeW, by+badgeH), badge, image.Point{}, draw.Over)

		lineH := titleFace.Metrics().Height.Ceil()
		for j, line := range wrap(titleFace, m.Title, cellW, 2) {
			lx := cellX + (cellW-font.MeasureString(titleFace, line).Ceil())/2
			drawText(s.img, titleFace, line, lx, cy+tileH+lineH*(j+1), guideWhite)
		}
	}
	if len(page.Movies) == 0 {
		body := s.faces.get(false, s.h*0.05)
		s.centred(body, "No movies yet", int(s.h*0.5), guideDim)
	}
	return s.img, nil
}

// cover scales src to fill dst, cropping whatever overhangs evenly from both
// sides, so a wide frame fills a tall tile without being squashed.
func cover(dst *image.RGBA, src image.Image) {
	sb, db := src.Bounds(), dst.Bounds()
	if sb.Dx() == 0 || sb.Dy() == 0 {
		return
	}
	scale := max(float64(db.Dx())/float64(sb.Dx()), float64(db.Dy())/float64(sb.Dy()))
	cropW := int(float64(db.Dx()) / scale)
	cropH := int(float64(db.Dy()) / scale)
	x0 := sb.Min.X + (sb.Dx()-cropW)/2
	y0 := sb.Min.Y + (sb.Dy()-cropH)/2
	xdraw.ApproxBiLinear.Scale(dst, db, src, image.Rect(x0, y0, x0+cropW, y0+cropH), draw.Src, nil)
}

// roundCorners clears img's corners outside a radius r, so a poster drawn
// edge to edge keeps the tile's rounded shape.
func roundCorners(img *image.RGBA, r float64) {
	b := img.Bounds()
	ri := int(r)
	for y := range ri {
		for x := range ri {
			dx, dy := r-float64(x)-0.5, r-float64(y)-0.5
			if dx*dx+dy*dy <= r*r {
				continue
			}
			for _, p := range []image.Point{
				{b.Min.X + x, b.Min.Y + y}, {b.Max.X - 1 - x, b.Min.Y + y},
				{b.Min.X + x, b.Max.Y - 1 - y}, {b.Max.X - 1 - x, b.Max.Y - 1 - y},
			} {
				img.SetRGBA(p.X, p.Y, color.RGBA{})
			}
		}
	}
}

// wrap breaks text into at most maxLines lines no wider than width, the last
// one ellipsized if the text runs on.
func wrap(face font.Face, text string, width, maxLines int) []string {
	var lines []string
	line := ""
	words := strings.Fields(text)
	for i, w := range words {
		try := strings.TrimSpace(line + " " + w)
		if font.MeasureString(face, try).Ceil() <= width || line == "" {
			line = try
			continue
		}
		if len(lines) == maxLines-1 {
			line = strings.Join(append([]string{line}, words[i:]...), " ")
			break
		}
		lines = append(lines, line)
		line = w
	}
	if line != "" {
		lines = append(lines, ellipsize(face, line, width))
	}
	return lines
}
