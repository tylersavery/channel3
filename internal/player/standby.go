package player

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// standbyCard is the Please Stand By image, drawn by tools/standbycard and
// regenerated with the Makefile target standby-card.
//
// It is embedded so the binary is the only thing that has to reach the Pi, and
// written out at startup because mpv loads a path rather than bytes.
//
//go:embed assets/standby.png
var standbyCard []byte

// StandbyCardName is the file the card is written to inside the runtime dir.
const StandbyCardName = "standby.png"

// WriteStandbyCard writes the embedded card into dir and returns its path.
//
// This is one of the two files the service writes, the other being the pid file
// serve keeps. Nothing about playback is written anywhere.
func WriteStandbyCard(dir string) (string, error) {
	path := filepath.Join(dir, StandbyCardName)
	if err := os.WriteFile(path, standbyCard, 0o644); err != nil {
		return "", fmt.Errorf("player: write the standby card to %s: %w", path, err)
	}
	return path, nil
}
