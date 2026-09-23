package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeChannelConfig writes one channel config file into root/channels.
func writeChannelConfig(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "channels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// missingBinary is a path that does not exist, used so a test can make yt-dlp
// fail without touching the network.
func missingBinary(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-such-binary")
}

// TestIngestExitCodes pins the contract the ingest wrapper script and any cron
// job depend on: 0 when nothing failed, 2 when an item failed, 1 for a usage or
// config error.
func TestIngestExitCodes(t *testing.T) {
	t.Run("usage error is 1", func(t *testing.T) {
		if got := run([]string{"ingest", "--no-such-flag"}); got != 1 {
			t.Errorf("exit code = %d, want 1", got)
		}
	})

	t.Run("unexpected argument is 1", func(t *testing.T) {
		if got := run([]string{"ingest", "trains"}); got != 1 {
			t.Errorf("exit code = %d, want 1", got)
		}
	})

	t.Run("config error is 1", func(t *testing.T) {
		root := t.TempDir()
		writeChannelConfig(t, root, "broken.yaml", "id: Trains\nnumber: 0\nname: \"\"\nsources: []\n")
		if got := run([]string{"ingest", "--root", root}); got != 1 {
			t.Errorf("exit code = %d, want 1", got)
		}
	})

	t.Run("missing config directory is 1", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "absent")
		if got := run([]string{"ingest", "--root", root}); got != 1 {
			t.Errorf("exit code = %d, want 1", got)
		}
	})

	t.Run("a failed item is 2", func(t *testing.T) {
		root := t.TempDir()
		writeChannelConfig(t, root, "trains.yaml",
			"id: trains\nnumber: 3\nname: Train TV\nsources:\n  - https://example.invalid/nope\n")

		if got := run([]string{"ingest", "--root", root, "--yt-dlp", missingBinary(t)}); got != 2 {
			t.Errorf("exit code = %d, want 2", got)
		}

		entries, err := os.ReadDir(filepath.Join(root, "library", "trains"))
		if err != nil {
			t.Fatalf("read library dir: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("got %d files, want one failed sidecar", len(entries))
		}
	})

	t.Run("nothing to do is 0", func(t *testing.T) {
		root := t.TempDir()
		writeChannelConfig(t, root, "quiet.yaml", "id: quiet\nnumber: 9\nname: Quiet Channel\nsources: []\n")

		if got := run([]string{"ingest", "--root", root, "--yt-dlp", missingBinary(t)}); got != 0 {
			t.Errorf("exit code = %d, want 0", got)
		}
	})

	t.Run("a dry run does not download", func(t *testing.T) {
		root := t.TempDir()
		writeChannelConfig(t, root, "trains.yaml",
			"id: trains\nnumber: 3\nname: Train TV\nsources:\n  - file:///no/such/video.mp4\n")

		if got := run([]string{"ingest", "--root", root, "--dry-run"}); got != 0 {
			t.Errorf("exit code = %d, want 0", got)
		}
		if _, err := os.Stat(filepath.Join(root, "library")); !os.IsNotExist(err) {
			t.Errorf("a dry run wrote to the library: %v", err)
		}
	})
}

// TestIngestRootFromEnvironment checks that --root may be left out when
// CHANNEL3_ROOT is set, which is how the systemd unit will call it.
func TestIngestRootFromEnvironment(t *testing.T) {
	root := t.TempDir()
	writeChannelConfig(t, root, "quiet.yaml", "id: quiet\nnumber: 9\nname: Quiet Channel\nsources: []\n")
	t.Setenv(rootEnv, root)

	if got := run([]string{"ingest", "--yt-dlp", missingBinary(t)}); got != 0 {
		t.Errorf("exit code = %d, want 0", got)
	}
}
