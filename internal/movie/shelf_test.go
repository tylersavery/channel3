package movie

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShelfOrdersByTitle(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []Movie{
		{Title: "Up", Year: 2009, File: "Up (2009).mkv"},
		{Title: "The Iron Giant", Year: 1999, File: "The Iron Giant (1999).mkv"},
		{Title: "A Bug's Life", Year: 1998, File: "A Bug's Life (1998).mkv"},
		{Title: "Gone", File: "Gone.mkv"},
	} {
		if err := writeSidecar(filepath.Join(dir, m.Name()+".json"), m); err != nil {
			t.Fatal(err)
		}
		if m.Title != "Gone" {
			if err := os.WriteFile(filepath.Join(dir, m.File), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	movies, skipped, err := Shelf(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range movies {
		got = append(got, m.Title)
	}
	want := []string{"A Bug's Life", "The Iron Giant", "Up"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("shelf %v, want %v", got, want)
	}
	if len(skipped) != 2 {
		t.Errorf("skipped %v, want the broken sidecar and the missing file", skipped)
	}
}

func TestShelfOfNothing(t *testing.T) {
	movies, skipped, err := Shelf(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(movies) != 0 || len(skipped) != 0 {
		t.Errorf("got %v %v %v", movies, skipped, err)
	}
}

func TestPositionsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "positions.json")
	p, err := ReadPositions(path)
	if err != nil || len(p) != 0 {
		t.Fatalf("a missing file should be no positions: %v %v", p, err)
	}
	p["Up (2009).mkv"] = 42*time.Minute + 10*time.Second + 400*time.Millisecond
	if err := p.Write(path); err != nil {
		t.Fatal(err)
	}
	back, err := ReadPositions(path)
	if err != nil {
		t.Fatal(err)
	}
	if back["Up (2009).mkv"] != 42*time.Minute+10*time.Second {
		t.Errorf("read back %v", back)
	}
}

func TestResumeAt(t *testing.T) {
	m := Movie{File: "Up.mkv", Duration: 96 * 60}
	tests := []struct {
		at   time.Duration
		want time.Duration
	}{
		{0, 0},
		{90 * time.Second, 0},
		{40 * time.Minute, 40 * time.Minute},
		{93 * time.Minute, 0},
	}
	for _, tt := range tests {
		if got := (Positions{"Up.mkv": tt.at}).ResumeAt(m); got != tt.want {
			t.Errorf("stopped at %v: resume at %v, want %v", tt.at, got, tt.want)
		}
	}
}
