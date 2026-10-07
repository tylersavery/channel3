package movie

import "testing"

func TestParseName(t *testing.T) {
	tests := []struct {
		file string
		want Name
	}{
		{"The Iron Giant (1999).mkv", Name{"The Iron Giant", 1999}},
		{"The.Iron.Giant.1999.1080p.BluRay.x264.mkv", Name{"The Iron Giant", 1999}},
		{"THE_IRON_GIANT_t00.mkv", Name{"The Iron Giant", 0}},
		{"My Neighbor Totoro [1988].mp4", Name{"My Neighbor Totoro", 1988}},
		{"1917 (2019).mkv", Name{"1917", 2019}},
		{"2001 A Space Odyssey (1968).mkv", Name{"2001 A Space Odyssey", 1968}},
		{"2001.A.Space.Odyssey.mkv", Name{"2001 A Space Odyssey", 0}},
		{"Paddington.2.2017.2160p.UHD.BluRay.HDR.mkv", Name{"Paddington 2", 2017}},
		{"Ponyo.1080p.BluRay.mkv", Name{"Ponyo", 0}},
		{"Wall-E - 2008.mkv", Name{"Wall-E", 2008}},
		{"/movies/inbox/Up (2009).m4v", Name{"Up", 2009}},
		{"HOW_TO_TRAIN_YOUR_DRAGON_t01.mkv", Name{"How to Train Your Dragon", 0}},
	}
	for _, tt := range tests {
		if got := ParseName(tt.file); got != tt.want {
			t.Errorf("ParseName(%q) = %+v, want %+v", tt.file, got, tt.want)
		}
	}
}

func TestNameString(t *testing.T) {
	if got := (Name{"Up", 2009}).String(); got != "Up (2009)" {
		t.Errorf("got %q", got)
	}
	if got := (Name{"Ponyo", 0}).String(); got != "Ponyo" {
		t.Errorf("got %q", got)
	}
}

func TestTrackLabel(t *testing.T) {
	tests := []struct {
		track Track
		want  string
	}{
		{Track{Language: "eng", Channels: 6}, "English · 5.1"},
		{Track{Language: "eng", Title: "Director's Commentary", Channels: 2}, "English · Director's Commentary"},
		{Track{Language: "fre"}, "French"},
		{Track{Language: "en", Title: "English"}, "English"},
		{Track{}, "Unknown"},
		{Track{Language: "swe"}, "swe"},
	}
	for _, tt := range tests {
		if got := tt.track.Label(); got != tt.want {
			t.Errorf("%+v: got %q, want %q", tt.track, got, tt.want)
		}
	}
}
