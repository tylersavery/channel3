package schedule

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// updateGolden rewrites the golden order from the current implementation.
//
// The golden file was generated once by this implementation and then frozen. It
// exists so that a change to the seed, the generator or the shuffle fails a test
// instead of silently reshuffling every channel on the Pi. Regenerate it only
// when such a change is deliberate:
//
//	go test ./internal/schedule/ -run TestOrderGolden -update
//
// then commit the new file as part of the change that caused it.
var updateGolden = flag.Bool("update", false, "rewrite testdata/golden_order.json from the current implementation")

const goldenPath = "testdata/golden_order.json"

// goldenOrder is the frozen fixture: the channel and items that go in, and the
// order that must come out.
type goldenOrder struct {
	ChannelID string       `json:"channel_id"`
	Day       string       `json:"day"`
	Items     []goldenItem `json:"items"`
	Order     []string     `json:"order"`
}

type goldenItem struct {
	ID              string  `json:"id"`
	DurationSeconds float64 `json:"duration_seconds"`
}

func TestOrderGolden(t *testing.T) {
	golden := readGolden(t)

	day, err := time.Parse(dayLayout, golden.Day)
	if err != nil {
		t.Fatalf("parse golden day %q: %v", golden.Day, err)
	}

	ch := Channel{ID: golden.ChannelID, Number: 3, Name: "Golden"}
	for _, gi := range golden.Items {
		ch.Items = append(ch.Items, Item{
			ID:       gi.ID,
			Title:    gi.ID,
			Path:     "/library/" + golden.ChannelID + "/" + gi.ID + ".mp4",
			Duration: time.Duration(math.Round(gi.DurationSeconds * float64(time.Second))),
		})
	}

	got := itemIDs(Order(ch, day))

	if *updateGolden {
		golden.Order = got
		writeGolden(t, golden)
		t.Logf("rewrote %s", goldenPath)
	}

	if !slices.Equal(got, golden.Order) {
		t.Fatalf("order for %s on %s changed.\n got: %v\nwant: %v\nIf this change is deliberate, regenerate with: go test ./internal/schedule/ -run TestOrderGolden -update",
			golden.ChannelID, golden.Day, got, golden.Order)
	}
}

func readGolden(t *testing.T) goldenOrder {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden goldenOrder
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if len(golden.Items) == 0 {
		t.Fatalf("golden %s has no items", goldenPath)
	}
	return golden
}

func writeGolden(t *testing.T, golden goldenOrder) {
	t.Helper()
	data, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
		t.Fatalf("create golden directory: %v", err)
	}
	if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}

func TestOrderIsStableForTheSameDay(t *testing.T) {
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute, 7*time.Minute)
	day := time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC)

	want := itemIDs(Order(ch, day))
	for i := range 20 {
		if got := itemIDs(Order(ch, day)); !slices.Equal(got, want) {
			t.Fatalf("call %d gave %v, want %v", i, got, want)
		}
	}

	// The same date in another zone is the same broadcast day for seeding.
	toronto := mustLoadLocation(t, "America/Toronto")
	elsewhere := time.Date(2026, time.September, 22, 4, 0, 0, 0, toronto)
	if got := itemIDs(Order(ch, elsewhere)); !slices.Equal(got, want) {
		t.Fatalf("the same date in %s gave %v, want %v", toronto, got, want)
	}
}

func TestOrderChangesWithSeedInputs(t *testing.T) {
	ch := buildChannel("trains",
		10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute,
		7*time.Minute, 12*time.Minute, 3*time.Minute, 25*time.Minute)
	day := time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC)
	base := itemIDs(Order(ch, day))

	tests := []struct {
		name string
		ch   Channel
		day  time.Time
	}{
		{"the next broadcast day", ch, day.AddDate(0, 0, 1)},
		{"the previous broadcast day", ch, day.AddDate(0, 0, -1)},
		{"another channel on the same day", Channel{ID: "space", Items: ch.Items}, day},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := itemIDs(Order(tc.ch, tc.day))
			if slices.Equal(got, base) {
				t.Fatalf("order is unchanged from the base case: %v", got)
			}
			assertPermutation(t, got, itemIDs(ch.Items))
		})
	}
}

func TestOrderDoesNotMutateTheChannel(t *testing.T) {
	ch := buildChannel("trains", 10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute, 7*time.Minute)
	before := itemIDs(ch.Items)
	day := time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC)

	order := Order(ch, day)
	if got := itemIDs(ch.Items); !slices.Equal(got, before) {
		t.Fatalf("Order reordered the caller's items: %v, want %v", got, before)
	}

	// Writing through the returned slice must not reach the channel either.
	order[0] = Item{ID: "clobbered"}
	if got := itemIDs(ch.Items); !slices.Equal(got, before) {
		t.Fatalf("writing to the returned order changed the channel: %v, want %v", got, before)
	}
}

func TestOrderDropsUnplayableItems(t *testing.T) {
	ch := buildChannel("trains", 10*time.Minute, 0, 20*time.Minute, -5*time.Minute, 7*time.Minute)
	day := time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC)

	got := itemIDs(Order(ch, day))
	want := []string{"trains-01", "trains-03", "trains-05"}
	assertPermutation(t, got, want)
}

func TestOrderEdgeCases(t *testing.T) {
	day := time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		ch   Channel
		want []string
	}{
		{"no items", Channel{ID: "empty"}, nil},
		{"one item", buildChannel("solo", 7*time.Minute), []string{"solo-01"}},
		{"one playable item among rejects", buildChannel("mixed", 0, 7*time.Minute, -1), []string{"mixed-02"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := itemIDs(Order(tc.ch, day))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOrderAfterRemovingAnItem is the brainstorm's "a removed item shifts the
// order predictably": the day reshuffles, but nothing is lost or duplicated and
// the day is shorter by exactly the removed item.
func TestOrderAfterRemovingAnItem(t *testing.T) {
	ch := buildChannel("trains",
		10*time.Minute, 5*time.Minute, 20*time.Minute, 15*time.Minute,
		7*time.Minute, 12*time.Minute, 3*time.Minute, 25*time.Minute)
	day := time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC)

	full := Order(ch, day)
	fullTotal := totalDuration(full)

	removed := ch.Items[3]
	shorter := Channel{ID: ch.ID, Number: ch.Number, Name: ch.Name}
	shorter.Items = append(shorter.Items, ch.Items[:3]...)
	shorter.Items = append(shorter.Items, ch.Items[4:]...)

	got := Order(shorter, day)
	if len(got) != len(full)-1 {
		t.Fatalf("order has %d items, want %d", len(got), len(full)-1)
	}
	assertPermutation(t, itemIDs(got), itemIDs(shorter.Items))
	if slices.Contains(itemIDs(got), removed.ID) {
		t.Errorf("removed item %s is still in the order", removed.ID)
	}
	if got, want := totalDuration(got), fullTotal-removed.Duration; got != want {
		t.Errorf("total duration = %s, want %s", got, want)
	}

	// The remaining items are reshuffled, not merely closed up around the gap.
	withoutRemoved := slices.DeleteFunc(itemIDs(full), func(id string) bool { return id == removed.ID })
	if slices.Equal(itemIDs(got), withoutRemoved) {
		t.Errorf("the shorter order is the full order with the item cut out: %v", itemIDs(got))
	}
}

// assertPermutation checks that got holds exactly the same ids as want, in any
// order, each exactly once.
func assertPermutation(t *testing.T, got, want []string) {
	t.Helper()
	gotSorted := slices.Sorted(slices.Values(got))
	wantSorted := slices.Sorted(slices.Values(want))
	if !slices.Equal(gotSorted, wantSorted) {
		t.Fatalf("items are %v, want exactly %v once each", gotSorted, wantSorted)
	}
}
