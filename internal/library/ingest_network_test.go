package library

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// networkTestURL is the video the network test ingests: a sixty-second NASA Jet
// Propulsion Laboratory clip, public domain, small enough to download in a few
// seconds. Override it with CHANNEL3_NETWORK_TEST_URL when it goes away.
const networkTestURL = "https://www.youtube.com/watch?v=8-X8acD_r38"

// networkTestEnv gates the only test in Channel Three that touches the network.
const networkTestEnv = "CHANNEL3_NETWORK_TESTS"

// TestIngestNetwork downloads one real video with the real yt-dlp and the real
// ffprobe, and checks that what lands on disk is something the library and mpv
// can use.
//
// It is skipped unless CHANNEL3_NETWORK_TESTS=1, so the ordinary test run stays
// offline and fast. Run it with:
//
//	CHANNEL3_NETWORK_TESTS=1 go test ./internal/library/ -run Network
func TestIngestNetwork(t *testing.T) {
	if os.Getenv(networkTestEnv) != "1" {
		t.Skipf("set %s=1 to run the test that downloads a real video", networkTestEnv)
	}
	for _, bin := range []string{defaultYTDLP, defaultFFProbe} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("%s is required by the network test: %v", bin, err)
		}
	}

	source := networkTestURL
	if override := os.Getenv("CHANNEL3_NETWORK_TEST_URL"); override != "" {
		source = override
	}

	root := t.TempDir()
	channels := []Channel{{ID: "space", Number: 12, Name: "Space Channel", Sources: []Source{{URL: source}}}}
	var out bytes.Buffer

	report, err := Ingest(IngestOptions{
		Root:     root,
		Channels: channels,
		Runner:   YTDLP{},
		Prober:   FFProbe{},
		Out:      &out,
	})
	if err != nil {
		t.Fatalf("Ingest: %v\n%s", err, out.String())
	}
	if report.OK != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v, want one ok and nothing failed\n%s", report, out.String())
	}

	entries, err := os.ReadDir(ChannelDir(root, "space"))
	if err != nil {
		t.Fatalf("read channel dir: %v", err)
	}
	var sidecarName string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			sidecarName = entry.Name()
		}
	}
	if sidecarName == "" {
		t.Fatalf("no sidecar was written: %v", names(entries))
	}

	sidecar, err := ReadSidecar(filepath.Join(ChannelDir(root, "space"), sidecarName))
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if sidecar.Status != StatusOK {
		t.Fatalf("status = %q, want ok", sidecar.Status)
	}
	if sidecar.Title == "" {
		t.Error("the sidecar has no title")
	}
	if sidecar.Source != source {
		t.Errorf("source = %q, want %q", sidecar.Source, source)
	}
	if sidecar.IngestedAt == nil || time.Since(*sidecar.IngestedAt) > time.Hour {
		t.Errorf("ingested_at = %v", sidecar.IngestedAt)
	}
	if strings.Contains(sidecar.File, string(os.PathSeparator)) {
		t.Errorf("file = %q, want a name relative to the sidecar's own directory", sidecar.File)
	}

	media := filepath.Join(ChannelDir(root, "space"), sidecar.File)
	info, err := os.Stat(media)
	if err != nil {
		t.Fatalf("stat the downloaded video: %v", err)
	}
	if info.Size() < 100_000 {
		t.Errorf("the downloaded video is %d bytes, which is too small to be video", info.Size())
	}

	// Probing the file again is the playable check: ffprobe only reads a
	// duration out of a container it could parse.
	probed, err := FFProbe{}.DurationSeconds(media)
	if err != nil {
		t.Fatalf("the downloaded file is not playable: %v", err)
	}
	if diff := probed - sidecar.Duration; diff > 0.001 || diff < -0.001 {
		t.Errorf("sidecar duration %v does not match ffprobe's %v", sidecar.Duration, probed)
	}

	index, err := Scan(root, channels)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := index.Items("space"); len(got) != 1 {
		t.Fatalf("the library index found %d items, want the one just ingested", len(got))
	}

	// A second run re-expands the source, because that is how a video added to a
	// playlist is noticed, but it must not download or probe anything again.
	var again bytes.Buffer
	report, err = Ingest(IngestOptions{
		Root:     root,
		Channels: channels,
		Runner:   YTDLP{},
		Prober:   FFProbe{},
		Out:      &again,
	})
	if err != nil {
		t.Fatalf("second Ingest: %v\n%s", err, again.String())
	}
	if report.Skipped != 1 || report.OK != 0 || report.Failed != 0 {
		t.Errorf("second report = %+v, want one skip\n%s", report, again.String())
	}

	after, err := os.Stat(media)
	if err != nil {
		t.Fatalf("stat the downloaded video again: %v", err)
	}
	if !after.ModTime().Equal(info.ModTime()) || after.Size() != info.Size() {
		t.Error("the second run downloaded the video again")
	}
}
