// Package settings reads the station's look and feel from <root>/settings.yaml:
// which on-screen pieces are shown and for how long.
//
// The file is read once when serve starts. Nothing is ever changed from the
// television, and nothing here affects what plays or when.
package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// FileName is the settings file's name under the root.
const FileName = "settings.yaml"

// Shortest and longest an on-screen piece may stay up. Anything shorter is a
// flicker, anything longer is chrome over the programme.
const (
	minDuration = 100 * time.Millisecond
	maxDuration = 10 * time.Second
)

// Settings is the resolved settings, with every default filled in.
type Settings struct {
	// ChannelNumber is the green number shown after a channel change.
	ChannelNumber Overlay
	// Bumper is the channel's card shown after a channel change.
	Bumper Overlay
}

// Overlay is one on-screen piece: whether it is shown, and for how long.
type Overlay struct {
	Enabled  bool
	Duration time.Duration
}

// Default is what a root with no settings file gets.
func Default() Settings {
	return Settings{
		ChannelNumber: Overlay{Enabled: true, Duration: 1500 * time.Millisecond},
		Bumper:        Overlay{Enabled: true, Duration: 2 * time.Second},
	}
}

// file is the YAML shape. Every field is optional, which is why they are
// pointers: an absent field keeps its default.
type file struct {
	ChannelNumber *overlayFile `yaml:"channel_number"`
	Bumpers       *overlayFile `yaml:"bumpers"`
}

type overlayFile struct {
	Enabled  *bool   `yaml:"enabled"`
	Duration *string `yaml:"duration"`
}

// Path returns the settings file under root.
func Path(root string) string {
	return filepath.Join(root, FileName)
}

// Load reads the settings under root. A missing file is the defaults and no
// error. A file that does not parse, names a field that does not exist or holds
// a duration out of range is an error, alongside the defaults, so the caller can
// log it and carry on.
func Load(root string) (Settings, error) {
	s := Default()
	data, err := os.ReadFile(Path(root))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read %s: %w", FileName, err)
	}

	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return Default(), fmt.Errorf("%s: %w", FileName, err)
	}

	if err := apply(&s.ChannelNumber, f.ChannelNumber, "channel_number"); err != nil {
		return Default(), err
	}
	if err := apply(&s.Bumper, f.Bumpers, "bumpers"); err != nil {
		return Default(), err
	}
	return s, nil
}

// apply overlays the fields that are present onto o.
func apply(o *Overlay, f *overlayFile, name string) error {
	if f == nil {
		return nil
	}
	if f.Enabled != nil {
		o.Enabled = *f.Enabled
	}
	if f.Duration != nil {
		d, err := time.ParseDuration(*f.Duration)
		if err != nil {
			return fmt.Errorf("%s: %s.duration: %q is not a duration such as 1.5s", FileName, name, *f.Duration)
		}
		if d < minDuration || d > maxDuration {
			return fmt.Errorf("%s: %s.duration: %s is outside %s to %s", FileName, name, d, minDuration, maxDuration)
		}
		o.Duration = d
	}
	return nil
}
