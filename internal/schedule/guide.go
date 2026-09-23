package schedule

import "time"

// Guide returns the airings of ch from `from` until at least horizon has been
// covered, starting with whatever is on at `from`.
//
// Slots are contiguous: each slot's End is the next slot's Start. Only the first
// slot carries a non-zero Offset, because only the first is already part way
// through when the guide is asked for. The first slot's Start may be earlier
// than `from` for the same reason.
//
// An airing that would run past the 04:00 rollover is cut short there, and the
// next slot is the first item of the new day's order. That is what the service
// really does: the day's order is recomputed at 04:00 and the item playing at
// the time is replaced rather than finished.
//
// A channel with nothing playable returns nil.
func Guide(ch Channel, from time.Time, horizon time.Duration, c Clock) []Slot {
	end := from.Add(horizon)

	var slots []Slot
	for cursor := from; ; {
		slot, ok := At(ch, cursor, c)
		if !ok {
			return slots
		}
		if boundary := c.nextDay(c.BroadcastDay(cursor)); slot.End.After(boundary) {
			slot.End = boundary
		}
		slots = append(slots, slot)

		// Every slot ends strictly after the cursor that produced it, so this
		// loop always makes progress towards end.
		cursor = slot.End
		if !cursor.Before(end) {
			return slots
		}
	}
}
