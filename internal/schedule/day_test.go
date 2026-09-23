package schedule

import (
	"testing"
	"time"

	// The DST tables are the point of this file, so the test binary carries its
	// own copy of the zone database rather than depending on the host having one.
	_ "time/tzdata"
)

func TestBroadcastDay(t *testing.T) {
	toronto := mustLoadLocation(t, "America/Toronto")

	tests := []struct {
		name string
		clk  Clock
		in   time.Time
		want time.Time
	}{
		{
			name: "afternoon belongs to its own calendar day",
			clk:  NewClock(time.UTC),
			in:   time.Date(2026, time.September, 22, 19, 4, 11, 0, time.UTC),
			want: time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC),
		},
		{
			name: "exactly 04:00 starts its own day",
			clk:  NewClock(time.UTC),
			in:   time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC),
			want: time.Date(2026, time.September, 22, 4, 0, 0, 0, time.UTC),
		},
		{
			name: "one nanosecond before 04:00 belongs to the previous day",
			clk:  NewClock(time.UTC),
			in:   time.Date(2026, time.September, 22, 3, 59, 59, 999999999, time.UTC),
			want: time.Date(2026, time.September, 21, 4, 0, 0, 0, time.UTC),
		},
		{
			name: "small hours belong to the previous day",
			clk:  NewClock(toronto),
			in:   time.Date(2026, time.September, 22, 1, 30, 0, 0, toronto),
			want: time.Date(2026, time.September, 21, 4, 0, 0, 0, toronto),
		},
		{
			name: "the first of the month rolls back into the previous month",
			clk:  NewClock(toronto),
			in:   time.Date(2026, time.October, 1, 2, 0, 0, 0, toronto),
			want: time.Date(2026, time.September, 30, 4, 0, 0, 0, toronto),
		},
		{
			name: "new year's morning rolls back into the previous year",
			clk:  NewClock(toronto),
			in:   time.Date(2026, time.January, 1, 3, 0, 0, 0, toronto),
			want: time.Date(2025, time.December, 31, 4, 0, 0, 0, toronto),
		},
		{
			name: "an instant in another zone is converted before the date is read",
			clk:  NewClock(toronto),
			in:   time.Date(2026, time.September, 22, 7, 0, 0, 0, time.UTC), // 03:00 in Toronto
			want: time.Date(2026, time.September, 21, 4, 0, 0, 0, toronto),
		},
		{
			name: "a midnight day start is taken literally",
			clk:  Clock{Location: time.UTC},
			in:   time.Date(2026, time.September, 22, 3, 0, 0, 0, time.UTC),
			want: time.Date(2026, time.September, 22, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "a day start with minutes and seconds keeps them",
			clk:  Clock{Location: time.UTC, DayStart: 4*time.Hour + 30*time.Minute + 15*time.Second},
			in:   time.Date(2026, time.September, 22, 4, 30, 14, 0, time.UTC),
			want: time.Date(2026, time.September, 21, 4, 30, 15, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.clk.BroadcastDay(tc.in)
			if !got.Equal(tc.want) {
				t.Fatalf("BroadcastDay(%s) = %s, want %s", tc.in, got, tc.want)
			}
			if got.Location() != tc.clk.location() {
				t.Errorf("BroadcastDay returned zone %s, want %s", got.Location(), tc.clk.location())
			}
		})
	}
}

func TestBroadcastDayIsIdempotent(t *testing.T) {
	clk := NewClock(mustLoadLocation(t, "America/Toronto"))
	start := clk.BroadcastDay(time.Date(2026, time.September, 22, 19, 4, 11, 0, time.UTC))
	again := clk.BroadcastDay(start)
	if !again.Equal(start) {
		t.Fatalf("BroadcastDay of a day start = %s, want %s", again, start)
	}
}

func TestBroadcastDayNilLocationIsLocal(t *testing.T) {
	clk := Clock{DayStart: DefaultDayStart}
	got := clk.BroadcastDay(time.Date(2026, time.September, 22, 19, 4, 11, 0, time.UTC))
	if got.Location() != time.Local {
		t.Fatalf("zone with a nil Location = %s, want %s", got.Location(), time.Local)
	}
}

// TestBroadcastDayLengthAcrossDST is the reason BroadcastDay builds its result
// with time.Date instead of subtracting 24 hours.
func TestBroadcastDayLengthAcrossDST(t *testing.T) {
	toronto := mustLoadLocation(t, "America/Toronto")
	clk := NewClock(toronto)

	tests := []struct {
		name string
		day  time.Time
		want time.Duration
	}{
		{
			name: "ordinary day is 24 hours",
			day:  time.Date(2026, time.September, 22, 4, 0, 0, 0, toronto),
			want: 24 * time.Hour,
		},
		{
			name: "spring forward day is 23 hours",
			day:  time.Date(2026, time.March, 7, 4, 0, 0, 0, toronto),
			want: 23 * time.Hour,
		},
		{
			name: "fall back day is 25 hours",
			day:  time.Date(2026, time.October, 31, 4, 0, 0, 0, toronto),
			want: 25 * time.Hour,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clk.nextDay(tc.day).Sub(tc.day)
			if got != tc.want {
				t.Fatalf("broadcast day starting %s lasted %s, want %s", tc.day, got, tc.want)
			}
		})
	}
}

// mustLoadLocation loads a zone or fails the test.
func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}
