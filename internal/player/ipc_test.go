package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// testLogger discards log output so a failing test reads as assertions rather
// than as mpv chatter.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// dialFake starts a fake mpv and connects to it.
func dialFake(t *testing.T) (*fakeMPV, *ipcConn) {
	t.Helper()
	socket := socketPath(t)
	fake := startFakeMPV(t, socket)
	conn, err := dialIPC(socket, testLogger())
	if err != nil {
		t.Fatalf("dial fake mpv: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("closing the ipc connection: %v", err)
		}
	})
	return fake, conn
}

// TestCommandCorrelatesRepliesUnderConcurrency proves the request id routing:
// every caller gets its own answer even with many commands in flight.
func TestCommandCorrelatesRepliesUnderConcurrency(t *testing.T) {
	fake, conn := dialFake(t)

	const callers = 16
	for i := range callers {
		fake.SetProperty(fmt.Sprintf("prop-%d", i), fmt.Sprintf("value-%d", i))
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, callers)
	got := make([]string, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := conn.Command(ctx, "get_property", fmt.Sprintf("prop-%d", i))
			if err != nil {
				errs[i] = err
				return
			}
			errs[i] = json.Unmarshal(data, &got[i])
		}()
	}
	wg.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if want := fmt.Sprintf("value-%d", i); got[i] != want {
			t.Errorf("caller %d got %q, want %q", i, got[i], want)
		}
	}
}

// TestCommandTurnsMPVErrorIntoGoError checks that mpv's own error text survives
// into the Go error, because that text is what an operator has to act on.
func TestCommandTurnsMPVErrorIntoGoError(t *testing.T) {
	fake, conn := dialFake(t)
	fake.SetPropertyError("time-pos", "property unavailable")

	_, err := conn.Command(t.Context(), "get_property", "time-pos")
	if err == nil {
		t.Fatal("expected an error from an mpv failure reply")
	}
	if !strings.Contains(err.Error(), "property unavailable") {
		t.Errorf("error %q does not carry mpv's text", err)
	}
}

// TestEventsArriveInOrder checks that the reader preserves mpv's ordering,
// which is what lets the station trust an end-file after a start-file.
func TestEventsArriveInOrder(t *testing.T) {
	fake, conn := dialFake(t)

	want := []string{"start-file", "file-loaded", "end-file"}
	for _, name := range want {
		fake.Emit(map[string]any{"event": name})
	}

	for i, name := range want {
		select {
		case ev := <-conn.Events():
			if ev.Name != name {
				t.Fatalf("event %d is %q, want %q", i, ev.Name, name)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d (%s)", i, name)
		}
	}
}

// TestEndFileEventCarriesReasonAndEntry checks the fields the supervisor reads.
func TestEndFileEventCarriesReasonAndEntry(t *testing.T) {
	fake, conn := dialFake(t)
	fake.Emit(map[string]any{"event": "end-file", "reason": "error", "playlist_entry_id": 7, "file_error": "loading failed"})

	select {
	case ev := <-conn.Events():
		if ev.Name != "end-file" || ev.Reason != "error" || ev.EntryID != 7 {
			t.Fatalf("got %+v, want an end-file error event for entry 7", ev)
		}
		if ev.FileError != "loading failed" {
			t.Errorf("file_error is %q, want %q", ev.FileError, "loading failed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the end-file event")
	}
}

// TestCommandTimesOutWhenMPVStopsReplying is the hung mpv case: the context
// deadline has to win, or the whole service stalls behind one command.
func TestCommandTimesOutWhenMPVStopsReplying(t *testing.T) {
	fake, conn := dialFake(t)
	fake.Stall(true)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := conn.Command(ctx, "get_property", "idle-active")
	if err == nil {
		t.Fatal("expected a timeout from a stalled mpv")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("error %q is not a deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("command took %s to time out", elapsed)
	}
}

// TestCommandFailsAfterConnectionCloses checks that a caller is released rather
// than left waiting when mpv goes away.
func TestCommandFailsAfterConnectionCloses(t *testing.T) {
	fake, conn := dialFake(t)
	fake.Stall(true)

	errs := make(chan error, 1)
	go func() {
		_, err := conn.Command(t.Context(), "get_property", "idle-active")
		errs <- err
	}()

	// Give the command time to be sent before the socket is dropped.
	time.Sleep(50 * time.Millisecond)
	fake.CloseConns()

	select {
	case err := <-errs:
		if !errors.Is(err, errConnClosed) {
			t.Fatalf("got %v, want errConnClosed when the connection closed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the caller was not released when the connection closed")
	}

	select {
	case <-conn.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the reader goroutine did not stop")
	}
}
