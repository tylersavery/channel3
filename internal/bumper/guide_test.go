package bumper

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"golang.org/x/image/font/basicfont"
)

func guideFixture() GuidePage {
	return GuidePage{
		Clock: "10:42 AM",
		Rows: []GuideRow{
			{Number: 3, Name: "FarmTV", Icon: []byte(testIcon), Color: green, Now: "Combine Time", NowUntil: "11:15 AM", Next: "Big Trains", NextAt: "11:15"},
			{Number: 4, Name: "Diggers TV", Color: green, Now: strings.Repeat("A very long title ", 20), Highlight: true},
			{Number: 22, Name: "Rock & Roll"},
		},
		Music: "Clair de Lune · Debussy", Page: 1, Pages: 3,
	}
}

func TestGuideFillsTheScreen(t *testing.T) {
	for _, screen := range []image.Point{{1920, 1080}, {1366, 768}} {
		img, err := RenderGuide(guideFixture(), screen.X, screen.Y)
		if err != nil {
			t.Fatalf("%v: %v", screen, err)
		}
		if got := img.Bounds().Size(); got != screen {
			t.Errorf("guide is %v for a %v screen", got, screen)
		}
		if _, _, _, a := img.At(screen.X/2, screen.Y/2).RGBA(); a != 0xffff {
			t.Errorf("%v: the guide has a see-through middle, so the picture behind would show", screen)
		}
	}
}

// TestGuideHighlightsTheChannelYouCameFrom checks the highlighted row's band
// is drawn, in the highlight colour, and the others are not.
func TestGuideHighlightsTheChannelYouCameFrom(t *testing.T) {
	img, err := RenderGuide(guideFixture(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	h := 1080.0
	headerH := int(h * 0.11)
	rowsTop := headerH + int(h*0.015)
	rowH := (1080 - rowsTop - int(h*0.09) - int(h*0.015)) / GuideRowsPerPage
	x := 1920 - 1920*4/100 + 4
	probe := func(row int) color.RGBA { return img.RGBAAt(x, rowsTop+row*rowH+rowH/2) }
	if got := probe(1); got != guideHighlight {
		t.Errorf("the highlighted row's edge is %v, want the highlight colour", got)
	}
	if got := probe(0); got != guideBackground {
		t.Errorf("an ordinary row's edge is %v, want the background", got)
	}
}

func TestEllipsizeFits(t *testing.T) {
	face := basicfont.Face7x13
	if got := ellipsize(face, "Short", 200); got != "Short" {
		t.Errorf("a short title was changed to %q", got)
	}
	got := ellipsize(face, strings.Repeat("x", 100), 70)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) > 10 {
		t.Errorf("ellipsize gave %q, want at most ten characters ending in an ellipsis", got)
	}
	if got := ellipsize(face, "anything", 0); got != "" {
		t.Errorf("no room gave %q, want nothing", got)
	}
}

func TestGuideRejectsAZeroScreen(t *testing.T) {
	if _, err := RenderGuide(guideFixture(), 0, 0); err == nil {
		t.Error("a 0x0 guide rendered")
	}
}
