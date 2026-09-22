package library

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Status is the outcome ingest recorded for an item.
type Status string

const (
	// StatusOK marks an item that downloaded and probed cleanly. Only these play.
	StatusOK Status = "ok"
	// StatusFailed marks an item ingest could not produce. It stays visible in
	// the library so the failure is not silent, and ingest retries it next run.
	StatusFailed Status = "failed"
)

// Sidecar is the JSON record written beside every item in the library, at
// <root>/library/<channel-id>/<id>.json.
//
// This schema is a contract. Ingest writes it, the index reads it, and changing
// a field name invalidates every library already on disk.
//
// An ok sidecar carries id, title, source, file, duration, ingested_at and
// status. A failed one carries id, source, status, error and attempted_at, and
// nothing else: there is no file and no duration to record.
//
// File is relative to the sidecar's own directory for anything that was
// downloaded, and absolute for a file:// source, which is never copied.
// Duration is seconds as a float, exact enough that cumulative schedule
// boundaries do not drift. Times are RFC 3339 in UTC.
type Sidecar struct {
	ID          string     `json:"id"`
	Title       string     `json:"title,omitempty"`
	Source      string     `json:"source"`
	File        string     `json:"file,omitempty"`
	Duration    float64    `json:"duration,omitempty"`
	IngestedAt  *time.Time `json:"ingested_at,omitempty"`
	Status      Status     `json:"status"`
	Error       string     `json:"error,omitempty"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
}

// sidecarJSON exists so Sidecar.MarshalJSON can encode itself without recursing.
type sidecarJSON Sidecar

// MarshalJSON normalises both times to UTC, which the schema requires, and
// leaves HTML characters alone so a source URL keeps its ampersands.
func (s Sidecar) MarshalJSON() ([]byte, error) {
	if s.IngestedAt != nil {
		utc := s.IngestedAt.UTC()
		s.IngestedAt = &utc
	}
	if s.AttemptedAt != nil {
		utc := s.AttemptedAt.UTC()
		s.AttemptedAt = &utc
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(sidecarJSON(s)); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// MarshalSidecar returns the canonical on-disk encoding: two-space indented
// JSON with a trailing newline and no HTML escaping, so source URLs keep their
// ampersands and a sidecar stays readable in a terminal.
func MarshalSidecar(s Sidecar) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("encode sidecar %q: %w", s.ID, err)
	}
	return buf.Bytes(), nil
}

// UnmarshalSidecar decodes a sidecar. Unknown fields are ignored so a library
// written by a newer version of channel3 still plays.
func UnmarshalSidecar(data []byte) (Sidecar, error) {
	var s Sidecar
	if err := json.Unmarshal(data, &s); err != nil {
		return Sidecar{}, fmt.Errorf("decode sidecar: %w", err)
	}
	return s, nil
}

// ReadSidecar reads and decodes the sidecar at path.
func ReadSidecar(path string) (Sidecar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Sidecar{}, fmt.Errorf("read sidecar: %w", err)
	}
	s, err := UnmarshalSidecar(data)
	if err != nil {
		return Sidecar{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// WriteSidecar writes s to path in the canonical encoding.
func WriteSidecar(path string, s Sidecar) error {
	data, err := MarshalSidecar(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write sidecar: %w", err)
	}
	return nil
}
