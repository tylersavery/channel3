// Package proc makes an external command die when its context does.
//
// Channel Three shells out to yt-dlp, ffprobe and cec-ctl, and every one of
// those tools starts children of its own: yt-dlp runs ffmpeg to merge a
// download, cec-ctl forks while it waits on the bus. A plain
// exec.CommandContext kills only the process it started, so the grandchild
// lives on holding the write end of the pipes the parent's output was being
// read through, and Run blocks until that grandchild finishes on its own. The
// timeout then bounds nothing: an ingest that was meant to give up after two
// minutes waits for however long ffmpeg takes.
//
// This package targets macOS and Linux, which are the only two machines
// Channel Three runs on: the Mac it is developed and ingested on and the Pi it
// broadcasts from. Setpgid is a Unix field and this file does not build
// anywhere else.
package proc

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// WaitDelay is how long Wait may spend on the command's pipes after the process
// itself has gone.
//
// Killing the process group should close them at once, so reaching this delay
// means a child escaped its group, which is rare and which nothing here can do
// anything about. Two seconds is long enough that a slow machine does not trip
// it and short enough that a stuck ingest still returns.
const WaitDelay = 2 * time.Second

// Harden makes cmd's whole process tree die when its context is done.
//
// It must be called on a command built by exec.CommandContext, and before the
// command is started. The command runs in its own process group, cancellation
// signals that whole group rather than the one process, and Wait stops waiting
// on the pipes shortly after.
func Harden(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = WaitDelay
}

// killGroup signals the command's process group.
//
// The negative pid is the group, which is the point: every child the tool
// started is in it. A group that has already gone is reported as done rather
// than as a failure, because os/exec turns any other error from Cancel into the
// error Wait returns and a race between the deadline and a clean exit is not a
// failure of the command.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
