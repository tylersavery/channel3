package main

// runIngest will download every configured source into the library and write a
// sidecar for each item.
// Phase 2 adds --channel, --dry-run, --yt-dlp and --ffprobe here.
func runIngest(g *globals, args []string) error {
	fs := g.flagSet("ingest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	return notImplemented("ingest")
}
