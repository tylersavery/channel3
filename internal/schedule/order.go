package schedule

import (
	"hash/fnv"
	"math/rand/v2"
	"time"
)

// dayLayout is the calendar date format the seed is built from. Only the date
// matters, so every instant within one broadcast day seeds the same shuffle.
const dayLayout = "2006-01-02"

// Order returns the items ch plays on the broadcast day starting at day, in the
// order it plays them.
//
// The order is a seeded shuffle, so it is stable for a given channel and day
// across calls, processes and machines, and it changes at every 04:00 rollover.
// Items with a duration of zero or less are dropped before shuffling: they would
// occupy no airtime and would make the boundary walk ambiguous.
//
// day is expected to be a BroadcastDay result. Only its calendar date in its own
// location is read.
//
// The caller's Items slice is never reordered. Order copies first, because the
// index this slice comes from hands out the same backing array to every caller.
func Order(ch Channel, day time.Time) []Item {
	items := make([]Item, 0, len(ch.Items))
	for _, item := range ch.Items {
		if item.Duration > 0 {
			items = append(items, item)
		}
	}
	if len(items) < 2 {
		return items
	}

	// Fisher-Yates written out rather than rand.Shuffle, so the order this
	// package produces is pinned by the golden test to this algorithm and not to
	// whatever the standard library shuffles with today.
	rng := newRand(seed(ch.ID, day))
	for i := len(items) - 1; i > 0; i-- {
		j := rng.IntN(i + 1)
		items[i], items[j] = items[j], items[i]
	}
	return items
}

// seed hashes a channel and a broadcast day into the shuffle's seed with
// FNV-1a 64, which is specified, cheap and stable across Go versions.
func seed(channelID string, day time.Time) uint64 {
	h := fnv.New64a()
	// fnv's Write never returns an error, which is why hash.Hash documents it
	// and why there is nothing to handle here.
	h.Write([]byte(channelID))
	h.Write([]byte("|"))
	h.Write([]byte(day.Format(dayLayout)))
	return h.Sum64()
}

// newRand builds the generator the shuffle draws from: PCG from math/rand/v2,
// with the FNV hash used for both of its two seed words.
func newRand(s uint64) *rand.Rand {
	return rand.New(rand.NewPCG(s, s))
}
