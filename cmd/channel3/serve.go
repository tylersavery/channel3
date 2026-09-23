package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tylersavery/channel3/internal/input"
	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/player"
	"github.com/tylersavery/channel3/internal/schedule"
)

// repeatedFlag collects a flag given more than once, in the order it was given.
type repeatedFlag []string

func (f *repeatedFlag) String() string { return fmt.Sprint([]string(*f)) }

func (f *repeatedFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// runServe runs the broadcast service: the mpv supervisor and the station loop.
//
// Phase 7 adds --listen here.
func runServe(g *globals, args []string) error {
	fs := g.flagSet("serve")
	mpvPath := fs.String("mpv", "mpv", "path to the mpv binary")
	var mpvArgs repeatedFlag
	fs.Var(&mpvArgs, "mpv-arg", "extra argument passed to mpv, repeatable")
	startChannel := fs.String("start-channel", "", "channel id to tune at startup (default the lowest numbered channel)")
	standby := fs.String("standby", "", "path to a Please Stand By image that replaces the built in card")
	inputDevice := fs.String("input-device", "", "evdev device to read the remote from (default: find the Flirc, then any keyboard)")
	noInput := fs.Bool("no-input", false, "do not read any keys, so the channel cannot be changed")
	useCEC := fs.Bool("cec", false, "send power and volume keys to the television over HDMI-CEC")
	cecDevice := fs.String("cec-device", "/dev/cec0", "CEC device cec-ctl talks to")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "channel3 serve: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return &exitError{code: 1}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := serve(ctx, g.root, serveOptions{
		MPV:          *mpvPath,
		MPVArgs:      mpvArgs,
		StartChannel: *startChannel,
		StandbyCard:  *standby,
		InputDevice:  *inputDevice,
		NoInput:      *noInput,
		CEC:          *useCEC,
		CECDevice:    *cecDevice,
	}); err != nil {
		return &exitError{code: 1, err: err}
	}
	return nil
}

// serveOptions are the serve flags, already parsed.
type serveOptions struct {
	MPV          string
	MPVArgs      []string
	StartChannel string
	StandbyCard  string
	// InputDevice is the evdev device to read. Empty auto-detects.
	InputDevice string
	// NoInput turns every key source off.
	NoInput bool
	// CEC turns on forwarding power and volume to the television.
	CEC bool
	// CECDevice is the CEC character device cec-ctl is pointed at.
	CECDevice string
}

// serve broadcasts until the context is cancelled.
func serve(ctx context.Context, root string, opts serveOptions) error {
	pid, err := writePIDFile(root)
	if err != nil {
		return err
	}
	defer removePIDFile(pid)

	runtimeDir, err := player.RuntimeDir()
	if err != nil {
		return err
	}
	card := opts.StandbyCard
	if card == "" {
		card, err = player.WriteStandbyCard(runtimeDir)
		if err != nil {
			return err
		}
	} else if _, err := os.Stat(card); err != nil {
		return fmt.Errorf("serve: --standby %s: %w", card, err)
	}

	// mpv runs on its own context rather than the signal context. Cancelling
	// the signal context kills the process outright, and a Ctrl-C has to leave
	// time for the quit command that hands the television back cleanly.
	mpvCtx, stopMPV := context.WithCancel(context.Background())
	defer stopMPV()

	mpv, err := player.Start(mpvCtx, player.Options{
		Launcher:    player.MPVLauncher{Binary: opts.MPV, Extra: opts.MPVArgs},
		Socket:      player.SocketPath(runtimeDir),
		StandbyPath: card,
		Logger:      slog.Default(),
	})
	if err != nil {
		return err
	}
	// Closing the supervisor quits mpv cleanly, so the television is handed
	// back rather than left on a frozen frame.
	defer func() {
		if err := mpv.Close(); err != nil {
			slog.Error("could not stop mpv", "error", err)
		}
	}()

	keys, stopInput := startInput(ctx, opts, slog.Default())
	// The terminal is put back before this function returns rather than when
	// the source's own goroutine gets round to it, so a Ctrl-C never leaves the
	// developer's shell in raw mode.
	defer stopInput()

	var tv input.CEC
	if opts.CEC {
		tv = input.NewCEC(opts.CECDevice, slog.Default())
		slog.Info("television control is on", "cec device", opts.CECDevice)
	}

	station, err := newStation(stationOptions{
		Player:       mpv,
		Clock:        schedule.NewClock(time.Local),
		Now:          time.Now,
		Reload:       func() ([]schedule.Channel, error) { return loadStation(root) },
		StartChannel: opts.StartChannel,
		Logger:       slog.Default(),
		Keys:         keys,
		CEC:          tv,
	})
	if err != nil {
		return err
	}

	slog.Info("broadcasting", "root", root, "channel", station.Tuned(), "pid", os.Getpid())
	return station.run(ctx)
}

// startInput opens every key source this machine has and merges them.
//
// On the Pi that is the evdev device the Flirc presents, and there is no
// terminal. On a Mac it is the terminal, and there is no evdev. A source that
// cannot be opened is a warning and nothing more: a television with no remote
// still plays the channel it booted on, which is a far better failure than not
// booting.
//
// The returned function restores anything the sources changed about the
// terminal and must be called before the process exits.
func startInput(ctx context.Context, opts serveOptions, log *slog.Logger) (<-chan input.Key, func()) {
	if opts.NoInput {
		log.Info("input is off, the channel cannot be changed")
		return nil, func() {}
	}

	var sources []input.Source
	restore := func() {}

	if input.IsTerminal(os.Stdin) {
		log.Info("reading keys from the terminal", "keys", "+ and - or the arrows to change channel, digits to tune, p power, [ and ] volume")
		tty := input.NewTTY(os.Stdin, log)
		restore = tty.Restore
		sources = append(sources, tty)
	}

	device := opts.InputDevice
	if device == "" {
		found, err := input.DetectDevice()
		if err != nil {
			log.Warn("no remote was found, the channel can only be changed from the terminal", "error", err)
		} else {
			device = found
		}
	}
	if device != "" {
		sources = append(sources, input.NewDevice(device, log))
	}

	if len(sources) == 0 {
		log.Warn("no key sources are available, the channel cannot be changed")
		return nil, restore
	}
	return input.Merge(ctx, sources...), restore
}

// writePIDFile records this process id so ingest can refuse to run during a
// broadcast. It returns the path it wrote.
//
// An existing file whose process is gone is stale and is replaced; one whose
// process is alive means a second serve, which is refused because two mpv
// instances would fight over the screen.
func writePIDFile(root string) (string, error) {
	path := library.PIDFile(root)
	if running, err := pidFileOwner(path); err != nil {
		return "", err
	} else if running > 0 {
		return "", fmt.Errorf("serve: already broadcasting as pid %d (%s)", running, path)
	}

	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("serve: write %s: %w", path, err)
	}
	return path, nil
}

// pidFileOwner returns the pid in the file if that process is still running,
// and zero if the file is absent, unreadable or stale.
func pidFileOwner(path string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("serve: read %s: %w", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		slog.Warn("replacing an unreadable pid file", "path", path)
		return 0, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, nil
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		slog.Info("replacing a stale pid file", "path", path, "pid", pid)
		return 0, nil
	}
	return pid, nil
}

// removePIDFile clears the pid file on the way out.
func removePIDFile(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Error("could not remove the pid file", "path", path, "error", err)
	}
}
