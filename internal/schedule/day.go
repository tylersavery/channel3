package schedule

import "time"

// DefaultDayStart is when the broadcast day rolls over: 04:00 local, late enough
// that the daily reshuffle never happens while anyone is watching.
const DefaultDayStart = 4 * time.Hour

// NewClock returns the standard Channel Three clock for loc, with the broadcast
// day starting at 04:00 local. A nil loc means the system's local zone.
func NewClock(loc *time.Location) Clock {
	return Clock{Location: loc, DayStart: DefaultDayStart}
}

// BroadcastDay returns the start of the broadcast day t belongs to: 04:00 local
// on t's calendar day, or 04:00 on the previous calendar day when t is earlier
// than that.
//
// The result is built with time.Date rather than by subtracting 24 h, so a
// daylight saving day is 23 or 25 hours long because the standard library says
// so and not because this package did arithmetic about it.
func (c Clock) BroadcastDay(t time.Time) time.Time {
	loc := c.location()
	local := t.In(loc)
	y, m, d := local.Date()
	hour, min, sec, nsec := c.dayStartFields()

	start := time.Date(y, m, d, hour, min, sec, nsec, loc)
	if local.Before(start) {
		// time.Date normalises day zero to the last day of the previous month.
		start = time.Date(y, m, d-1, hour, min, sec, nsec, loc)
	}
	return start
}

// nextDay returns the start of the broadcast day following the one that starts
// at day. The gap between them is 23, 24 or 25 hours depending on daylight
// saving, which is exactly what a broadcast day is.
func (c Clock) nextDay(day time.Time) time.Time {
	loc := c.location()
	local := day.In(loc)
	y, m, d := local.Date()
	hour, min, sec, nsec := c.dayStartFields()
	return time.Date(y, m, d+1, hour, min, sec, nsec, loc)
}

// dayStartFields splits DayStart into the clock fields time.Date takes.
func (c Clock) dayStartFields() (hour, min, sec, nsec int) {
	rest := c.DayStart
	hour = int(rest / time.Hour)
	rest -= time.Duration(hour) * time.Hour
	min = int(rest / time.Minute)
	rest -= time.Duration(min) * time.Minute
	sec = int(rest / time.Second)
	rest -= time.Duration(sec) * time.Second
	return hour, min, sec, int(rest)
}
