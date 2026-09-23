# Verification Report: Phase 5 — Player supervisor and `channel3 serve`

Reviewed 2026-09-23 against `docs/plans/plan.md`, Phase 5, by a reviewer session that did not write the code. The work is uncommitted in the working tree. This reviewer was spawned by an orchestrator lead and does not commit.

## Summary

Phase 5 is complete and correct. All eight non-gated tasks are implemented, every case named in the Tests section maps to a real test, and every item in the Verification checklist passes on the Mac against Homebrew mpv 0.41.0. Task 9 is hardware-gated, correctly left unticked, and carries a written reason at `docs/plans/plan.md:461`.

The parts that decide whether this appliance survives unattended all work. I killed mpv with `pkill -x mpv` during playback and a new process was up, reconnected and re-seeked to the schedule's offset inside one second, with `mpv stopped, restarting after=30.07s wait=500ms` then a reconnect then `mpv restarted, reloading the current item` then a load at `offset=5.019s`. I truncated `b.mp4` to zero bytes and restarted: mpv reported `file_error="unrecognized file format"` once, the station logged `dropping an unplayable item for the rest of the day channel=clips item=b`, and every subsequent load was `item=a` alone. I pointed serve at a channel with nothing ingested and it logged `nothing to play, showing stand by` exactly once and held the card with mpv alive, which also proves the embedded PNG loads in real mpv, because a card that failed to load would emit an error end-file and drive the station into a reload loop.

The `end-file` filtering is the rule the whole loop rests on and it holds against the real thing. Across a four-minute live run at twenty-second boundaries the log shows exactly one load per boundary and never a double load, so mpv's `stop` for the file our own `loadfile` displaced is genuinely being swallowed at `internal/player/player.go:599`. The four-argument `loadfile` works on 0.41 and the offsets prove it: booting mid-clip loaded `offset=14.28s` and playback ran six seconds to the boundary, which could not happen if the options string were being read as the index.

The supervisor does not leak. I ran eight full launch, crash, restart and close cycles through a `go test -overlay` probe that never touched the tree, then counted goroutines still sitting in `supervise`, `ipcConn.read` or the process-wait closure. Zero remained. No mpv or serve process was left behind by any check in this review.

Four warnings, none blocking. The one worth fixing before Phase 6 is that a socket closing mid-command returns a misleading error that defeats the `errors.Is` guard already written against it.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | `go test -race -count=1 ./...` green. `internal/player` 21 top-level tests in 7.8 s; `cmd/channel3` 20 top-level and 22 subtests; `internal/schedule` 22 and 47; `internal/library` 75 and 43. 0 failures, 1 pre-existing skip in `library`. Coverage 70.2 % in `internal/player`, 60.1 % in `cmd/channel3`. |
| Matches plan | PASS | Tasks 1 to 8 done. Task 9 hardware-gated and unticked with a reason. All twelve base mpv flags at `internal/player/mpv.go:50` match the plan's list in order. `Load` sends the exact pinned command. Backoff, health interval and health deadline are the plan's numbers in `DefaultTimings` at `internal/player/player.go:94`. |
| Security | PASS | No network. The only `net` call in the package is `net.Dial("unix", …)` at `internal/player/ipc.go:88`. `grep -rn "os.WriteFile\|os.Create\|os.OpenFile" cmd/ internal/player/` returns the pid file write at `cmd/channel3/serve.go:147`, the card extraction at `internal/player/standby.go:28`, and three fixture writes in pre-existing test files. No playback state is persisted anywhere; station state is in-memory fields on one struct. |
| Code quality | PASS | `make lint` exits 0, `gofmt` silent, `go vet ./...` clean including `tools/`. Doc comments explain why rather than what. No dead code found in the new files. |
| Scope | PASS | Nothing from Phase 6 or 7 is implemented. `serve.go:32` names the flags each of those phases will add rather than adding them. `tune(id)` exists because task 7 needs it, not as Phase 6 work done early. |
| Integration summary | N/A | No HTTP endpoints in this phase. |

## Context Health

Context scaffold not present. `.claude/context/` does not exist. Run `/intel` to generate it.

## Checklist verification

Each of these was run by this reviewer, independently of the executor.

| Checklist item | Result |
|----------------|--------|
| `make lint`, `make test`, `make build-arm64` pass; no test spawns a real mpv | PASS. Lint clean, full suite green under `-race -count=1`, `bin/channel3-linux-arm64` is `ELF 64-bit LSB executable, ARM aarch64, statically linked`. `grep -rn "exec.Command" --include="*_test.go" .` returns two hits, both `/bin/sh` in the pre-existing Phase 2 `internal/library/ingest_test.go`. No test references `mpv` as a binary; every player test drives a `fakeLauncher`. |
| Serve runs on the Mac against the two 20 s clips | PASS. `bin/channel3 serve --root ~/srv/channel3 --start-channel clips --mpv-arg=--no-fullscreen --mpv-arg=--geometry=960x540` came up in under a second and played. |
| Stand By card first, then a clip mid-way matching `guide` | PASS. First load was `item=a offset=14.28s now=2026-09-23T08:23:34-04:00`, and `guide` for that minute showed `08:23 a (remaining 1m)`, consistent with a 20 s clip started at 08:23:20. The card precedes it: `station.run` calls `standby()` before `play()` at `cmd/channel3/station.go:136`, asserted by `TestStartupShowsStandbyThenLoadsTheSchedule`. |
| Clips alternate at boundaries, no gap over about a second, no repeat | PASS. Loads at 08:23:34 `a`, 08:23:40 `b` at `offset=340ms`, 08:24:00 `a` at `offset=11ms`, 08:24:20 `b` at `offset=176ms`. Strict alternation, boundaries twenty seconds apart, worst lateness 340 ms. |
| `pkill mpv`: new window inside five seconds at the current offset; log shows `Restarted` then a load | PASS. Killed at 08:24:04, reconnected at 08:24:05, `mpv restarted, reloading the current item`, load at `offset=5.019s`. New mpv pid 70925 replaced 70560. Under one second, not five. |
| Empty `b.mp4` and restart: `b` excluded after one error, `a` loops alone, log names the item | PASS. One `mpv could not play a file … file_error="unrecognized file format"`, then `dropping an unplayable item for the rest of the day channel=clips item=b title=b path=…/b.mp4`, then three consecutive `item=a` loads at 20 s boundaries. `b.mp4` was copied aside first and restored; md5 `8210d392b9719da36ae7ac987cd94d1f` before and after, `ffprobe` duration 20.000000. |
| A channel with no items shows the card and the process stays up | PASS. Ran against a throwaway root in the scratchpad with one channel and nothing ingested. One `nothing to play, showing stand by` line, mpv alive at pid 72412 eight seconds later, serve alive, clean exit on SIGINT. The single log line is also the proof there is no standby reload loop. |
| `serve.pid` exists while running and is gone after Ctrl-C; `ingest` refuses while it exists | PASS. File held `70558` during the run and `ingest` refused with `serve is broadcasting as pid 70558 (/Users/tyler/srv/channel3/serve.pid); stop it first`. Gone after SIGINT, and separately gone after SIGTERM, with no mpv left in either case. |
| `grep` for `os.WriteFile\|os.Create` shows only the pid file and the card | PASS. See the Security row. |
| Task 9 on the Pi | WARN, hardware-gated. Not run, correctly unticked, reason recorded in the plan. |

## Checks beyond the checklist

These cover the areas the lead asked to be examined with particular care.

| Area | Result |
|------|--------|
| Request ids correlate under concurrency | PASS. `TestCommandCorrelatesRepliesUnderConcurrency`. Ids come from an `atomic.Int64` at `internal/player/ipc.go:117` and each caller waits on its own buffered channel. |
| mpv error strings become Go errors | PASS. `internal/player/ipc.go:139` and `TestCommandTurnsMPVErrorIntoGoError`. |
| Events delivered in order | PASS. `TestEventsArriveInOrder`. One reader goroutine owns the socket, so ordering is structural. |
| Command timeout when the fake stops replying | PASS. `TestCommandTimesOutWhenMPVStopsReplying` asserts a deadline error inside 2 s. |
| Reader goroutine shutdown, no leaks or deadlock | PASS. Measured, not inferred: zero goroutines left in supervisor or IPC frames after eight full crash-and-close cycles. The reader cannot deadlock against the router because a full event buffer drops rather than blocks at `internal/player/ipc.go:206`. |
| Socket closes mid-command | PASS with a caveat. The caller is released, but with a misleading error. See WARN 1. |
| `loadfile` form and the version gate | PASS. `TestLoadSendsTheFourArgumentLoadfile` pins `["loadfile","/lib/a.mp4","replace",-1,"start=83.500"]` exactly. `TestStartRefusesOldMPV` refuses 0.37 with an error naming both versions; `TestStartAcceptsTheMinimumVersion` holds the boundary at 0.38. Confirmed working against real 0.41 by the offsets in the live run. |
| `stop` and `redirect` swallowed, `eof` and `error` surfaced | PASS. `TestEndFileReasonsAreFiltered` and `TestEndFileErrorIsSurfaced` against the fake, and one-load-per-boundary across a four-minute live run against real mpv. |
| Restart backoff and health check | PASS for behaviour, WARN for test coverage. See WARN 3. |
| `Restarted` makes the station re-seek | PASS. `cmd/channel3/station.go:171`, `TestRestartedReloadsTheCurrentItem`, and the live `pkill` run. |
| mpv killed on context cancel, no zombie on shutdown | PASS. `exec.CommandContext` at `internal/player/mpv.go:93`, and `killSession` waits on the process at `internal/player/player.go:644`. Zero mpv processes after every run in this review. |
| Station state in memory only | PASS. Every field on `station` at `cmd/channel3/station.go:35` is in-memory and nothing writes them anywhere. |
| Tune to `--start-channel` or lowest number | PASS. `cmd/channel3/station.go:115`, `TestStartChannelSelectsByID`. `loadStation` sorts by number so index zero is the lowest. |
| `eof` re-asks `schedule.At` rather than taking the next in the list | PASS. `cmd/channel3/station.go:170` routes both reasons through `play()`, which calls `schedule.At(playable, s.now(), s.clock)`. `TestEOFAsksTheScheduleAgain`. |
| `error` excludes for the day and re-asks | PASS. `TestErrorExcludesTheItemForTheDay`, plus the live truncation run. |
| All items excluded shows Standby | PASS. `TestEveryItemExcludedShowsStandby`. |
| 30 s reconcile with 5 s tolerance corrects a clock jump | PASS. `TestReconcileCorrectsAClockJump` advances a fake clock three hours and asserts one reload at the new offset. `TestReconcileLeavesHealthyPlaybackAlone` proves two seconds of drift does not seek. `TestReconcileReloadsWhenMPVIsOnTheWrongItem` covers a dropped event. |
| 04:00 rollover rescans, clears exclusions, replays | PASS. `TestRolloverRescansAndForgivesExclusions` counts the scans. `TestRolloverKeepsPlayingWhenTheRescanFails` covers the failure path. See WARN 4 for a detail in that path. |
| Zero-item channel shows Standby | PASS. `TestChannelWithNoItemsShowsStandby` and the live throwaway root. |
| Races between the event loop, the tick and shutdown | PASS. `-race` is green, and the structure is the stronger argument: `station.run` is a single goroutine and `play`, `reconcile` and `handle` are only ever reached from its `select`, so no station field is touched concurrently. The `Supervisor` is the shared object and guards `conn` and `entries` with one mutex. |
| One log line per load with channel, item id, offset and `now` | PASS. `cmd/channel3/station.go:227`. Live: `INFO loading channel=clips number=5 item=a title=a offset=14.28s now=2026-09-23T08:23:34-04:00`. |
| `--mpv-arg` repeatable and appended after the base flags | PASS. `internal/player/mpv.go:92`. `BaseArgs` returns a fresh slice on every call, so the append cannot alias. Both extras took effect in the live runs. |
| `--standby` override | PASS. A missing path is refused before mpv or the pid file exist: `serve: --standby /no/such/card.png: stat …: no such file or directory`, and no pid file was left behind. A valid override ran and held the card. |
| Standby PNG embedded, 1920x1080, small, reproducible | PASS. `PNG image data, 1920 x 1080, 8-bit/color RGB`, 8791 bytes. `go run ./tools/standbycard -o …` reproduced it byte for byte, md5 `d8aa73056e4075aea5cc9455433eaf64`. |

## Deviations from the plan

All five the executor declared are accurate, and I found no undeclared ones. My rulings:

1. **The card is drawn by a Go program, not ffmpeg or ImageMagick. Accept, and this is not a deviation.** The plan at `docs/plans/plan.md:437` says the card is "generated by an ImageMagick or Go command recorded in the Makefile as `standby-card`". A Go command is one of the two options the plan names. `tools/standbycard/main.go` is deterministic, the target exists in the Makefile, the PNG is committed, and regeneration is byte-identical. The one structural note is that `tools/` is a new top-level directory absent from the layout block in `CLAUDE.md`. It is build-time only, is not imported by `cmd/channel3`, and does not enter either binary, so it is worth a line in `CLAUDE.md` eventually rather than a change now.
2. **Unix socket path length cap. Accept.** Not anticipated by the plan and correctly handled at `internal/player/mpv.go:82` with an explicit message rather than an opaque bind failure. This would have bitten on the Pi under a long `XDG_RUNTIME_DIR`.
3. **mpv runs on its own context rather than the signal context. Accept, and it is the right call.** `cmd/channel3/serve.go:97`. The plan's "kill on context cancel" is still honoured by the launcher; serve simply chooses which context so that Ctrl-C leaves room for the `quit` command. Verified: SIGINT and SIGTERM both quit mpv cleanly and leave no process.
4. **`Standby()` sends the three-argument `loadfile`. Accept.** Task 5 only requires the card be loaded with `replace`, and the four-argument form is pinned for `Load` alone. An image has no timeline for `start=`. Real mpv 0.41 accepted it in every run.
5. **A `Timings` struct so tests can shorten deadlines. Accept.** `DefaultTimings` at `internal/player/player.go:94` holds the plan's numbers and the service uses them. See WARN 3 for the test-coverage consequence.

One further extension the executor noted but did not list as a deviation: `reason: quit` is swallowed alongside `stop` and `redirect` at `internal/player/player.go:599`, where the plan names only the latter two. **Accept.** Surfacing a `quit` end-file during teardown would make the station issue a load into an mpv that is exiting. The plan's intent is that only `eof` and `error` reach the station, and this serves it.

## Issues

### FAIL (must fix)

None.

### WARN (should review)

1. **`internal/player/ipc.go:138`** — when the socket closes while a command is in flight, the caller gets `player: mpv rejected [get_property idle-active]: ` with an empty mpv error text instead of `errConnClosed`. `read` closes every pending channel at `internal/player/ipc.go:182` before the deferred `close(c.done)` runs, so the `select` takes the closed `replies` channel and reads a zero-value `mpvReply` whose `Error` is `""`, which is not `"success"`. I confirmed this is deterministic with a `go test -overlay` probe that never touched the tree: five of five attempts returned that text with `errors.Is(err, errConnClosed)` false. The consequences are contained but real. The guard at `internal/player/player.go:622` is written as `!errors.Is(err, errConnClosed)` and will therefore never suppress the spurious "mpv did not accept the quit command" warning it exists to suppress. `isUnavailable` at `internal/player/player.go:356` cannot classify the error either, so `Position` returns it and the reconcile logs "could not read the player position, reloading", which recovers correctly but for the wrong stated reason. On a headless Pi the log is the only diagnostic, and it will say mpv rejected a command when mpv actually died. `TestCommandFailsAfterConnectionCloses` at `internal/player/ipc_test.go:157` asserts only `err != nil`, which is why this passed. Fix at `internal/player/ipc.go:138`: `case reply, ok := <-replies:` followed by `if !ok { return nil, errConnClosed }`, and tighten the test to assert the error identity.

2. **`cmd/channel3/station.go:196`** — an `EndFile` with reason `error` whose path does not resolve to an item on the tuned channel excludes nothing, and `handle` at `cmd/channel3/station.go:170` then calls `play()` regardless, which reloads the same unplayable item. There is no backoff and no minimum interval between loads, so this is an unthrottled spin rather than a slow retry. The reachable path is the warn-and-continue branch at `internal/player/player.go:279`: if a `loadfile` reply cannot be decoded for its `playlist_entry_id`, no entry is recorded, the later `end-file` resolves to an empty path, and `findByPath` returns false for it at `cmd/channel3/station.go:365`. mpv 0.38 and newer always return that field, so this is latent rather than observed, and live exclusion worked correctly on the first attempt. The comment at `cmd/channel3/station.go:193` shows the executor considered excluding the last-loaded item and judged it a guess, which is defensible; the gap is that the fallback is an infinite loop rather than a bounded one. Suggested fix: remember the path the station last handed to `Load` and refuse to reload the same path twice in a row without at least one reconcile interval in between, logging once when it declines.

3. **`internal/player/player.go:94` and `internal/player/player.go:534`** — the plan's restart policy numbers are asserted nowhere. `DefaultTimings` holds 500 ms, 5 s, 60 s, 10 s and 2 s, and `nextBackoff` implements the doubling and the cap, but `nextBackoff` has no test and every player test overrides the timings with 10 ms, 50 ms and 1 s and asserts only that a restart eventually happens. The doubling sequence, the 5 s ceiling and the reset after 60 s of health are all unverified by the suite, so a later edit to any of them would go unnoticed. The live `pkill` run observed only the first step, `wait=500ms`. `nextBackoff` is a pure function of two arguments; a table test over it plus one assertion that `DefaultTimings` returns the plan's five numbers would close this in a few lines.

4. **`cmd/channel3/station.go:307`** — `rollover` assigns `s.day = day` before attempting the rescan, so a rescan that fails at 04:00 is never retried and the library is not re-read until the next day's rollover. The station keeps broadcasting yesterday's channels, which is the right behaviour for the screen, and exclusions are still cleared, so nothing breaks. But a transient failure, which on the Pi most plausibly means a disk not yet mounted at boot, costs a full day of newly ingested content with only one error line to show for it. Suggested fix: advance `s.day` only after `reload()` succeeds, so the next reconcile tick retries.

5. **`docs/plans/plan.md:461`** — task 9 is hardware-gated and was not run. There is no Pi and Phase 4 has not happened, so boot-to-video, a boundary transition and `pkill mpv` recovery on the real target, and any mpv flag changes the Pi needs, remain unproven. All three behaviours were verified on the Mac against mpv 0.41.0. The plan already tracks the close-out at `docs/plans/plan.md:785`. This is the expected state of the phase, not a defect.

## Verdict

PASS WITH WARNINGS
