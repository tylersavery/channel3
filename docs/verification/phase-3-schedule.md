# Verification Report: Phase 3 — Schedule package and `channel3 guide`

Reviewed 2026-09-22 against `docs/plans/plan.md`, Phase 3, by a reviewer session that did not write the code. The work is uncommitted in the working tree. This reviewer was spawned by an orchestrator lead and does not commit.

## Summary

Phase 3 is complete and correct. All seven tasks are implemented, the four types and four functions match the plan's signature block at `docs/plans/plan.md:284` character for character, every case named in the Tests section maps to a real test, and every item in the Verification checklist passes. The package is pure in the strong sense: `go list -deps` returns the package and nothing else with a dot in it, there are no package-level variables, no `time.Now`, no `os`, no `io` and no `net` anywhere in the non-test sources, and `Order` copies before it shuffles so the index's shared backing array is never reordered under a caller.

The boundary arithmetic is right at every edge the plan names and at several it does not. Exactly on a cumulative boundary returns the next item at offset zero, one nanosecond earlier returns the previous item with one nanosecond left, twenty passes minus one nanosecond lands on the last item, and a single-item channel loops on itself. All of it is integer nanosecond arithmetic on `time.Duration`. There is no float anywhere in the package, so no precision is lost between a sidecar's duration and a playback offset.

The golden test does what it is there for. I replaced `rand.NewPCG(s, s)` with `rand.NewPCG(s, s+1)` through a `go test -overlay` so the repo was never touched, and `TestOrderGolden` failed with a readable diff and the regeneration command. The algorithm is genuinely pinned, not merely fixtured.

Two warnings, neither blocking. One is a doc comment on `Slot.End` that `Guide` now contradicts, which matters because Phase 7 serialises `start`, `end` and `duration` from that struct. The other is that `--hours` has no upper bound, and the guide holds every slot in memory.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | `go test -race -count=3 ./internal/schedule/` green in 1.52 s. `go test -race -count=1 ./...` green across all three packages. 22 top-level tests and 47 subtests in `internal/schedule`, 7 and 21 in `cmd/channel3`, 0 failures, 0 skips. Coverage 97.3 % in `internal/schedule` and 75.7 % in `cmd/channel3`. |
| Matches plan | PASS | All 7 tasks done. All 5 checklist items verified independently. Signatures identical to the plan's block. |
| Security | PASS | No network, no filesystem, no process execution in `internal/schedule`. `grep` for `time.Now`, `os.`, `net/`, `io.` and `exec.` across the four non-test files returns nothing. No mutable package state. |
| Code quality | PASS | `gofmt -l .` silent, `go vet ./...` clean, `make lint` exits 0. No dead code: `nextDay`, `NewClock` and `DefaultDayStart` all have callers. Doc comments explain why, not what. |
| Scope | PASS | Nothing from Phase 5 or Phase 7 is implemented. `loadStation` holds nothing either command does with the result, as its own comment says. |
| Integration summary | N/A | No HTTP endpoints in this phase. |

## Context Health

Context scaffold not present. `.claude/context/` does not exist. Run `/intel` to generate it.

## Checklist verification

Each of these was run by this reviewer.

| Checklist item | Result |
|----------------|--------|
| `go test -race -count=3 ./internal/schedule/` passes | PASS. Green in 1.52 s, no flake across the three runs. |
| `go list -deps ./internal/schedule/` shows only standard library packages | PASS. With the executor's wider `grep '\.'` the only line is `github.com/tylersavery/channel3/internal/schedule` itself. |
| `bin/channel3 guide --root ~/srv/channel3 --hours 3` prints a plausible guide; twice within a minute gives identical titles and times | PASS. The 60 s NASA clip and the 20 s local clip alternate across the whole horizon with no gaps. Two consecutive runs were byte-identical. |
| `--at 2026-09-23T03:59:00-04:00` and `--at 2026-09-23T04:00:00-04:00` show different orders | PASS, but not demonstrable on the live root. The throwaway `test` channel has two items, so there are only two possible permutations, and both 2026-09-22 and 2026-09-23 happen to seed to `[a, Mars]`. I built an eight-item synthetic root in the scratchpad and ran the same two instants against it. At 03:59 the old day is airing `T-item08` from 03:50; at 04:00 the new day's order begins `item06, item05, item02, item01, item08, item07, item04, item03`. The orders differ and the switch is at exactly 04:00. The property itself is asserted properly on eight items at `internal/schedule/schedule_test.go:161`. This is a limitation of the fixture on the live root, not of the implementation. |
| The golden test exists and its expected order was generated once, then frozen | PASS. `internal/schedule/testdata/golden_order.json` holds eight items and the order for 2026-09-22, as the plan specifies. Confirmed it is load-bearing by perturbing the PCG seed through `go test -overlay` and watching `TestOrderGolden` fail. |

## Tests section, case by case

| Case the plan names | Where | Result |
|---------------------|-------|--------|
| Same inputs give the same `(item, offset)` across repeated calls | `internal/schedule/schedule_test.go:138` | PASS. Twenty calls compared on item, offset, `Start` and `End`. |
| Same across process runs (golden file) | `internal/schedule/order_test.go:42` | PASS. |
| One nanosecond before a cumulative duration returns the earlier item at `Duration - 1ns`; exactly on it returns the next at zero | `internal/schedule/schedule_test.go:36` | PASS. Six of the twelve subtests are boundary cases, and every one of them also runs `assertSlotConsistent`, which re-derives the offset from `when.Sub(slot.Start)` and asserts it lies in `[0, Duration)`. |
| Wrap: `elapsed` many multiples of `T` still lands correctly | `internal/schedule/schedule_test.go:44` | PASS. Seventeen passes, twenty passes minus one nanosecond, and ninety-nine loops of a single-item channel. |
| Day rollover: 03:59:59 and 04:00:00 local produce different orders; 04:00:00 is offset zero of the new order's first item | `internal/schedule/schedule_test.go:161` | PASS. Also asserts `Start` equals the rollover exactly. |
| DST: spring forward and fall back in `America/Toronto` give 23 h and 25 h, and `At` never panics or returns a negative offset | `internal/schedule/day_test.go:109` and `internal/schedule/schedule_test.go:202` | PASS. The lengths are asserted directly; `At` is then walked minute by minute across eight hours spanning each transition, 960 instants, each checked for a consistent slot. The test binary embeds `time/tzdata` at `internal/schedule/day_test.go:9`, so this does not depend on the host zone database. |
| Removed item: the order changes, every remaining item appears exactly once, `T` shrinks by that duration | `internal/schedule/order_test.go:203` | PASS. Also asserts the shorter order is not the full order with the item cut out, which is the part that would silently regress if the seed ever depended on item count. |
| Empty channel returns `false` | `internal/schedule/schedule_test.go:81` | PASS. Three variants: no items, all zero duration, all negative. |
| Single-item channel loops on itself | `internal/schedule/schedule_test.go:103` | PASS. |
| `Guide` covers at least `horizon` | `internal/schedule/guide_test.go:9` | PASS. One hour, six hours and thirty-six hours, the last crossing two rollovers. |
| `Guide` slots are contiguous | `internal/schedule/guide_test.go:169` | PASS. The shared helper asserts every slot's `Start` equals the previous slot's `End`, that only the first carries an offset, that the first covers `from`, and that no slot is longer than its item. |
| A guide that spans 04:00 switches order at exactly 04:00 | `internal/schedule/guide_test.go:62` | PASS. Asserts the last old-day slot ends exactly at the rollover, the first new-day slot starts exactly there, the new-day slot is the new order's first item, and every slot on each side belongs to the right day's order. |
| Golden: eight items, expected order for 2026-09-22 | `internal/schedule/order_test.go:42` | PASS. |

Beyond the plan's list, the executor covered the guide's zero and negative horizon, a single-item guide, `BroadcastDay` rolling back across a month and a year boundary, an instant supplied in a different zone than the clock's, a nil `Location`, a `DayStart` carrying minutes and seconds, and `Order` refusing to write through the returned slice into the caller's channel. That last one at `internal/schedule/order_test.go:162` is the test that matters most for Phase 5, because `library.Index` hands the same backing array to every caller.

## Executor deviations, ruled

**`DefaultDayStart` and `NewClock(loc)` added.** Acceptable. The four planned types and four planned functions are all present with the plan's exact signatures; these are additions, not changes. They close the trap the `Clock` doc comment itself warns about at `internal/schedule/schedule.go:47`, which is a caller writing `Clock{Location: loc}` and silently getting a midnight broadcast day. That behaviour is pinned deliberately at `internal/schedule/day_test.go:64`, so the trap is documented rather than papered over. Phases 5 and 7 now have a correct-by-construction way to build a clock. No contract is reopened.

**`Guide` cuts a slot's `End` at 04:00 when an airing spans the rollover.** Acceptable, and the right call. The plan's Tests section at `docs/plans/plan.md:337` requires both that slots are contiguous and that a guide spanning 04:00 switches order at exactly 04:00. Because `At` restarts the order at 04:00, those two cannot both hold if the airing runs to its natural end, so something has to give and cutting is the option that keeps the guide honest about what the TV will show. It is documented at `internal/schedule/guide.go:13` and tested at `internal/schedule/guide_test.go:81`, which asserts the cut slot's `End` is exactly the rollover and, separately at `:85`, that its wall-clock length is strictly less than its item's duration. I reproduced it through the CLI: a 12-minute item starting at 03:50 ends at 04:00 and the new day's first item starts there. It is also consistent with what Phase 5 will really do, since the station's 30-second reconcile compares `At(now)` with what mpv is playing and reloads within half a minute of the rollover. The one cost is WARN 1.

**Fisher-Yates written by hand over a PCG generator rather than `rand.Shuffle`.** Acceptable, and it is what the plan asked for. `docs/plans/plan.md:320` says the shuffle must be a Fisher-Yates over a PCG generator "so the algorithm is specified and stable across Go versions", which `rand.Shuffle` would have delegated back to the standard library. Seeding both PCG words from the same 64-bit FNV hash narrows the seed space from 128 bits to 64, which is irrelevant for permuting a few dozen videos and is the price of a seed that is one specified hash of two specified strings. The golden file pins the result and I confirmed it fails on a one-character change to the generator.

**`cmd/channel3/guide_test.go` and the `-update` flag added.** Acceptable. The plan's Files list named no test file for the command, but the Ground rules require tests beside code and this phase adds two files under `cmd/channel3/`. Without it, tasks 6 and 7 would have had no test at all; with it, `loadStation`, `printGuide`, `remainingLabel` and nine exit-code paths are covered. The `-update` flag is the standard Go golden-file pattern and its doc comment at `internal/schedule/order_test.go:14` correctly scopes the invocation to this one package. That scoping matters: `go test ./... -update` fails in the other two packages with "flag provided but not defined: -update". The doc comment already prescribes the right command, so this is a note rather than a warning.

**The plan's dependency grep was too narrow.** Acceptable, and an improvement. The plan's `grep -v '^[a-z]*$'` at `docs/plans/plan.md:352` keeps every line containing a slash, so it prints `hash/fnv`, `math/rand/v2`, `time` and a dozen transitive standard library packages and leaves a human to eyeball them. `grep '\.'` matches only paths with a dot in the first element, which is exactly the set of non-standard-library dependencies, and it prints only the package itself. That is the stronger result, and the plan line should be updated to match.

## Issues

### FAIL (must fix)

None.

### WARN (should review)

- `internal/schedule/schedule.go:41` The `End` field comment reads "Start + Item.Duration", which `Guide` contradicts at `internal/schedule/guide.go:28` for any airing cut at the 04:00 rollover. Phase 7's API contract serialises `start`, `end` and `duration` per slot, so an executor who trusts this comment and derives duration from `End - Start` will report a short duration on exactly one slot a day. Suggested fix: change the comment to "wall clock end of this airing; Start + Item.Duration except where Guide cuts an airing at the broadcast day rollover" and leave the code alone.

- `cmd/channel3/guide.go:39` `--hours` is checked for a lower bound of 1 but has no upper bound, and `schedule.Guide` accumulates every slot in a slice before returning. Measured against the live root, `--hours 20000` builds 1,797,942 slots in 1.65 s, and it scales linearly, so a mistyped `--hours 1000000` allocates on the order of 90 million slots and exhausts memory. Phase 7 already caps its own `hours` parameter at 48. Suggested fix: add an upper bound beside the existing check, for example `if *hours < 1 || *hours > 48`, with the same error shape.

## Verdict

PASS WITH WARNINGS
