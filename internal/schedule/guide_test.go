package schedule

import (
	"slices"
	"testing"
	"time"
)

func TestGuideCoversTheHorizon(t *testing.T) {
	toronto := mustLoadLocation(t, "America/Toronto")
	clk := NewClock(toronto)
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute, 7*time.Minute)
	from := time.Date(2026, time.September, 22, 19, 4, 11, 0, toronto)

	tests := []struct {
		name    string
		horizon time.Duration
	}{
		{"one hour", time.Hour},
		{"six hours", 6 * time.Hour},
		{"a day and a half", 36 * time.Hour},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			slots := Guide(ch, from, tc.horizon, clk)
			if len(slots) == 0 {
				t.Fatalf("guide is empty")
			}
			assertGuideContiguous(t, slots, from)
			if want := from.Add(tc.horizon); slots[len(slots)-1].End.Before(want) {
				t.Errorf("guide ends at %s, which is short of the horizon %s", slots[len(slots)-1].End, want)
			}
		})
	}
}

func TestGuideFirstSlotIsTheCurrentAiring(t *testing.T) {
	toronto := mustLoadLocation(t, "America/Toronto")
	clk := NewClock(toronto)
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute, 7*time.Minute)
	from := time.Date(2026, time.September, 22, 19, 4, 11, 0, toronto)

	now, ok := At(ch, from, clk)
	if !ok {
		t.Fatalf("At reported no item")
	}
	slots := Guide(ch, from, 2*time.Hour, clk)
	if slots[0].Item.ID != now.Item.ID || slots[0].Offset != now.Offset || !slots[0].Start.Equal(now.Start) {
		t.Fatalf("first slot = %+v, want the current airing %+v", slots[0], now)
	}
	if slots[0].Offset == 0 {
		t.Errorf("this fixture should start part way into an item, but the offset is zero")
	}
	if !slots[0].Start.Before(from) {
		t.Errorf("first slot starts at %s, want earlier than %s", slots[0].Start, from)
	}
}

// TestGuideSwitchesOrderAtFour is the 04:00 reshuffle seen from the guide: the
// airing in progress is cut off at 04:00 and the new day's order starts there.
func TestGuideSwitchesOrderAtFour(t *testing.T) {
	toronto := mustLoadLocation(t, "America/Toronto")
	clk := NewClock(toronto)
	ch := buildChannel("trains",
		10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute,
		7*time.Minute, 12*time.Minute, 3*time.Minute, 25*time.Minute)

	from := time.Date(2026, time.September, 22, 3, 30, 0, 0, toronto)
	rollover := time.Date(2026, time.September, 22, 4, 0, 0, 0, toronto)
	slots := Guide(ch, from, 2*time.Hour, clk)
	assertGuideContiguous(t, slots, from)

	cross := slices.IndexFunc(slots, func(s Slot) bool { return !s.Start.Before(rollover) })
	if cross <= 0 {
		t.Fatalf("no slot starts at or after the rollover: %d slots from %s", len(slots), from)
	}
	if !slots[cross].Start.Equal(rollover) {
		t.Fatalf("the first slot of the new day starts at %s, want exactly %s", slots[cross].Start, rollover)
	}
	if !slots[cross-1].End.Equal(rollover) {
		t.Fatalf("the last slot of the old day ends at %s, want exactly %s", slots[cross-1].End, rollover)
	}

	cut := slots[cross-1]
	if got := cut.End.Sub(cut.Start); got >= cut.Item.Duration {
		t.Errorf("this fixture should be cut off by the rollover, but %s ran its full %s", cut.Item.ID, got)
	}

	oldOrder := itemIDs(Order(ch, clk.BroadcastDay(from)))
	newOrder := itemIDs(Order(ch, clk.BroadcastDay(rollover)))
	if got := slots[cross].Item.ID; got != newOrder[0] {
		t.Errorf("first item after the rollover = %s, want the new order's first item %s", got, newOrder[0])
	}
	for i, slot := range slots[:cross] {
		if !slices.Contains(oldOrder, slot.Item.ID) {
			t.Errorf("slot %d (%s) is not in the previous day's order", i, slot.Item.ID)
		}
	}
	for i, slot := range slots[cross:] {
		if !slices.Contains(newOrder, slot.Item.ID) {
			t.Errorf("slot %d after the rollover (%s) is not in the new day's order", i, slot.Item.ID)
		}
	}
}

func TestGuideFollowsTheDayOrder(t *testing.T) {
	clk := NewClock(time.UTC)
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute)
	day := clk.BroadcastDay(time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC))
	order := itemIDs(Order(ch, day))

	slots := Guide(ch, day, 100*time.Minute, clk)
	got := itemIDs(slotItems(slots))

	// 100 minutes covers two full passes of the 50 minute order.
	want := append(slices.Clone(order), order...)
	if len(got) < len(want) {
		t.Fatalf("guide has %d slots, want at least %d", len(got), len(want))
	}
	if !slices.Equal(got[:len(want)], want) {
		t.Fatalf("guide order = %v, want %v", got[:len(want)], want)
	}
}

func TestGuideEdgeCases(t *testing.T) {
	clk := NewClock(time.UTC)
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute)
	from := time.Date(2026, time.September, 22, 19, 4, 11, 0, time.UTC)

	t.Run("a channel with nothing playable has no guide", func(t *testing.T) {
		if got := Guide(Channel{ID: "empty"}, from, 6*time.Hour, clk); got != nil {
			t.Fatalf("guide = %v, want nil", got)
		}
	})

	t.Run("a zero horizon still reports what is on now", func(t *testing.T) {
		got := Guide(ch, from, 0, clk)
		if len(got) != 1 {
			t.Fatalf("guide has %d slots, want 1", len(got))
		}
	})

	t.Run("a negative horizon still reports what is on now", func(t *testing.T) {
		got := Guide(ch, from, -time.Hour, clk)
		if len(got) != 1 {
			t.Fatalf("guide has %d slots, want 1", len(got))
		}
	})

	t.Run("a single item channel repeats itself", func(t *testing.T) {
		solo := buildChannel("solo", 7*time.Minute)
		got := Guide(solo, from, 30*time.Minute, clk)
		if len(got) < 5 {
			t.Fatalf("guide has %d slots, want at least 5", len(got))
		}
		assertGuideContiguous(t, got, from)
		for i, slot := range got {
			if slot.Item.ID != solo.Items[0].ID {
				t.Fatalf("slot %d = %s, want the only item %s", i, slot.Item.ID, solo.Items[0].ID)
			}
		}
	})
}

// assertGuideContiguous checks the guide's shape: it starts on the airing that
// covers from, every slot butts up against the next, and only the first is part
// way through.
func assertGuideContiguous(t *testing.T, slots []Slot, from time.Time) {
	t.Helper()
	if len(slots) == 0 {
		t.Fatal("guide is empty")
	}
	first := slots[0]
	if first.Start.After(from) || !first.End.After(from) {
		t.Errorf("first slot %s to %s does not cover %s", first.Start, first.End, from)
	}
	for i, slot := range slots {
		if !slot.End.After(slot.Start) {
			t.Errorf("slot %d ends at %s, which is not after its start %s", i, slot.End, slot.Start)
		}
		if got := slot.End.Sub(slot.Start); got > slot.Item.Duration {
			t.Errorf("slot %d lasts %s, longer than its item's %s", i, got, slot.Item.Duration)
		}
		if i == 0 {
			continue
		}
		if !slot.Start.Equal(slots[i-1].End) {
			t.Errorf("slot %d starts at %s, want the previous slot's end %s", i, slot.Start, slots[i-1].End)
		}
		if slot.Offset != 0 {
			t.Errorf("slot %d has offset %s, want zero for every slot but the first", i, slot.Offset)
		}
	}
}

// slotItems is the item of every slot, in order.
func slotItems(slots []Slot) []Item {
	items := make([]Item, len(slots))
	for i, slot := range slots {
		items[i] = slot.Item
	}
	return items
}
