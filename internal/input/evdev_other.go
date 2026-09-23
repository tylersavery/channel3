//go:build !linux

package input

import (
	"context"
	"errors"
	"runtime"
)

// errNoEvdev says this platform has no evdev.
//
// The service runs on a Pi and is developed on a Mac. On the Mac the terminal
// source in tty.go is the remote, and serve reports this once rather than
// failing to start.
var errNoEvdev = errors.New("input: reading /dev/input is only supported on Linux, this is " + runtime.GOOS)

// Keys reports that this platform has no evdev and closes the channel.
func (d *Device) Keys(ctx context.Context) <-chan Key {
	d.log.Warn("ignoring the input device", "device", d.path, "error", errNoEvdev)
	out := make(chan Key)
	close(out)
	return out
}

// DetectDevice always fails off Linux.
func DetectDevice() (string, error) {
	return "", errNoEvdev
}
