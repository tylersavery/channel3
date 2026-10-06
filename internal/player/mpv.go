package player

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// socketPathLimit is the longest Unix socket path that reliably works.
//
// The kernel struct behind a Unix socket address has a fixed-size path field:
// 104 bytes on macOS, 108 on Linux. A longer path fails at bind time with a
// message that does not mention length, so the launcher checks it up front.
const socketPathLimit = 100

// runtimeDirName is the directory this service keeps its socket and its
// extracted Stand By card in, inside the system runtime or temporary directory.
const runtimeDirName = "channel3"

// Process is a running mpv, as the supervisor sees it.
type Process interface {
	// Wait blocks until the process exits.
	Wait() error
	// Kill stops the process. It is safe to call after the process has exited.
	Kill() error
}

// Launcher starts mpv listening on an IPC socket.
//
// The supervisor owns the socket path and the restart policy; a Launcher only
// knows how to start one process. Tests substitute a launcher that serves a
// fake socket, so no test starts a real mpv.
type Launcher interface {
	Launch(ctx context.Context, socket string) (Process, error)
}

// BaseArgs are the mpv flags Channel Three always sets.
//
// The television shows video and nothing else: no on-screen controller, no OSD
// of mpv's own, no keyboard bindings of mpv's own, and no terminal output. The
// one exception is ShowText, which the station uses for the channel number;
// the osd-* styling below is that number's look, a retro green in the top
// right corner. idle=yes keeps mpv
// alive between files so the supervisor holds one process for the life of the
// service, and keep-open=no makes a finished file end rather than freeze on its
// last frame. image-display-duration=inf is what holds the Stand By card up
// until the next load.
func BaseArgs(socket string) []string {
	return []string{
		"--fullscreen",
		"--no-osc",
		"--no-osd-bar",
		"--osd-level=0",
		"--osd-font=DejaVu Sans Mono",
		"--osd-bold=yes",
		"--osd-font-size=120",
		"--osd-color=#33FF33",
		"--osd-border-color=#000000",
		"--osd-border-size=4",
		"--osd-align-x=right",
		"--osd-align-y=top",
		"--osd-margin-x=60",
		"--osd-margin-y=45",
		"--no-input-default-bindings",
		"--input-vo-keyboard=no",
		"--no-terminal",
		"--idle=yes",
		"--keep-open=no",
		"--hr-seek=yes",
		"--image-display-duration=inf",
		// A song plays to a black screen, never to its embedded cover art.
		"--audio-display=no",
		"--input-ipc-server=" + socket,
	}
}

// MPVLauncher starts the real mpv binary.
type MPVLauncher struct {
	// Binary is the mpv executable, looked up on PATH when it has no separator.
	Binary string
	// Extra holds the --mpv-arg values, appended after the base flags so an
	// operator can override any of them and Phase 9 can add the Pi's DRM flags.
	Extra []string
}

// Launch starts mpv on socket. Cancelling ctx kills the process.
func (l MPVLauncher) Launch(ctx context.Context, socket string) (Process, error) {
	binary := l.Binary
	if binary == "" {
		binary = "mpv"
	}
	if len(socket) > socketPathLimit {
		return nil, fmt.Errorf("player: socket path %s is %d bytes, over the %d byte limit for a unix socket",
			socket, len(socket), socketPathLimit)
	}
	// mpv refuses to bind a socket path that already exists, which is what a
	// previous process that was killed rather than quit leaves behind.
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("player: clear stale socket %s: %w", socket, err)
	}

	args := append(BaseArgs(socket), l.Extra...)
	cmd := exec.CommandContext(ctx, binary, args...)
	// mpv writes nothing useful to stdout with --no-terminal, and the service
	// owns its own logging, so both streams go nowhere.
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("player: start %s: %w", binary, err)
	}
	return &execProcess{cmd: cmd}, nil
}

// execProcess is a real mpv process.
type execProcess struct {
	cmd *exec.Cmd
}

// Wait blocks until mpv exits.
//
// An exit status is how mpv reports that it quit, so a non-zero status is not
// wrapped as an error here; the supervisor decides what to do about it.
func (p *execProcess) Wait() error {
	err := p.cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}

// Kill stops mpv. A process that has already exited is not an error.
func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	err := p.cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

// RuntimeDir returns the directory for this service's socket and Stand By card,
// creating it if it does not exist.
//
// XDG_RUNTIME_DIR is the right place on the Pi, where systemd provides one that
// is cleaned up at logout. The temporary directory is the fallback everywhere
// else, including a developer's Mac.
func RuntimeDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, runtimeDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("player: create runtime dir %s: %w", dir, err)
	}
	return dir, nil
}

// SocketPath returns the IPC socket path for this process inside dir.
//
// The process id is in the name so two services on one machine, or a restarted
// service whose old socket file is still lying around, never collide.
func SocketPath(dir string) string {
	return filepath.Join(dir, "mpv-"+strconv.Itoa(os.Getpid())+".sock")
}

// minimumVersion is the oldest mpv this service supports.
//
// mpv 0.38 changed loadfile to take an index before the options string, and the
// supervisor sends that four argument form. On anything older the options would
// be read as the index and every load would start from the beginning.
var minimumVersion = version{major: 0, minor: 38}

// version is an mpv version, to the minor component.
type version struct {
	major int
	minor int
}

// String renders a version the way mpv writes it.
func (v version) String() string { return fmt.Sprintf("%d.%d", v.major, v.minor) }

// olderThan reports whether v predates other.
func (v version) olderThan(other version) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	return v.minor < other.minor
}

// parseVersion reads the string mpv reports for the mpv-version property, which
// looks like "mpv v0.41.0" or "mpv v0.38.0-dirty".
func parseVersion(s string) (version, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return version{}, fmt.Errorf("player: mpv reported an empty version")
	}
	raw := strings.TrimPrefix(fields[len(fields)-1], "v")
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) < 2 {
		return version{}, fmt.Errorf("player: cannot read an mpv version from %q", s)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return version{}, fmt.Errorf("player: cannot read the major version from %q: %w", s, err)
	}
	minor, err := strconv.Atoi(strings.SplitN(parts[1], "-", 2)[0])
	if err != nil {
		return version{}, fmt.Errorf("player: cannot read the minor version from %q: %w", s, err)
	}
	return version{major: major, minor: minor}, nil
}
