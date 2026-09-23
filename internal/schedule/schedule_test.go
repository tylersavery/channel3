package schedule

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAtBoundariesAndWrap(t *testing.T) {
	clk := NewClock(time.UTC)
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute)

	day := clk.BroadcastDay(time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC))
	order := Order(ch, day)
	total := totalDuration(order)
	if total != 50*time.Minute {
		t.Fatalf("total duration = %s, want 50m", total)
	}

	// Cumulative start of each item within one pass through the order.
	starts := make([]time.Duration, len(order))
	for i := 1; i < len(order); i++ {
		starts[i] = starts[i-1] + order[i-1].Duration
	}

	tests := []struct {
		name       string
		elapsed    time.Duration
		wantIndex  int
		wantOffset time.Duration
	}{
		{"day start is the first item at zero", 0, 0, 0},
		{"inside the first item", starts[1] / 2, 0, starts[1] / 2},
		{"one nanosecond before the first boundary", starts[1] - 1, 0, order[0].Duration - 1},
		{"exactly on the first boundary", starts[1], 1, 0},
		{"one nanosecond before the second boundary", starts[2] - 1, 1, order[1].Duration - 1},
		{"exactly on the second boundary", starts[2], 2, 0},
		{"exactly on the last boundary", starts[3], 3, 0},
		{"one nanosecond before the loop", total - 1, 3, order[3].Duration - 1},
		{"exactly on the loop is the first item again", total, 0, 0},
		{"one pass and a bit", total + starts[2] + time.Minute, 2, time.Minute},
		{"seventeen passes lands in the same place", 17*total + starts[2] + time.Minute, 2, time.Minute},
		{"twenty passes minus one nanosecond", 20*total - 1, 3, order[3].Duration - 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			when := day.Add(tc.elapsed)
			slot, ok := At(ch, when, clk)
			if !ok {
				t.Fatalf("At(%s) reported no item", when)
			}
			want := order[tc.wantIndex]
			if slot.Item.ID != want.ID {
				t.Errorf("item = %s, want %s", slot.Item.ID, want.ID)
			}
			if slot.Offset != tc.wantOffset {
				t.Errorf("offset = %s, want %s", slot.Offset, tc.wantOffset)
			}
			assertSlotConsistent(t, slot, when)
		})
	}
}

// assertSlotConsistent checks the invariants every slot At returns must hold.
func assertSlotConsistent(t *testing.T, slot Slot, when time.Time) {
	t.Helper()
	if got := slot.End.Sub(slot.Start); got != slot.Item.Duration {
		t.Errorf("End minus Start = %s, want the item duration %s", got, slot.Item.Duration)
	}
	if got := when.Sub(slot.Start); got != slot.Offset {
		t.Errorf("query time is %s into the airing, but Offset is %s", got, slot.Offset)
	}
	if slot.Offset < 0 || slot.Offset >= slot.Item.Duration {
		t.Errorf("offset %s is outside [0, %s)", slot.Offset, slot.Item.Duration)
	}
}

func TestAtEmptyAndUnplayableChannels(t *testing.T) {
	clk := NewClock(time.UTC)
	when := time.Date(2026, time.September, 22, 19, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		ch   Channel
	}{
		{"no items at all", Channel{ID: "empty", Number: 3, Name: "Empty"}},
		{"only zero duration items", buildChannel("zeroes", 0, 0)},
		{"only negative duration items", buildChannel("negative", -time.Minute)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := At(tc.ch, when, clk); ok {
				t.Fatalf("At reported an item for a channel with nothing playable")
			}
		})
	}
}

func TestAtSingleItemLoopsOnItself(t *testing.T) {
	clk := NewClock(time.UTC)
	ch := buildChannel("solo", 7*time.Minute)
	day := clk.BroadcastDay(time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name       string
		elapsed    time.Duration
		wantOffset time.Duration
	}{
		{"day start", 0, 0},
		{"part way through", 3 * time.Minute, 3 * time.Minute},
		{"one nanosecond before the loop", 7*time.Minute - 1, 7*time.Minute - 1},
		{"exactly on the loop", 7 * time.Minute, 0},
		{"ninety nine loops later", 99*7*time.Minute + time.Minute, time.Minute},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			when := day.Add(tc.elapsed)
			slot, ok := At(ch, when, clk)
			if !ok {
				t.Fatalf("At(%s) reported no item", when)
			}
			if slot.Item.ID != ch.Items[0].ID {
				t.Errorf("item = %s, want the only item %s", slot.Item.ID, ch.Items[0].ID)
			}
			if slot.Offset != tc.wantOffset {
				t.Errorf("offset = %s, want %s", slot.Offset, tc.wantOffset)
			}
			assertSlotConsistent(t, slot, when)
		})
	}
}

func TestAtIsRepeatable(t *testing.T) {
	clk := NewClock(mustLoadLocation(t, "America/Toronto"))
	ch := buildChannel("trains", 612437*time.Millisecond, 8*time.Minute, 22*time.Minute, 95*time.Second, 14*time.Minute)
	when := time.Date(2026, time.September, 22, 19, 4, 11, 500, time.UTC)

	first, ok := At(ch, when, clk)
	if !ok {
		t.Fatalf("At reported no item")
	}
	for i := range 20 {
		got, ok := At(ch, when, clk)
		if !ok {
			t.Fatalf("call %d reported no item", i)
		}
		if got.Item.ID != first.Item.ID || got.Offset != first.Offset ||
			!got.Start.Equal(first.Start) || !got.End.Equal(first.End) {
			t.Fatalf("call %d returned %+v, want %+v", i, got, first)
		}
	}
}

// TestAtRollsOverAtFour is the daily reshuffle: the last minute of one broadcast
// day and the first instant of the next answer from different orders.
func TestAtRollsOverAtFour(t *testing.T) {
	toronto := mustLoadLocation(t, "America/Toronto")
	clk := NewClock(toronto)
	ch := buildChannel("trains",
		10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute,
		7*time.Minute, 12*time.Minute, 3*time.Minute, 25*time.Minute)

	before := time.Date(2026, time.September, 22, 3, 59, 59, 0, toronto)
	after := time.Date(2026, time.September, 22, 4, 0, 0, 0, toronto)

	oldOrder := Order(ch, clk.BroadcastDay(before))
	newOrder := Order(ch, clk.BroadcastDay(after))
	if slices.Equal(itemIDs(oldOrder), itemIDs(newOrder)) {
		t.Fatalf("the order either side of 04:00 is the same: %v", itemIDs(newOrder))
	}

	slot, ok := At(ch, after, clk)
	if !ok {
		t.Fatalf("At(04:00) reported no item")
	}
	if slot.Offset != 0 {
		t.Errorf("offset at 04:00 = %s, want 0", slot.Offset)
	}
	if slot.Item.ID != newOrder[0].ID {
		t.Errorf("item at 04:00 = %s, want the new order's first item %s", slot.Item.ID, newOrder[0].ID)
	}
	if !slot.Start.Equal(after) {
		t.Errorf("start at 04:00 = %s, want %s", slot.Start, after)
	}

	priorSlot, ok := At(ch, before, clk)
	if !ok {
		t.Fatalf("At(03:59:59) reported no item")
	}
	if !slices.Contains(itemIDs(oldOrder), priorSlot.Item.ID) {
		t.Errorf("item at 03:59:59 = %s, which is not in the previous day's order", priorSlot.Item.ID)
	}
}

// TestAtAcrossDST walks a minute at a time through both daylight saving
// transitions. Nothing may panic, and every offset must stay inside its item.
func TestAtAcrossDST(t *testing.T) {
	toronto := mustLoadLocation(t, "America/Toronto")
	clk := NewClock(toronto)
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute, 7*time.Minute)

	tests := []struct {
		name string
		from time.Time
	}{
		{"spring forward", time.Date(2026, time.March, 8, 0, 0, 0, 0, toronto)},
		{"fall back", time.Date(2026, time.November, 1, 0, 0, 0, 0, toronto)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for minute := range 8 * 60 {
				when := tc.from.Add(time.Duration(minute) * time.Minute)
				slot, ok := At(ch, when, clk)
				if !ok {
					t.Fatalf("At(%s) reported no item", when)
				}
				assertSlotConsistent(t, slot, when)
			}
		})
	}
}

// buildChannel makes a channel whose items have the given durations and
// predictable ids, so a test can talk about order without inventing fixtures.
func buildChannel(id string, durations ...time.Duration) Channel {
	items := make([]Item, len(durations))
	for i, d := range durations {
		items[i] = Item{
			ID:       fmt.Sprintf("%s-%02d", id, i+1),
			Title:    fmt.Sprintf("%s episode %d", strings.ToUpper(id), i+1),
			Path:     fmt.Sprintf("/library/%s/%02d.mp4", id, i+1),
			Duration: d,
		}
	}
	return Channel{ID: id, Number: 3, Name: strings.ToUpper(id) + " TV", Items: items}
}

// itemIDs is the id of every item, in order.
func itemIDs(items []Item) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}
