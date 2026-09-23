//go:build linux

package input

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// eviocgrab is EVIOCGRAB, _IOW('E', 0x90, int).
//
// The constant is written out because golang.org/x/sys/unix does not export it.
// The kernel reads the ioctl argument as a value rather than through the
// pointer the macro implies, so the grab is set with unix.IoctlSetInt.
const eviocgrab = 0x40044590

// ioctlRead is _IOC_READ, the direction bits of an ioctl that fills a buffer.
const ioctlRead = 2

// eviocgbit returns EVIOCGBIT(ev, len): give me the bitmask of codes this
// device can report for event type ev.
func eviocgbit(ev, length int) uint {
	return (ioctlRead << 30) | (uint(length) << 16) | ('E' << 8) | uint(0x20+ev)
}

// keyBitmapBytes is large enough for every KEY_* code the kernel defines
// (KEY_MAX is 0x2ff), which is what EVIOCGBIT is asked to fill.
const keyBitmapBytes = (0x2ff + 8) / 8

// readBufferSize holds a handful of events per read. A remote sends three or
// four per press, so this is never the limit on anything.
const readBufferSize = 16 * eventSize

// Keys reads presses from the device until ctx is cancelled.
//
// The device is reopened with a backoff whenever it goes away, because the
// Flirc can be unplugged from a running Pi and plugged back in, and because a
// USB receiver sometimes reappears as a different event number after a
// suspend. The channel closes only when ctx is done.
func (d *Device) Keys(ctx context.Context) <-chan Key {
	out := make(chan Key)

	go func() {
		defer close(out)

		backoff := reopenInitialBackoff
		for ctx.Err() == nil {
			opened, err := d.session(ctx, out)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				d.log.Warn("the input device is not readable, retrying",
					"device", d.path, "error", err, "retry in", backoff)
			} else {
				d.log.Warn("the input device went away, waiting for it to come back",
					"device", d.path, "retry in", backoff)
			}
			if opened {
				// A device that worked for a while and then vanished is an
				// unplugged receiver rather than a misconfiguration, so the
				// next attempt starts from the short wait again.
				backoff = reopenInitialBackoff
			}

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			if !opened {
				backoff = nextReopenBackoff(backoff)
			}
		}
	}()

	return out
}

// session opens the device once and pumps it until it fails.
//
// It reports whether the device was successfully opened, which is how Keys
// tells "the receiver was unplugged" from "this path has never worked".
func (d *Device) session(ctx context.Context, out chan<- Key) (bool, error) {
	// The device is opened non-blocking so the Go runtime can poll it, which
	// is what lets a Close from another goroutine interrupt a read that is
	// waiting for a keypress.
	fd, err := unix.Open(d.path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, fmt.Errorf("input: open %s: %w", d.path, err)
	}
	file := os.NewFile(uintptr(fd), d.path)
	defer file.Close()

	// The grab stops keypresses reaching the console as well as this service.
	// Without it every digit from the remote is also typed at the login prompt
	// behind mpv, which is both ugly and a way to lock the Pi's tty.
	if err := unix.IoctlSetInt(fd, eviocgrab, 1); err != nil {
		// A device that will not be grabbed is still worth reading. The
		// keypresses reach the console too, which is untidy rather than fatal.
		d.log.Warn("could not take exclusive use of the input device",
			"device", d.path, "error", err)
	}

	name, err := deviceName(d.path)
	if err != nil {
		name = "unknown"
	}
	d.log.Info("reading the remote", "device", d.path, "name", name)

	// Closing the file from here is what unblocks the read below when the
	// service is shutting down.
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			file.Close()
		case <-closed:
		}
	}()

	var dec decoder
	buf := make([]byte, readBufferSize)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			for _, key := range dec.decode(buf[:n]) {
				select {
				case out <- key:
				case <-ctx.Done():
					return true, nil
				}
			}
		}
		if err != nil {
			if errors.Is(err, os.ErrClosed) || ctx.Err() != nil {
				return true, nil
			}
			return true, fmt.Errorf("input: read %s: %w", d.path, err)
		}
	}
}

// DetectDevice returns the input device most likely to be the remote.
//
// A Flirc is preferred by name, because that is the receiver this appliance is
// built around and a Pi may well have a keyboard plugged in beside it. Failing
// that, the first device that can report a channel key or a digit is taken,
// which is any keyboard. A mouse or a power button, which report neither, are
// passed over.
func DetectDevice() (string, error) {
	paths, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return "", fmt.Errorf("input: look for an input device: %w", err)
	}
	if len(paths) == 0 {
		return "", errors.New("input: no /dev/input/event* devices exist")
	}
	sort.Strings(paths)

	var fallback string
	for _, path := range paths {
		name, err := deviceName(path)
		if err == nil && strings.Contains(strings.ToLower(name), "flirc") {
			return path, nil
		}
		if fallback == "" && reportsRemoteKeys(path) {
			fallback = path
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", errors.New("input: no input device reports channel or digit keys")
}

// deviceName reads a device's name from sysfs.
//
// sysfs is used rather than the EVIOCGNAME ioctl so that detection never has to
// open a device it is not going to read, which would take it away from whatever
// else is using it for as long as the file was open.
func deviceName(path string) (string, error) {
	name := filepath.Base(path)
	data, err := os.ReadFile(filepath.Join("/sys/class/input", name, "device", "name"))
	if err != nil {
		return "", fmt.Errorf("input: read the name of %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// reportsRemoteKeys asks the device whether it can send a channel key or a
// digit, which is what tells a keyboard from a mouse.
func reportsRemoteKeys(path string) bool {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer unix.Close(fd)

	bits := make([]byte, keyBitmapBytes)
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(eviocgbit(evKey, len(bits))),
		uintptr(unsafe.Pointer(&bits[0])),
	)
	if errno != 0 {
		return false
	}
	return hasKeyBit(bits, KeyChannelUp) || hasKeyBit(bits, Key0)
}

// hasKeyBit reports whether a key code is set in an EVIOCGBIT bitmap.
func hasKeyBit(bits []byte, code int) bool {
	index := code / 8
	if index >= len(bits) {
		return false
	}
	return bits[index]&(1<<(code%8)) != 0
}
