package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tylersavery/channel3/internal/api"
	"github.com/tylersavery/channel3/internal/bumper"
	"github.com/tylersavery/channel3/internal/input"
	"github.com/tylersavery/channel3/internal/library"
	"github.com/tylersavery/channel3/internal/player"
	"github.com/tylersavery/channel3/internal/schedule"
	"github.com/tylersavery/channel3/internal/settings"
	"github.com/tylersavery/channel3/web"
)

// defaultListen is the address the guide is served on. The project pins 3333
// for development and uses the same port on the Pi, where nothing else listens.
const defaultListen = ":3333"

// embeddedUIDir is where the Vite build lands inside web/dist, which is what
// web.Dist embeds.
const embeddedUIDir = "dist/ui"

// HTTP server timeouts. A phone on the home network is the only client, so
// these are short: nothing here streams, and every response is a small JSON
// document or one file out of a build.
const (
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 10 * time.Second
	httpWriteTimeout      = 30 * time.Second
	httpIdleTimeout       = 60 * time.Second
	httpShutdownTimeout   = 5 * time.Second
)

// repeatedFlag collects a flag given more than once, in the order it was given.
type repeatedFlag []string

func (f *repeatedFlag) String() string { return fmt.Sprint([]string(*f)) }

func (f *repeatedFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// runServe runs the broadcast service: the mpv supervisor, the station loop and
// the guide API.
func runServe(g *globals, args []string) error {
	fs := g.flagSet("serve")
	mpvPath := fs.String("mpv", "mpv", "path to the mpv binary")
	listen := fs.String("listen", defaultListen, "address the guide API and web interface listen on")
	uiDir := fs.String("ui-dir", "", "serve the web interface from this directory instead of the build embedded in the binary")
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
		Listen:       *listen,
		UIDir:        *uiDir,
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
	MPV     string
	MPVArgs []string
	// Listen is the address the guide API and the web interface are served on.
	Listen string
	// UIDir serves the web interface from disk rather than from the build
	// embedded in the binary, which is how the page is developed.
	UIDir        string
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

	clock := schedule.NewClock(time.Local)

	// The guide answers from whichever station is running, and there is none
	// until mpv is up and the library has been read. Everything it is asked
	// before that is answered honestly: no channels, nothing tuned.
	var current atomic.Pointer[station]
	ui, err := webUI(opts.UIDir)
	if err != nil {
		return err
	}

	// The listener goes up before mpv. An appliance whose television is dark
	// because mpv will not start is exactly when someone reaches for their
	// phone, and the guide answering is how they find out the service is alive.
	stopHTTP, err := startHTTP(opts.Listen, api.Deps{
		Channels: func() []schedule.Channel {
			if s := current.Load(); s != nil {
				// The exclusions go with it, so the guide shows the order the
				// television is really playing rather than one that still
				// includes a file mpv could not open.
				return s.PlayableChannels()
			}
			return nil
		},
		Now: time.Now,
		Tuned: func() string {
			if s := current.Load(); s != nil {
				return s.Tuned()
			}
			return ""
		},
		Clock: clock,
		UI:    ui,
	})
	if err != nil {
		return err
	}
	defer stopHTTP()

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

	// A settings file with a mistake in it costs the look, never the
	// broadcast: the defaults are used and the journal says what was wrong.
	look, err := settings.Load(root)
	if err != nil {
		slog.Error("settings are not usable, using the defaults", "file", settings.Path(root), "error", err)
	}
	slog.Info("on-screen settings",
		"channel number", look.ChannelNumber.Enabled, "number for", look.ChannelNumber.Duration,
		"bumpers", look.Bumper.Enabled, "bumper for", look.Bumper.Duration)

	station, err := newStation(stationOptions{
		Player:       mpv,
		Clock:        clock,
		Now:          time.Now,
		Reload:       func() ([]schedule.Channel, error) { return loadStation(root) },
		StartChannel: opts.StartChannel,
		Logger:       slog.Default(),
		Keys:         keys,
		CEC:          tv,
		Settings:     &look,
		Cards:        func() map[string]bumper.Card { return loadCards(root) },
	})
	if err != nil {
		return err
	}
	current.Store(station)

	slog.Info("broadcasting", "root", root, "channel", station.Tuned(), "pid", os.Getpid())
	return station.run(ctx)
}

// webUI resolves where the web interface is served from.
//
// An empty dir takes the build embedded in the binary. A missing or empty build
// is not an error: internal/api serves its fallback page and the API carries on,
// which is what a fresh clone that has never run npm looks like.
func webUI(dir string) (fs.FS, error) {
	if dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("serve: --ui-dir %s: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("serve: --ui-dir %s is not a directory", dir)
		}
		slog.Info("serving the web interface from disk", "directory", dir)
		return os.DirFS(dir), nil
	}

	embedded, err := fs.Sub(web.Dist, embeddedUIDir)
	if err != nil {
		return nil, fmt.Errorf("serve: read the embedded web interface: %w", err)
	}
	return embedded, nil
}

// startHTTP serves the guide API and the web interface until the returned
// function is called.
//
// The listener is opened before the goroutine starts, so a port already in use
// is reported here and fails the command rather than disappearing into a log
// line nobody reads.
func startHTTP(address string, deps api.Deps) (func(), error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("serve: listen on %s: %w", address, err)
	}

	srv := &http.Server{
		Handler: api.New(deps),
		// The guide is read by phones on the home network and by nothing else.
		// The timeouts are there so a client that opens a connection and goes
		// away, which is what a phone locking its screen looks like, cannot
		// hold a connection open forever.
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}

	stopped := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		stopped <- err
	}()
	slog.Info("guide listening", "address", listener.Addr().String())

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("could not shut the guide down cleanly", "error", err)
		}
		if err := <-stopped; err != nil {
			slog.Error("the guide stopped with an error", "error", err)
		}
	}, nil
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
