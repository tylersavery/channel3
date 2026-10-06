package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeRootFile writes body to rel under root, creating directories.
func writeRootFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCardsReadsIconsAndSkipsChannelsWithout(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "channels/farm.yaml", "id: farm\nnumber: 3\nname: FarmTV\nsources: []\nbumper:\n  icon: tractor.svg\n  color: \"#367C2B\"\n")
	writeRootFile(t, root, "channels/plain.yaml", "id: plain\nnumber: 4\nname: Plain\nsources: []\n")
	writeRootFile(t, root, "channels/lost.yaml", "id: lost\nnumber: 5\nname: Lost\nsources: []\nbumper:\n  icon: missing.svg\n  color: \"#000000\"\n")
	writeRootFile(t, root, "icons/tractor.svg", "<svg/>")

	cards := loadCards(root)

	if farm, ok := cards["farm"]; !ok || farm.Name != "FarmTV" || string(farm.Icon) != "<svg/>" {
		t.Errorf("farm card = %+v, %v, want FarmTV with the tractor icon", farm, ok)
	}
	if _, ok := cards["plain"]; ok {
		t.Error("a channel with no bumper block got a card")
	}
	if lost, ok := cards["lost"]; !ok || lost.Icon != nil {
		t.Errorf("lost card = %+v, %v, want a card with its name alone when the icon is missing", lost, ok)
	}
}

func TestLoadCardsWithBrokenConfigIsNoCards(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "channels/bad.yaml", "id: Bad\n")
	if cards := loadCards(root); cards != nil {
		t.Errorf("broken config gave cards %v, want none", cards)
	}
}

// TestPrepareCommandFillsHomeAndKeepsOriginals runs channel3 prepare on a
// folder holding a portrait clip and a file that is not a video. The clip
// lands in local/home, its original moves to local/home-originals, and the
// other file is left alone.
func TestPrepareCommandFillsHomeAndKeepsOriginals(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	root := t.TempDir()
	inbox := filepath.Join(t.TempDir(), "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	clip := filepath.Join(inbox, "IMG_0001.mov")
	gen := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=360x640:rate=30:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", clip)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot make a test clip: %v: %s", err, out)
	}
	writeRootFile(t, inbox, "notes.txt", "not a video")

	if code := run([]string{"--root", root, "prepare", inbox}); code != 0 {
		t.Fatalf("channel3 prepare exited %d", code)
	}

	prepared, _ := filepath.Glob(filepath.Join(root, "local", "home", "*.mp4"))
	if len(prepared) != 1 {
		t.Errorf("local/home holds %v, want one prepared clip", prepared)
	}
	if _, err := os.Stat(filepath.Join(root, "local", "home-originals", "IMG_0001.mov")); err != nil {
		t.Errorf("the original was not kept in local/home-originals: %v", err)
	}
	if _, err := os.Stat(clip); !os.IsNotExist(err) {
		t.Error("the original is still in the inbox")
	}
	if _, err := os.Stat(filepath.Join(inbox, "notes.txt")); err != nil {
		t.Errorf("a file that is not a video was touched: %v", err)
	}
}
