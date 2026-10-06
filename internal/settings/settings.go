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
	// TrackInfo is a song's title and artist, shown as it starts and again
	// as it ends.
	TrackInfo Overlay
	// Volume is how the remote's volume buttons behave.
	Volume Volume
}

// Volume control. With Control "pi" the volume and mute buttons set mpv's own
// volume, capped at Max, and the television's volume is left where it was set
// by hand: the loudest anyone should ever hear. With "tv" they are passed to the
// television over CEC and none of the rest applies.
type Volume struct {
	Control string // "pi" or "tv"
	Start   int    // percent at startup; never saved, so every boot starts here
	Max     int    // percent the up button stops at
	Step    int    // percent per press
	Show    bool   // whether a press puts the level on screen
}

// Volume control modes.
const (
	VolumeOnPi = "pi"
	VolumeOnTV = "tv"
)

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
		TrackInfo:     Overlay{Enabled: true, Duration: 8 * time.Second},
		Volume:        Volume{Control: VolumeOnPi, Start: 50, Max: 70, Step: 5, Show: true},
	}
}

// file is the YAML shape. Every field is optional, which is why they are
// pointers: an absent field keeps its default.
type file struct {
	ChannelNumber *overlayFile `yaml:"channel_number"`
	Bumpers       *overlayFile `yaml:"bumpers"`
	TrackInfo     *overlayFile `yaml:"track_info"`
	Volume        *volumeFile  `yaml:"volume"`
}

type volumeFile struct {
	Control *string `yaml:"control"`
	Start   *int    `yaml:"start"`
	Max     *int    `yaml:"max"`
	Step    *int    `yaml:"step"`
	Show    *bool   `yaml:"show"`
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
	if err := apply(&s.TrackInfo, f.TrackInfo, "track_info"); err != nil {
		return Default(), err
	}
	if err := applyVolume(&s.Volume, f.Volume); err != nil {
		return Default(), err
	}
	return s, nil
}

// applyVolume overlays the volume fields that are present onto v, then checks
// the result as a whole, since start and max only make sense together.
func applyVolume(v *Volume, f *volumeFile) error {
	if f == nil {
		return nil
	}
	if f.Control != nil {
		v.Control = *f.Control
	}
	if f.Start != nil {
		v.Start = *f.Start
	}
	if f.Max != nil {
		v.Max = *f.Max
	}
	if f.Step != nil {
		v.Step = *f.Step
	}
	if f.Show != nil {
		v.Show = *f.Show
	}

	if v.Control != VolumeOnPi && v.Control != VolumeOnTV {
		return fmt.Errorf("%s: volume.control: %q must be %s or %s", FileName, v.Control, VolumeOnPi, VolumeOnTV)
	}
	if v.Max < 1 || v.Max > 100 {
		return fmt.Errorf("%s: volume.max: %d must be between 1 and 100", FileName, v.Max)
	}
	if v.Start < 0 || v.Start > v.Max {
		return fmt.Errorf("%s: volume.start: %d must be between 0 and volume.max, %d", FileName, v.Start, v.Max)
	}
	if v.Step < 1 || v.Step > 50 {
		return fmt.Errorf("%s: volume.step: %d must be between 1 and 50", FileName, v.Step)
	}
	return nil
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
