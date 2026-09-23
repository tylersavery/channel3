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
// Phase 6 adds --input-device, --no-input, --cec and --cec-device here, and
// Phase 7 adds --listen.
func runServe(g *globals, args []string) error {
	fs := g.flagSet("serve")
	mpvPath := fs.String("mpv", "mpv", "path to the mpv binary")
	var mpvArgs repeatedFlag
	fs.Var(&mpvArgs, "mpv-arg", "extra argument passed to mpv, repeatable")
	startChannel := fs.String("start-channel", "", "channel id to tune at startup (default the lowest numbered channel)")
	standby := fs.String("standby", "", "path to a Please Stand By image that replaces the built in card")
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

	station, err := newStation(stationOptions{
		Player:       mpv,
		Clock:        schedule.NewClock(time.Local),
		Now:          time.Now,
		Reload:       func() ([]schedule.Channel, error) { return loadStation(root) },
		StartChannel: opts.StartChannel,
		Logger:       slog.Default(),
	})
	if err != nil {
		return err
	}

	slog.Info("broadcasting", "root", root, "channel", station.tuned, "pid", os.Getpid())
	return station.run(ctx)
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
