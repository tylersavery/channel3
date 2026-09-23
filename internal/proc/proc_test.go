package proc

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// forkAndHang writes a script that leaves a child holding the output pipes, so
// a command that is killed one process at a time never finishes.
func forkAndHang(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "forker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 30 &\nwait\n"), 0o755); err != nil {
		t.Fatalf("write the script: %v", err)
	}
	return path
}

func TestHardenSetsTheProcessGroupAndTheWaitDelay(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "true")
	Harden(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Harden did not put the command in its own process group")
	}
	if cmd.WaitDelay != WaitDelay {
		t.Errorf("WaitDelay = %v, want %v", cmd.WaitDelay, WaitDelay)
	}
	if cmd.Cancel == nil {
		t.Error("Harden left the default cancel in place")
	}
}

// TestHardenedCommandDiesWithItsContext is the whole reason this package
// exists: the deadline has to take the children too.
func TestHardenedCommandDiesWithItsContext(t *testing.T) {
	// Long enough that the script has certainly forked before the deadline, so
	// this measures the process group and not a lucky race.
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, forkAndHang(t))
	cmd.Stdout = &out
	cmd.Stderr = &out
	Harden(cmd)

	started := time.Now()
	if err := cmd.Run(); err == nil {
		t.Fatal("a command killed at its deadline was reported as a success")
	}
	if took := time.Since(started); took > WaitDelay {
		t.Errorf("Run took %s, want the whole process tree to go at the deadline", took)
	}
}

// TestCancelWithNoProcessIsNotAFailure covers a command whose context is done
// before it ever started.
//
// os/exec turns any error from Cancel other than a finished process into the
// error Wait reports, so a command that was never running must not be reported
// as one that failed. The same reasoning covers a kill that finds nothing,
// which cannot be provoked here without signalling a pid this test no longer
// owns.
func TestCancelWithNoProcessIsNotAFailure(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "true")
	Harden(cmd)

	if err := cmd.Cancel(); err != os.ErrProcessDone {
		t.Errorf("cancel before the command started = %v, want %v", err, os.ErrProcessDone)
	}
}
