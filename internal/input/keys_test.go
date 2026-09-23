package input

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// fakeSource is a source the test feeds by hand.
//
// It honours the contract every real source keeps: the channel it returns is
// closed when the context is cancelled or when the source has nothing more to
// give, which is what lets Merge know when to close its own.
type fakeSource struct {
	in chan Key
}

func newFakeSource() *fakeSource { return &fakeSource{in: make(chan Key)} }

func (f *fakeSource) Keys(ctx context.Context) <-chan Key {
	out := make(chan Key)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case key, ok := <-f.in:
				if !ok {
					return
				}
				select {
				case out <- key:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// TestMergeFansIn is the point of the thing: a remote and a keyboard are one
// stream by the time the station sees them.
func TestMergeFansIn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	remote, keyboard := newFakeSource(), newFakeSource()
	merged := Merge(ctx, remote, keyboard)

	remote.in <- Key{Action: ChannelUp}
	keyboard.in <- Key{Action: Digit, Digit: 7}
	remote.in <- Key{Action: Mute}

	got := map[Key]int{}
	for range 3 {
		select {
		case key := <-merged:
			got[key]++
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d keys came through the merge", len(got))
		}
	}

	for _, want := range []Key{{Action: ChannelUp}, {Action: Digit, Digit: 7}, {Action: Mute}} {
		if got[want] != 1 {
			t.Errorf("%v arrived %d times, want once", want, got[want])
		}
	}
}

// TestMergeSkipsNilSources covers a device that could not be found, which serve
// passes along as nothing at all.
func TestMergeSkipsNilSources(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	keyboard := newFakeSource()
	merged := Merge(ctx, nil, keyboard, nil)

	keyboard.in <- Key{Action: ChannelDown}
	select {
	case got := <-merged:
		if got.Action != ChannelDown {
			t.Errorf("got %v, want a channel down", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing came through a merge with nil sources")
	}
}

// TestMergeWithNoSourcesClosesAtOnce is what --no-input and a Pi with no remote
// both look like: a stream that is over before it starts, so the station knows
// not to wait on it.
func TestMergeWithNoSourcesClosesAtOnce(t *testing.T) {
	merged := Merge(t.Context())
	select {
	case _, ok := <-merged:
		if ok {
			t.Error("a merge of no sources produced a key")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a merge of no sources never closed")
	}
}

// TestMergeClosesOnlyWhenEverySourceHas guards against one unplugged receiver
// taking the keyboard down with it.
func TestMergeClosesOnlyWhenEverySourceHas(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	remote, keyboard := newFakeSource(), newFakeSource()
	merged := Merge(ctx, remote, keyboard)

	close(remote.in)
	select {
	case _, ok := <-merged:
		if !ok {
			t.Fatal("the merge closed while a source was still open")
		}
		t.Fatal("the merge produced a key nobody sent")
	case <-time.After(50 * time.Millisecond):
	}

	// The keyboard still works with the remote gone.
	keyboard.in <- Key{Action: ChannelUp}
	select {
	case got := <-merged:
		if got.Action != ChannelUp {
			t.Errorf("got %v, want a channel up", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the surviving source stopped delivering")
	}

	close(keyboard.in)
	select {
	case _, ok := <-merged:
		if ok {
			t.Error("the merge produced a key after every source closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the merge never closed after every source closed")
	}
}

// TestMergeLeavesNoGoroutines is what keeps a service that runs for months from
// growing: cancelling the context has to take every pump down with it.
func TestMergeLeavesNoGoroutines(t *testing.T) {
	settle(t)
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(t.Context())
	merged := Merge(ctx, newFakeSource(), newFakeSource(), newFakeSource())

	cancel()
	select {
	case _, ok := <-merged:
		if ok {
			t.Error("the merge produced a key after the context was cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the merge never closed after the context was cancelled")
	}

	settle(t)
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("%d goroutines are still running, want no more than the %d there were before", after, before)
	}
}

// settle waits for goroutines that are on their way out to finish.
func settle(t *testing.T) {
	t.Helper()
	baseline := runtime.NumGoroutine()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		now := runtime.NumGoroutine()
		if now == baseline {
			return
		}
		baseline = now
	}
}
