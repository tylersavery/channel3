package input

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fakeRunner records commands instead of running them. No test in this package
// starts cec-ctl or touches a /dev/cec device.
type fakeRunner struct {
	calls []string
	// fail makes every call fail, which is what a Pi with no CEC adapter does.
	fail error
	// deadlines records how long each call was given.
	deadlines []time.Duration
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if deadline, ok := ctx.Deadline(); ok {
		f.deadlines = append(f.deadlines, time.Until(deadline).Round(time.Second))
	}
	return f.fail
}

// quietCEC builds a CEC over the fake runner with logging discarded.
func quietCEC(run Runner) *RealCEC {
	return newCEC("/dev/cec0", slog.New(slog.NewTextHandler(io.Discard, nil)), run)
}

// TestPowerAlternates is the whole of the power button: the service cannot ask
// the television what state it is in, so it toggles what it last sent, starting
// from the assumption that a set showing a broadcast is on.
func TestPowerAlternates(t *testing.T) {
	run := &fakeRunner{}
	cec := quietCEC(run)

	for range 2 {
		if err := cec.Power(); err != nil {
			t.Fatalf("power: %v", err)
		}
	}

	want := []string{
		"cec-ctl -d /dev/cec0 --to 0 --standby",
		"cec-ctl -d /dev/cec0 --to 0 --image-view-on",
	}
	assertCalls(t, run.calls, want)
}

// TestVolumeSendsPressThenRelease covers CEC modelling a button as held down
// until it is let go.
func TestVolumeSendsPressThenRelease(t *testing.T) {
	tests := []struct {
		name string
		call func(*RealCEC) error
		want []string
	}{
		{
			name: "volume up",
			call: (*RealCEC).VolumeUp,
			want: []string{
				"cec-ctl -d /dev/cec0 --to 0 --user-control-pressed ui-cmd=volume-up",
				"cec-ctl -d /dev/cec0 --to 0 --user-control-released",
			},
		},
		{
			name: "volume down",
			call: (*RealCEC).VolumeDown,
			want: []string{
				"cec-ctl -d /dev/cec0 --to 0 --user-control-pressed ui-cmd=volume-down",
				"cec-ctl -d /dev/cec0 --to 0 --user-control-released",
			},
		},
		{
			name: "mute",
			call: (*RealCEC).Mute,
			want: []string{
				"cec-ctl -d /dev/cec0 --to 0 --user-control-pressed ui-cmd=mute",
				"cec-ctl -d /dev/cec0 --to 0 --user-control-released",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := &fakeRunner{}
			if err := tc.call(quietCEC(run)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			assertCalls(t, run.calls, tc.want)
		})
	}
}

// TestFailingCommandIsReportedNotFatal is the rule for a television that does
// not answer: the caller gets an error it is free to ignore, and the button
// still releases.
func TestFailingCommandIsReportedNotFatal(t *testing.T) {
	boom := errors.New("cec-ctl: no such device")
	run := &fakeRunner{fail: boom}
	cec := quietCEC(run)

	err := cec.VolumeUp()
	if err == nil {
		t.Fatal("a failing cec-ctl was reported as success")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error is %v, want it to wrap the command failure", err)
	}
	if len(run.calls) != 2 {
		t.Errorf("a failed press sent %d commands, want the release to follow anyway", len(run.calls))
	}

	// A failed power command still counts as the state we asked for, so the
	// next press sends the other one rather than repeating a command the
	// television has already ignored once.
	if err := cec.Power(); err == nil {
		t.Error("a failing power command was reported as success")
	}
	if got := run.calls[len(run.calls)-1]; !strings.HasSuffix(got, "--standby") {
		t.Errorf("the first power press sent %q, want standby", got)
	}
}

// TestEveryCallHasADeadline is what keeps a wedged cec-ctl away from the
// broadcast loop.
func TestEveryCallHasADeadline(t *testing.T) {
	run := &fakeRunner{}
	cec := quietCEC(run)

	if err := cec.Power(); err != nil {
		t.Fatalf("power: %v", err)
	}
	if len(run.deadlines) != 1 {
		t.Fatalf("recorded %d deadlines, want one per command", len(run.deadlines))
	}
	if run.deadlines[0] != cecTimeout {
		t.Errorf("the command was given %s, want %s", run.deadlines[0], cecTimeout)
	}
}

// assertCalls compares what was run with what should have been.
func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ran %d commands, want %d:\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d is %q, want %q", i, got[i], want[i])
		}
	}
}
