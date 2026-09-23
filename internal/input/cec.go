package input

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/tylersavery/channel3/internal/proc"
)

// cecTimeout is how long one cec-ctl invocation may take.
//
// A television that is off, asleep or simply not listening leaves cec-ctl
// waiting, and nothing about the broadcast may wait with it. Three seconds is
// long enough for a set that is slow to answer and short enough that a button
// press does not feel stuck.
const cecTimeout = 3 * time.Second

// cecTarget is the CEC logical address of the television, which is always 0.
const cecTarget = "0"

// CEC is what this service can ask the television to do.
//
// It is deliberately only the things a remote has buttons for and nothing the
// schedule depends on. If CEC turns out not to work on the television in the
// living room, the fallback is a universal remote and none of this is used.
type CEC interface {
	// Power puts the television to sleep, or wakes it if we put it to sleep.
	Power() error
	// VolumeUp raises the volume by one step.
	VolumeUp() error
	// VolumeDown lowers it by one step.
	VolumeDown() error
	// Mute toggles mute.
	Mute() error
}

// Runner runs one external command with a deadline.
//
// It exists so the tests can see exactly what would have been run without a
// television or a /dev/cec0 anywhere near them.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// execRunner runs commands for real.
type execRunner struct{}

// Run executes the command and folds its output into the error, because
// cec-ctl explains itself on stdout rather than in its exit status.
func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// cec-ctl against a television that never answers is exactly the case this
	// deadline exists for, so the deadline has to take the whole process group
	// rather than leave a child holding the output pipe.
	proc.Harden(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, firstLine(text))
	}
	return nil
}

// firstLine is the part of a command's output worth putting in a log line.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// RealCEC drives the television by shelling out to cec-ctl.
//
// Every call is one short lived process, which is how cec-ctl is meant to be
// used and which means nothing is held open between button presses. Errors are
// logged and returned but never stop the broadcast: a television that ignores
// CEC is a television that has to be turned on by hand, not a reason to stop
// playing.
type RealCEC struct {
	binary string
	device string
	log    *slog.Logger
	run    Runner

	mu sync.Mutex
	// awake is the last power state this service asked for. The television's
	// real state is unknowable over CEC without a query the plan does not make,
	// so Power toggles what we last sent. The service starts believing the set
	// is on, because something is playing on it.
	awake bool
}

// NewCEC returns a CEC that drives device through cec-ctl.
func NewCEC(device string, log *slog.Logger) *RealCEC {
	return newCEC(device, log, execRunner{})
}

// newCEC is NewCEC with the command runner supplied, for tests.
func newCEC(device string, log *slog.Logger, run Runner) *RealCEC {
	if log == nil {
		log = slog.Default()
	}
	return &RealCEC{binary: "cec-ctl", device: device, log: log, run: run, awake: true}
}

// Power sends standby or image view on, alternating between the two.
func (c *RealCEC) Power() error {
	c.mu.Lock()
	wake := !c.awake
	c.awake = wake
	c.mu.Unlock()

	action := "--standby"
	if wake {
		action = "--image-view-on"
	}
	return c.send("power", action)
}

// VolumeUp raises the volume.
func (c *RealCEC) VolumeUp() error { return c.userControl("volume-up") }

// VolumeDown lowers it.
func (c *RealCEC) VolumeDown() error { return c.userControl("volume-down") }

// Mute toggles mute.
func (c *RealCEC) Mute() error { return c.userControl("mute") }

// userControl sends a remote button to the television and then releases it.
//
// CEC models a button as held down until it is let go, and a television that
// never gets the release may keep raising the volume. The release is sent even
// when the press failed, for the same reason.
func (c *RealCEC) userControl(command string) error {
	pressErr := c.send(command, "--user-control-pressed", "ui-cmd="+command)
	releaseErr := c.send(command+" release", "--user-control-released")
	if pressErr != nil {
		return pressErr
	}
	return releaseErr
}

// send runs one cec-ctl invocation aimed at the television.
func (c *RealCEC) send(what string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cecTimeout)
	defer cancel()

	full := append([]string{"-d", c.device, "--to", cecTarget}, args...)
	if err := c.run.Run(ctx, c.binary, full...); err != nil {
		// Logged here so every caller does not have to, and so the journal on
		// the Pi names the command that failed rather than just the button.
		c.log.Error("the television did not accept a cec command",
			"button", what, "command", c.binary+" "+strings.Join(full, " "), "error", err)
		return fmt.Errorf("cec: %s: %w", what, err)
	}
	c.log.Info("sent a cec command", "button", what, "device", c.device)
	return nil
}
