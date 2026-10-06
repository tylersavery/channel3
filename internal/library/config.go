// Package library owns Channel Three's content on disk: the channel config
// files, the JSON sidecar written beside every ingested item, and the index that
// turns both into playable items.
//
// Real config and media live outside this repository, under /srv/channel3 on the
// Pi and ~/srv/channel3 on the Mac. The layout is:
//
//	<root>/channels/<anything>.yaml   one channel per file
//	<root>/icons/<name>.svg           bumper icons the channel files name
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
	"image/color"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Channel is one themed channel: a number to tune to and the sources its items
// are ingested from.
type Channel struct {
	ID      string
	Number  int
	Name    string
	Sources []Source
	// Bumper is the card shown after tuning to the channel. Nil means the
	// channel has none.
	Bumper *Bumper
	// Guide makes the channel the on-screen guide rather than a channel of
	// its own videos. Nil for every other channel.
	Guide *Guide
}

// Guide is a guide channel's settings.
type Guide struct {
	// Music is the id of the channel whose songs play behind the guide, or
	// "" for a silent guide.
	Music string
}

// Bumper is a channel's card: its colour and, optionally, an icon.
type Bumper struct {
	// Icon is an SVG file name under <root>/icons, or "" for a card with the
	// channel name alone.
	Icon string
	// Color is the card's background.
	Color color.RGBA
}

// bumperFile is the YAML shape of a channel's bumper block.
type bumperFile struct {
	Icon  string `yaml:"icon"`
	Color string `yaml:"color"`
}

// bumperFields are the only keys a bumper block may contain.
var bumperFields = []string{"icon", "color"}

// colorPattern is the accepted bumper colour, #RRGGBB.
var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// IconsDir returns the directory holding bumper icons under root.
func IconsDir(root string) string {
	return filepath.Join(root, "icons")
}

// Source is one entry of a channel's sources list.
//
// URL is a http or https URL to download, a file:// URL naming video already on
// disk, or a path relative to the root, which is how video that travels with the
// library is written. Title, when it is set, is the guide title of everything
// the source produces, overriding both the file name and what yt-dlp reports.
type Source struct {
	URL   string
	Title string
	// fromFolder marks a source expandFolders made from one file in a folder
	// source. Its title is the file's name, a default rather than a choice, so
	// a song's own tags win over it and a later run never rewrites a title
	// with it.
	fromFolder bool
}

// sourceMapping is the mapping form of a source in YAML.
type sourceMapping struct {
	Source string `yaml:"source"`
	Title  string `yaml:"title"`
}

// sourceFields are the only keys the mapping form of a source may contain.
var sourceFields = []string{"source", "title"}

// UnmarshalYAML accepts both forms a source may take: a bare string, which is
// the source with no title, and a mapping of source and title.
func (s *Source) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var plain string
		if err := node.Decode(&plain); err != nil {
			return fmt.Errorf("sources: %w", err)
		}
		*s = Source{URL: plain}
		return nil
	case yaml.MappingNode:
		return s.unmarshalMapping(node)
	default:
		return fmt.Errorf("sources: an entry must be a source on its own or a mapping of %s",
			strings.Join(sourceFields, " and "))
	}
}

// unmarshalMapping decodes the mapping form, rejecting a key that is neither
// source nor title so a typo is never a silently missing title.
func (s *Source) unmarshalMapping(node *yaml.Node) error {
	var keys map[string]yaml.Node
	if err := node.Decode(&keys); err != nil {
		return fmt.Errorf("sources: %w", err)
	}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		if !slices.Contains(sourceFields, key) {
			return fmt.Errorf("sources: unknown field %q, expected one of %s",
				key, strings.Join(sourceFields, ", "))
		}
	}

	var raw sourceMapping
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("sources: %w", err)
	}
	if strings.TrimSpace(raw.Source) == "" {
		return errors.New("sources: a mapping entry must have a source")
	}
	*s = Source{URL: raw.Source, Title: strings.TrimSpace(raw.Title)}
	return nil
}

// channelFile is the YAML shape of one channel config file.
type channelFile struct {
	ID      string    `yaml:"id"`
	Number  int       `yaml:"number"`
	Name    string    `yaml:"name"`
	Sources []Source  `yaml:"sources"`
	Bumper  yaml.Node `yaml:"bumper"`
	Guide   yaml.Node `yaml:"guide"`
}

// configFields are the only keys a channel config file may contain.
var configFields = []string{"id", "number", "name", "sources", "bumper", "guide"}

// idPattern is the accepted channel id. It doubles as a directory name under
// <root>/library, which is why it stays this narrow.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// allowedSchemes are the URL schemes a source may use. Anything else is a config
// error rather than something ingest discovers later.
var allowedSchemes = []string{"http://", "https://", "file://"}

// schemePattern matches the leading "scheme:" of a source. A source that has one
// must use a scheme ingest can handle; a source that has none is read as a path
// relative to the root.
var schemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

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
	numberClaimedBy := make(map[int]string)
	idClaimedBy := make(map[string]string)

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := entry.Name()

		ch, usable, errs := readChannelFile(filepath.Join(dir, name), name)
		if usable.id {
			if prior, taken := idClaimedBy[ch.ID]; taken {
				errs = append(errs, &ConfigError{
					File:  name,
					Field: "id",
					Msg:   fmt.Sprintf("id %q is already used by %s", ch.ID, prior),
				})
			} else {
				idClaimedBy[ch.ID] = name
			}
		}
		if usable.number {
			if prior, taken := numberClaimedBy[ch.Number]; taken {
				errs = append(errs, &ConfigError{
					File:  name,
					Field: "number",
					Msg:   fmt.Sprintf("number %d is already used by %s", ch.Number, prior),
				})
			} else {
				numberClaimedBy[ch.Number] = name
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

// uniqueness says which cross-file uniqueness checks one file's fields are good
// enough to take part in. A field that failed its own validation is left out, so
// two files with the same malformed id are not also reported as duplicates.
type uniqueness struct {
	id     bool
	number bool
}

// readChannelFile parses and validates one config file. It reports which fields
// are usable for the duplicate checks, and every problem it found.
func readChannelFile(path, name string) (Channel, uniqueness, []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Channel{}, uniqueness{}, []error{&ConfigError{File: name, Msg: err.Error()}}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return Channel{}, uniqueness{}, []error{&ConfigError{File: name, Msg: "file is empty"}}
	}

	// Decoding into a node map first catches typos in field names, which would
	// otherwise leave a channel silently missing its sources.
	var keys map[string]yaml.Node
	if err := yaml.Unmarshal(data, &keys); err != nil {
		return Channel{}, uniqueness{}, []error{&ConfigError{File: name, Msg: fmt.Sprintf("parse: %v", err)}}
	}

	var raw channelFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Channel{}, uniqueness{}, []error{&ConfigError{File: name, Msg: fmt.Sprintf("parse: %v", err)}}
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
	idValid := idPattern.MatchString(raw.ID)
	if !idValid {
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
		if problem := sourceProblem(src.URL); problem != "" {
			errs = append(errs, &ConfigError{
				File:  name,
				Field: fmt.Sprintf("sources[%d]", i),
				Msg:   problem,
			})
		}
	}
	if len(raw.Sources) == 0 {
		slog.Warn("channel has no sources and will show the Please Stand By card", "file", name, "id", raw.ID)
	}

	bumper, bumperErrs := readBumper(raw.Bumper, name)
	errs = append(errs, bumperErrs...)
	guide, guideErr := readGuide(raw.Guide, name)
	if guideErr != nil {
		errs = append(errs, guideErr)
	}
	if guide != nil && len(raw.Sources) > 0 {
		errs = append(errs, &ConfigError{File: name, Field: "sources", Msg: "a guide channel shows the guide, so it has no sources of its own"})
	}

	ch := Channel{ID: raw.ID, Number: raw.Number, Name: raw.Name, Sources: raw.Sources, Bumper: bumper, Guide: guide}
	return ch, uniqueness{id: idValid, number: numberValid}, errs
}

// readGuide validates a guide block: a mapping with at most a music channel id.
// An absent block is an ordinary channel; an empty one is a silent guide.
func readGuide(node yaml.Node, file string) (*Guide, error) {
	if node.Kind == 0 {
		return nil, nil
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return &Guide{}, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, &ConfigError{File: file, Field: "guide", Msg: "must be a mapping, such as music: radio-classical"}
	}
	var keys map[string]yaml.Node
	if err := node.Decode(&keys); err != nil {
		return nil, &ConfigError{File: file, Field: "guide", Msg: err.Error()}
	}
	for key := range keys {
		if key != "music" {
			return nil, &ConfigError{File: file, Field: "guide." + key, Msg: "unknown field, expected music"}
		}
	}
	var raw struct {
		Music string `yaml:"music"`
	}
	if err := node.Decode(&raw); err != nil {
		return nil, &ConfigError{File: file, Field: "guide", Msg: err.Error()}
	}
	return &Guide{Music: strings.TrimSpace(raw.Music)}, nil
}

// readBumper validates a channel's bumper block. An absent block is no bumper
// and no error.
//
// Whether the icon file exists is not checked here: icons are synced beside the
// channel files, and a missing one costs the card its picture, not the channel
// its place on the air.
func readBumper(node yaml.Node, file string) (*Bumper, []error) {
	if node.Kind == 0 {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, []error{&ConfigError{File: file, Field: "bumper", Msg: "must be a mapping of " + strings.Join(bumperFields, " and ")}}
	}

	var errs []error
	var keys map[string]yaml.Node
	if err := node.Decode(&keys); err != nil {
		return nil, []error{&ConfigError{File: file, Field: "bumper", Msg: err.Error()}}
	}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		if !slices.Contains(bumperFields, key) {
			errs = append(errs, &ConfigError{
				File:  file,
				Field: "bumper." + key,
				Msg:   fmt.Sprintf("unknown field, expected one of %s", strings.Join(bumperFields, ", ")),
			})
		}
	}

	var raw bumperFile
	if err := node.Decode(&raw); err != nil {
		return nil, append(errs, &ConfigError{File: file, Field: "bumper", Msg: err.Error()})
	}
	background, colorOK := parseHexColor(raw.Color)
	if !colorOK {
		errs = append(errs, &ConfigError{
			File:  file,
			Field: "bumper.color",
			Msg:   fmt.Sprintf("%q must be a colour written as #RRGGBB, such as #E8A33D", raw.Color),
		})
	}
	icon := strings.TrimSpace(raw.Icon)
	if icon != "" && (filepath.Base(icon) != icon || filepath.Ext(icon) != ".svg" || strings.HasPrefix(icon, ".")) {
		errs = append(errs, &ConfigError{
			File:  file,
			Field: "bumper.icon",
			Msg:   fmt.Sprintf("%q must be the name of an .svg file in the icons directory, such as tractor.svg", raw.Icon),
		})
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return &Bumper{Icon: icon, Color: background}, nil
}

// parseHexColor reads a #RRGGBB colour and reports whether it was one.
func parseHexColor(hex string) (color.RGBA, bool) {
	if !colorPattern.MatchString(hex) {
		return color.RGBA{}, false
	}
	rgb, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 0xff}, true
}

// sourceProblem returns why src cannot be used as a source, or "" when it can.
//
// A source that carries a scheme must carry one ingest can handle. A source that
// carries none is a path relative to the root, which is what lets a library
// ingested on the Mac play on the Pi: the same config and the same sidecars
// resolve against whichever root they are read from.
func sourceProblem(src string) string {
	if schemePattern.MatchString(src) {
		if hasAllowedScheme(src) {
			return ""
		}
		return fmt.Sprintf("%q must start with %s, or be a root-relative path such as local/steam-engines.mp4",
			src, strings.Join(allowedSchemes, ", "))
	}
	if isRootRelative(src) {
		return ""
	}
	return fmt.Sprintf("%q is not a root-relative path: write it as local/steam-engines.mp4, "+
		"with no leading slash and no .. segment", src)
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

// isRootRelative reports whether src is a clean relative path that stays under
// the root it is joined to.
//
// Cleanliness is part of the rule rather than something to fix up, because
// local/../../etc/passwd cleans to a path outside the root and a config file
// should say what it means.
func isRootRelative(src string) bool {
	if src == "" || filepath.IsAbs(src) {
		return false
	}
	if filepath.Clean(src) != src {
		return false
	}
	return !slices.Contains(strings.Split(src, string(filepath.Separator)), "..")
}
