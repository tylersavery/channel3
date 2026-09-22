// Package library owns Channel Three's content on disk: the channel config
// files, the JSON sidecar written beside every ingested item, and the index that
// turns both into playable items.
//
// Real config and media live outside this repository, under /srv/channel3 on the
// Pi and ~/srv/channel3 on the Mac. The layout is:
//
//	<root>/channels/<anything>.yaml   one channel per file
//	<root>/library/<channel-id>/      that channel's media and sidecars
//	<root>/local/                     video that was never downloaded
//
// Nothing in this package touches the network. Ingest, which shells out to
// yt-dlp, is the only code in Channel Three that does.
package library

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Channel is one themed channel: a number to tune to and the sources its items
// are ingested from.
type Channel struct {
	ID      string
	Number  int
	Name    string
	Sources []string
}

// channelFile is the YAML shape of one channel config file.
type channelFile struct {
	ID      string   `yaml:"id"`
	Number  int      `yaml:"number"`
	Name    string   `yaml:"name"`
	Sources []string `yaml:"sources"`
}

// configFields are the only keys a channel config file may contain.
var configFields = []string{"id", "number", "name", "sources"}

// idPattern is the accepted channel id. It doubles as a directory name under
// <root>/library, which is why it stays this narrow.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// allowedSchemes are the URL schemes a source may use. Anything else is a config
// error rather than something ingest discovers later.
var allowedSchemes = []string{"http://", "https://", "file://"}

// ChannelsDir returns the directory holding channel config under root.
func ChannelsDir(root string) string {
	return filepath.Join(root, "channels")
}

// ConfigError is one validation failure, naming the file and field it came from.
type ConfigError struct {
	File  string
	Field string
	Msg   string
}

func (e *ConfigError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.File, e.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", e.File, e.Field, e.Msg)
}

// LoadChannels reads every *.yaml file in dir, one channel per file, and returns
// the channels sorted by number.
//
// Every validation failure in every file is reported, not just the first, so one
// run of the loader tells you everything that is wrong with the config. A
// channel with zero sources is allowed and warned about: it shows the Please
// Stand By card until it has something to play.
//
// A missing dir is an error wrapping fs.ErrNotExist. An existing but empty dir
// returns no channels and no error.
func LoadChannels(dir string) ([]Channel, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read channel config directory: %w", err)
	}

	var channels []Channel
	var problems []error
	claimedBy := make(map[int]string)

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := entry.Name()

		ch, numberValid, errs := readChannelFile(filepath.Join(dir, name), name)
		if numberValid {
			if prior, taken := claimedBy[ch.Number]; taken {
				errs = append(errs, &ConfigError{
					File:  name,
					Field: "number",
					Msg:   fmt.Sprintf("number %d is already used by %s", ch.Number, prior),
				})
			} else {
				claimedBy[ch.Number] = name
			}
		}
		if len(errs) > 0 {
			problems = append(problems, errs...)
			continue
		}
		channels = append(channels, ch)
	}

	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	if len(channels) == 0 {
		slog.Warn("no channel config found", "dir", dir)
	}
	slices.SortFunc(channels, func(a, b Channel) int { return cmp.Compare(a.Number, b.Number) })
	return channels, nil
}

// readChannelFile parses and validates one config file. It reports whether the
// number is usable for the duplicate check, and every problem it found.
func readChannelFile(path, name string) (Channel, bool, []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Channel{}, false, []error{&ConfigError{File: name, Msg: err.Error()}}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return Channel{}, false, []error{&ConfigError{File: name, Msg: "file is empty"}}
	}

	// Decoding into a node map first catches typos in field names, which would
	// otherwise leave a channel silently missing its sources.
	var keys map[string]yaml.Node
	if err := yaml.Unmarshal(data, &keys); err != nil {
		return Channel{}, false, []error{&ConfigError{File: name, Msg: fmt.Sprintf("parse: %v", err)}}
	}

	var raw channelFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Channel{}, false, []error{&ConfigError{File: name, Msg: fmt.Sprintf("parse: %v", err)}}
	}

	var errs []error
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		if !slices.Contains(configFields, key) {
			errs = append(errs, &ConfigError{
				File:  name,
				Field: key,
				Msg:   fmt.Sprintf("unknown field, expected one of %s", strings.Join(configFields, ", ")),
			})
		}
	}
	if !idPattern.MatchString(raw.ID) {
		errs = append(errs, &ConfigError{
			File:  name,
			Field: "id",
			Msg:   fmt.Sprintf("%q must match %s", raw.ID, idPattern),
		})
	}
	numberValid := raw.Number >= 1 && raw.Number <= 999
	if !numberValid {
		errs = append(errs, &ConfigError{
			File:  name,
			Field: "number",
			Msg:   fmt.Sprintf("%d must be between 1 and 999", raw.Number),
		})
	}
	if strings.TrimSpace(raw.Name) == "" {
		errs = append(errs, &ConfigError{File: name, Field: "name", Msg: "must not be empty"})
	}
	for i, src := range raw.Sources {
		if !hasAllowedScheme(src) {
			errs = append(errs, &ConfigError{
				File:  name,
				Field: fmt.Sprintf("sources[%d]", i),
				Msg:   fmt.Sprintf("%q must start with %s", src, strings.Join(allowedSchemes, ", ")),
			})
		}
	}
	if len(raw.Sources) == 0 {
		slog.Warn("channel has no sources and will show the Please Stand By card", "file", name, "id", raw.ID)
	}

	ch := Channel{ID: raw.ID, Number: raw.Number, Name: raw.Name, Sources: raw.Sources}
	return ch, numberValid, errs
}

// hasAllowedScheme reports whether src starts with a scheme ingest can handle.
func hasAllowedScheme(src string) bool {
	for _, scheme := range allowedSchemes {
		if strings.HasPrefix(src, scheme) {
			return true
		}
	}
	return false
}
