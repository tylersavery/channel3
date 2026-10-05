package bumper

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// testIcon is a plain red square, which is all a test needs to see an icon
// was drawn.
const testIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect x="0" y="0" width="10" height="10" fill="#FF0000"/></svg>`

var green = color.RGBA{R: 0x36, G: 0x7C, B: 0x2B, A: 0xff}

// TestCardScalesWithTheScreen is the same card on the Samsung's 720p panel and
// a 1080p one: the same share of the screen, in the lower left.
func TestCardScalesWithTheScreen(t *testing.T) {
	for _, screen := range []image.Point{{1920, 1080}, {1366, 768}} {
		p, err := Render(Card{Name: "FarmTV", Color: green}, screen.X, screen.Y)
		if err != nil {
			t.Fatalf("%v: render: %v", screen, err)
		}
		b := p.Image.Bounds()
		if got, want := float64(b.Dx())/float64(screen.X), cardWidthOfScreen; got < want-0.01 || got > want+0.01 {
			t.Errorf("%v: card is %.3f of the width, want %.2f", screen, got, want)
		}
		if p.X <= 0 || p.X > screen.X/10 {
			t.Errorf("%v: card starts at x=%d, want a small left margin", screen, p.X)
		}
		if bottom := p.Y + b.Dy(); bottom >= screen.Y || bottom < screen.Y*9/10 {
			t.Errorf("%v: card ends at y=%d, want just above the bottom edge", screen, bottom)
		}
	}
}

// TestCornersAreRoundAndTheMiddleIsTheColour checks the panel itself: clear in
// the very corner, the channel colour inside.
func TestCornersAreRoundAndTheMiddleIsTheColour(t *testing.T) {
	p, err := Render(Card{Color: green}, 1920, 1080)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if _, _, _, a := p.Image.At(0, 0).RGBA(); a != 0 {
		t.Errorf("the top left corner has alpha %d, want a transparent rounded corner", a)
	}
	b := p.Image.Bounds()
	if got := p.Image.RGBAAt(b.Dx()/2, b.Dy()/2); got != green {
		t.Errorf("the middle of a card with no name or icon is %v, want %v", got, green)
	}
}

func TestIconIsDrawnOnTheLeft(t *testing.T) {
	p, err := Render(Card{Color: green, Icon: []byte(testIcon)}, 1920, 1080)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	h := p.Image.Bounds().Dy()
	if got := p.Image.RGBAAt(h/2, h/2); got.R < 0xf0 || got.G > 0x10 {
		t.Errorf("the icon area is %v, want the icon's red", got)
	}
}

func TestUnreadableIconIsAnError(t *testing.T) {
	_, err := Render(Card{Name: "FarmTV", Color: green, Icon: []byte("<svg><path d=\"M 0 0 Q\"")}, 1920, 1080)
	if err == nil {
		t.Fatal("an unreadable icon rendered without error")
	}
	if !strings.Contains(err.Error(), "icon") {
		t.Errorf("error %q does not say it was the icon", err)
	}
}

// TestLongNamesShrinkToFit is a name too long for the card at full size. It
// must stay inside the card's padding rather than run off the edge.
func TestLongNamesShrinkToFit(t *testing.T) {
	p, err := Render(Card{Name: "The Very Long Channel Name For Testing", Color: green}, 1920, 1080)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	b := p.Image.Bounds()
	pad := int(float64(b.Dy()) * paddingOfCard)
	for x := b.Dx() - pad + 1; x < b.Dx()-int(float64(b.Dy())*cornerOfCard); x++ {
		for y := pad; y < b.Dy()-pad; y++ {
			if got := p.Image.RGBAAt(x, y); got != green {
				t.Fatalf("text reached x=%d in a card %d wide, past the right padding", x, b.Dx())
			}
		}
	}
}

func TestInkReadsOnTheBackground(t *testing.T) {
	cases := []struct {
		bg   color.RGBA
		dark bool
	}{
		{green, false},
		{color.RGBA{R: 0x6C, G: 0xB4, B: 0xE4, A: 0xff}, true},
		{color.RGBA{R: 0xFF, G: 0xDE, B: 0x00, A: 0xff}, true},
		{color.RGBA{R: 0x1A, G: 0x23, B: 0x7E, A: 0xff}, false},
	}
	for _, tc := range cases {
		r, _, _, _ := inkFor(tc.bg).RGBA()
		if gotDark := r < 0x8000; gotDark != tc.dark {
			t.Errorf("ink on %v is dark=%v, want dark=%v", tc.bg, gotDark, tc.dark)
		}
	}
}

func TestBGRASwapsRedAndBlue(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})
	if got := BGRA(img); string(got) != string([]byte{3, 2, 1, 4}) {
		t.Errorf("BGRA = %v, want [3 2 1 4]", got)
	}
}

func TestZeroScreenIsAnError(t *testing.T) {
	if _, err := Render(Card{Color: green}, 0, 1080); err == nil {
		t.Error("a zero width screen rendered without error")
	}
}
