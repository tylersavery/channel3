package main

// runServe will run the broadcast service: the mpv supervisor, the station loop,
// the input sources and the HTTP API.
// Phase 5 adds --mpv, --mpv-arg, --start-channel and --standby here.
// Phase 6 adds --input-device, --no-input, --cec and --cec-device.
// Phase 7 adds --listen.
func runServe(g *globals, args []string) error {
	fs := g.flagSet("serve")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	return notImplemented("serve")
}
