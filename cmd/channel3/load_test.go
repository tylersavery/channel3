package main

import (
	"os"
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
