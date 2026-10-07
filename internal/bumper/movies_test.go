package bumper

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// savePreview writes img to $CHANNEL3_PREVIEW_DIR, when it is set, so a person
// can look at a screen without a television.
func savePreview(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := os.Getenv("CHANNEL3_PREVIEW_DIR")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func solid(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func TestRenderMovieScreens(t *testing.T) {
	poster := solid(600, 900, color.RGBA{0xC0, 0x40, 0x30, 0xff})
	frame := solid(1920, 1080, color.RGBA{0x30, 0x80, 0xC0, 0xff})
	page := MenuPage{Page: 1, Pages: 2, Movies: []MenuMovie{
		{Number: 1, Title: "The Iron Giant (1999)", Poster: poster},
		{Number: 2, Title: "My Neighbor Totoro (1988)", Poster: frame},
		{Number: 3, Title: "Ponyo"},
		{Number: 4, Title: "Willy Wonka and the Chocolate Factory (1971)", Poster: frame},
		{Number: 5, Title: "Up (2009)", Poster: poster},
		{Number: 6, Title: "Paddington 2 (2017)", Poster: poster},
		{Number: 12, Title: "Wall-E (2008)"},
	}}
	for _, size := range []image.Point{{1280, 720}, {1920, 1080}} {
		menu, err := RenderMenu(page, size.X, size.Y)
		if err != nil {
			t.Fatal(err)
		}
		if menu.Bounds().Size() != size {
			t.Errorf("menu is %v, want %v", menu.Bounds().Size(), size)
		}
		savePreview(t, "menu-"+itoa(size.Y), menu)
	}

	pin, err := RenderPIN(2, 4, 1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	savePreview(t, "pin", pin)

	notice, err := RenderNotice("MOVIE NIGHT", "Up (2009)", []string{"1  Resume from 0:42:10", "2  Start over"}, "0 for the menu", 1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	savePreview(t, "resume", notice)

	empty, err := RenderMenu(MenuPage{Page: 1, Pages: 1}, 1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	savePreview(t, "empty", empty)

	if _, err := RenderMenu(page, 0, 0); err == nil {
		t.Error("a zero screen should be an error")
	}
}

func TestWrap(t *testing.T) {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 20, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	lines := wrap(face, "Willy Wonka and the Chocolate Factory and Then Some More Words", 150, 3)
	if len(lines) != 3 {
		t.Fatalf("got %d lines: %q", len(lines), lines)
	}
	if last := []rune(lines[2]); last[len(last)-1] != '…' {
		t.Errorf("a title that runs on should end in an ellipsis: %q", lines)
	}
	if got := wrap(face, "Up", 150, 3); len(got) != 1 || got[0] != "Up" {
		t.Errorf("got %q", got)
	}
}

func itoa(n int) string {
	return string(rune('0'+n/1000%10)) + string(rune('0'+n/100%10)) + string(rune('0'+n/10%10)) + string(rune('0'+n%10))
}
