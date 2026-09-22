package main

// runGuide will print what each channel is airing now and next, as text.
// Phase 3 adds --hours, --at and --channel here.
func runGuide(g *globals, args []string) error {
	fs := g.flagSet("guide")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	return notImplemented("guide")
}
