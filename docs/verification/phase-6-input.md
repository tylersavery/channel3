# Verification Report: Phase 6 — Channel switching: tuner, keyboard, Flirc, CEC

Reviewed 2026-09-23 against `docs/plans/plan.md`, Phase 6, by a reviewer session that did not write the code. The work is uncommitted in the working tree. This reviewer was spawned by an orchestrator lead and does not commit.

## Summary

Phase 6 is complete and correct for everything that can be checked without a Pi. Tasks 1 to 6 are implemented as specified, every case the plan's Tests section names maps to a real test that asserts the right thing, and every non-gated item in the Verification checklist passes on the Mac against real mpv 0.41.0. Tasks 7 and 8 are hardware-gated, correctly left unticked, and each carries a written reason in the plan at `docs/plans/plan.md:533` and `docs/plans/plan.md:536`.

The tuner is genuinely pure. It starts no goroutines, reads no clock, and holds two fields of state that a caller can only move by passing a time in. The strict-prefix rule the plan describes is implemented as a strict prefix and I pinned it against the plan's own example with an overlay probe: with channels 3, 5 and 12 keying 1 waits, keying 3 commits at once, and adding a real channel 1 to the list keeps 1 waiting rather than committing, which is the case a `>=` instead of a `>` at `internal/input/tuner.go:162` would break. Every other case in the Tests section is covered by the shipped table test, including both wrap directions, commit on the second digit, commit on the timeout, a timeout on a number that matches nothing, up clearing pending digits, digits on an empty channel list, and the deadline being the last digit plus 1.5 s.

Channel changing works on air. I drove `serve` through a pseudo-terminal three times against `~/srv/channel3` with real mpv: `+` went 5 to 3, `-` went back, the arrows did the same, `3` and `5` tuned at once, `9` logged `no channel has that number, staying put keyed=9 channel=clips` and left playback alone, and every load offset agreed with `bin/channel3 guide` for that minute. Pressing `5` while already on channel 5 logged `already on that channel` and loaded nothing, which is the executor's deviation and the right call. Ctrl-C exited 0 and restored the terminal byte for byte: I compared the full `termios` struct of the slave pty before and after and it is identical, including the control characters. `--no-input` ran 20 s headless, logged `input is off, the channel cannot be changed`, crossed an item boundary correctly and exited 0 on SIGINT with no mpv left behind. No process from any check in this review is still running, and `~/srv/channel3` is byte-identical to how I found it.

Four warnings, none blocking. The one worth fixing before Phase 7 is that a CEC call runs inside the station's select loop, so a television that does not answer stalls the broadcast for up to 6 s per volume press. It cannot bite today because `--cec` defaults off and Phase 4 has not run, but Phase 9's unit is where that default gets decided.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | `go test -race -count=1 ./...` green, and `make test` green. 173 top-level tests and 176 subtests across five packages, 0 failures, 0 new skips. `internal/input` 22 top-level and 64 subtests in 1.5 s; `cmd/channel3` 30 top-level and 22 subtests. Coverage 53.5 % in `internal/input` and 61.2 % in `cmd/channel3`, up from 60.1 %. |
| Matches plan | PASS | Tasks 1 to 6 done and ticked. Tasks 7 and 8 hardware-gated, unticked, reasons recorded. The evdev code map at `internal/input/evdev.go:75` matches the plan's task 2 list exactly, asserted whole by `TestDecodeEveryMappedCode`, which also fails if the map and the table ever differ in size. The terminal map at `internal/input/tty.go:19` is the plan's list exactly. |
| Security | PASS | No network. `grep -rn "\"net\|http" internal/input/*.go` returns nothing and `go list -deps ./internal/input` outside the standard library is `golang.org/x/sys/unix` and `golang.org/x/term` only. `grep -rn "Persist\|WriteFile\|json.Marshal\|os.Create\|os.OpenFile" internal/input/` returns nothing, so no playback state is persisted. The only file this phase opens for writing is nothing at all; the pid file is Phase 5's. `EVIOCGRAB` at `internal/input/evdev_linux.go:107` is the right hardening for the Pi's console. |
| Code quality | PASS | `make lint` exits 0, `gofmt` silent, `go vet ./...` clean, and `GOOS=linux go vet ./internal/input/` clean so the Linux-only file is checked from the Mac. Doc comments say why. One dead method, see WARN 4. |
| Scope | PASS | Nothing from Phase 7 is built. `serve.go:33` names `--listen` as Phase 7's to add. `Tuned()` exists because the plan's task 5 asks for it, and it is guarded with a `sync.RWMutex` rather than left for Phase 7 to discover. |
| Integration summary | N/A | No HTTP endpoints in this phase. |

## Context Health

Context scaffold not present. `.claude/context/` does not exist. Run `/intel` to generate it.

## Checklist verification

Each of these was run by this reviewer, independently of the executor.

| Checklist item | Result |
|----------------|--------|
| `make lint`, `make test`, `make build-arm64` pass; `GOOS=linux go vet ./internal/input/` passes | PASS. All four clean. `bin/channel3-linux-arm64` is `ELF 64-bit LSB executable, ARM aarch64, statically linked`, so the new dependencies did not pull in cgo. |
| Two channels on the Mac: `serve` in a terminal, `+` and `-` switch clips within a second at the offset `guide` predicts | PASS. Driven through a pty. `+` at 09:53:48 logged `tune number=3 channel=test` and `loading … offset=3.833s`; `-` at 09:53:51 logged `tune number=5 channel=clips` and `loading … item=b offset=11.116s`. Both inside the same second as the press. `bin/channel3 guide` for 09:53 showed channel 3 playing item `a` from 09:53, consistent with a 3.8 s offset at 09:53:48. The arrows produced the same two tunes. |
| Type the channel number and see it commit | PASS. `3` at 09:53:53 tuned test, `5` at 09:53:56 tuned clips, each with exactly one load. Two-digit commit on the second digit and commit on the timeout are covered by `TestTwoDigitsTuneAtOnce` and `TestDigitsTuneAfterTheTimeout`, which run the real station timer; the local root has no two-digit channel to press. |
| A number that matches no channel leaves playback untouched and logs it | PASS. `9` logged `no channel has that number, staying put keyed=9 channel=clips` and the next load was the scheduled boundary, not a retune. Asserted as well by `TestUnknownChannelNumberChangesNothing`, which reads the log. |
| `--no-input` runs without touching the terminal | PASS. 20 s headless run, one line `input is off, the channel cannot be changed`, an item boundary handled at 09:53:00, exit 0 on SIGINT, no mpv left. `startInput` returns at `cmd/channel3/serve.go:174` before any terminal call. |
| `grep -rn "Persist\|WriteFile\|json.Marshal" internal/input/` finds nothing | PASS. Nothing, with `os.Create` and `os.OpenFile` added to the pattern. |
| Tasks 7 and 8 on the Pi | WARN, hardware-gated. Neither run. No Pi, no Flirc, no television, and Phase 4 has not happened so there is no CEC result. Correctly unticked with reasons. `--cec` stays off by default. |

## Checks beyond the checklist

These cover the areas the lead asked to be examined with particular care.

| Area | Result |
|------|--------|
| Tuner purity | PASS. No goroutine, no clock, no I/O. `now` is only ever stored as `lastDigit` at `internal/input/tuner.go:149`. `NewTuner` copies its slice, pinned by `TestTunerCopiesItsChannels`. |
| Strict-prefix rule with channels 3, 5, 12 | PASS, measured. `internal/input/tuner.go:162` compares `len(number) > len(pending)` before the prefix, so an exact match that is also a prefix still waits. Proved with an overlay probe on all three cases plus a channel 1 added alongside 12. |
| Every case in the plan's Tests section | PASS. All eight named cases exist in `internal/input/tuner_test.go`, plus a single-channel case, a power-and-volume case proving those keys do not disturb pending digits, and a tuned channel that vanished from the config. |
| evdev 24-byte layout | PASS. `TestEventLayoutIs24Bytes` builds the record by hand with its own offsets rather than the decoder's, so a layout change fails the test instead of passing quietly. |
| Press only, release and autorepeat ignored | PASS. `internal/input/evdev.go:137`, `TestDecodePressReleaseAndRepeat` feeds press, syn, two repeats, release, syn and asserts exactly one ChannelUp. |
| Partial trailing record buffered | PASS. `TestDecodeBuffersAPartialRecord` splits mid-struct and `TestDecodeSplitsAcrossManyChunks` feeds the stream one byte at a time. `TestDecoderBufferDoesNotGrow` pins the leftover copy at `internal/input/evdev.go:148`, which is what keeps a months-long run from growing a backing array. |
| Unknown codes dropped, non-EV_KEY dropped | PASS. `TestDecodeDropsUnknownCodes` and `TestDecodeIgnoresNonKeyEvents`, the latter with EV_REL and EV_MSC carrying the same code as a channel key. |
| Code map matches the plan's task 2 | PASS. Verified code by code against `input-event-codes.h` values: 402/403 channel, 103/104/108/109 arrows and page, 2 to 11 digits, 71 to 82 keypad, 113 mute, 114 down, 115 up, 116 power. |
| `EVIOCGRAB` requested | PASS. `internal/input/evdev_linux.go:24` computes `_IOW('E', 0x90, int)` as `0x40044590`, which is correct, and `unix.IoctlSetInt` passes the value the kernel actually reads. A device that refuses the grab is logged and still read, which is the right trade. |
| Reopen with backoff when the device disappears | PASS by reading. `internal/input/evdev_linux.go:55` starts at 250 ms and doubles to a 5 s cap for a path that has never opened, and resets to 250 ms for a device that opened and then vanished, which is the unplugged-Flirc case. Cannot be exercised on the Mac. See WARN 3 for the missing unit test. |
| Auto-detect prefers "flirc" | PASS by reading. `internal/input/evdev_linux.go:174` lowercases the sysfs name and looks for the substring, scanning `/dev/input/event*` in sorted order. |
| Fallback when nothing advertises `KEY_CHANNELUP` or `KEY_0` | PASS by reading. `reportsRemoteKeys` asks `EVIOCGBIT(EV_KEY)` and checks both bits; `keyBitmapBytes` is 96, which covers `KEY_MAX` 0x2ff, and `hasKeyBit` bounds-checks its index. With no match, `DetectDevice` returns an error and `startInput` logs a warning and carries on rather than failing to boot. |
| `/dev/input` absent or unreadable | PASS with a caveat. Both produce an empty glob and the error `no /dev/input/event* devices exist`. A permissions failure is indistinguishable from an empty directory in the log. See WARN 2. |
| Goroutine and fd cleanup on cancel | PASS by reading. `session` opens non-blocking so the runtime can poll, closes the file from a watcher goroutine that is itself released by a deferred `close(closed)` at `internal/input/evdev_linux.go:123`, and the deferred `file.Close()` double-close is harmless. Every path through `Keys` ends at `close(out)`. |
| Raw mode only when stdin is a terminal and `--no-input` is unset | PASS. `cmd/channel3/serve.go:174` returns before any source is built under `--no-input`, and `input.IsTerminal(os.Stdin)` gates the TTY at `cmd/channel3/serve.go:182`. Confirmed live: the headless run touched nothing. |
| Terminal restored on every exit path | PASS, measured. `Restore` is idempotent under a mutex at `internal/input/tty.go:173`, deferred both by the read loop and by `serve` itself at `cmd/channel3/serve.go:137`, so it also covers a panic unwinding `serve`. Three live pty runs ended with the slave `termios` identical to before, control characters included. |
| Escape parsing does not misread a bare `[` | PASS. A bare `[` is volume down and never enters the escape branch; `TestDecodeTTY` covers it and the live run logged `ignoring a television key, cec is off key=volume-down`. |
| Escape parsing does not swallow a lone Escape | WARN. It is not swallowed but it does delay the next key. See WARN 1, which includes the live measurement. |
| CEC 3 s timeout per call | PASS. `exec.CommandContext` at `internal/input/cec.go:54` with a 3 s context built per call at `internal/input/cec.go:146`. `TestEveryCallHasADeadline` reads the deadline back off the context rather than trusting the constant. |
| Power toggles standby and image-view-on | PASS. `TestPowerAlternates` pins both full command lines in order, starting from standby because the service assumes a set showing a broadcast is on. A failed command still flips the state, which is deliberate and tested. |
| Volume sends pressed then released | PASS. `TestVolumeSendsPressThenRelease` covers up, down and mute, and `TestFailingCommandIsReportedNotFatal` proves the release is sent even when the press failed. |
| Failures logged and non-fatal | PASS, measured live. I ran `serve --cec --cec-device /dev/null` on a Mac with no `cec-ctl` and pressed `p` and `]`. Three `ERROR the television did not accept a cec command` lines naming the exact command, then playback carried on and crossed an item boundary, then a clean Ctrl-C exit. |
| Nothing runs unless `--cec` | PASS. `tv` stays nil at `cmd/channel3/serve.go:141` and the station logs `ignoring a television key, cec is off key=power`. Confirmed live. |
| Sources merged into one channel | PASS, measured. `Merge` had no test, so I asserted the contract with an overlay probe: two sources both reach the stream, a nil source is skipped, the merged channel stays open while one source lives and closes once all have closed. See WARN 3. |
| A committed change does exactly one `Load` at the new channel's offset | PASS. `TestChannelUpTunesTheNextChannel` counts the loads and compares path and offset against `schedule.At`. Structurally, mpv's `stop` end-file for the file our own `loadfile` displaced is swallowed at `internal/player/player.go:607`, so a tune cannot cause a second load through the event path. |
| Digit timeout handled by the station timer | PASS. `cmd/channel3/station.go:308` arms the timer from `Pending()` after every loop iteration and `Expire()` takes no time of its own. Go 1.26 `Reset` drains a fired channel, and a stale fire would be a no-op anyway, pinned by `TestExpireWithNothingPendingDoesNothing`. |
| `Tuned()` is goroutine-safe | PASS. `sync.RWMutex` around the one field at `cmd/channel3/station.go:179`, and `TestTunedReportsTheChannelOnAir` reads it from the test goroutine while the loop runs, under `-race`. |
| Phase 5 throttle fields cleared on tune | PASS. `tune` clears `stalled` at `cmd/channel3/station.go:151` and the `play` that follows sets `lastLoad` and `lastKnown` for the new channel, or `standby` clears both when the new channel has nothing on. No path leaves a stale item path able to suppress a reload. |
| No new persistence | PASS. Every field added to `station` is in memory, and the input package writes nothing at all. |
| The select loop still handles events, reconcile and shutdown under `-race` | PASS. `-race` green across 30 `cmd/channel3` tests, and the live runs exercised end-file boundaries, a tune, and SIGINT in the same process. |
| New dependencies | PASS. `golang.org/x/sys v0.48.0` and `golang.org/x/term v0.46.0`, nothing else in `go.sum`, no cgo, static arm64 build confirmed by `file`. |

## Deviations from the plan

| Deviation | Ruling |
|-----------|--------|
| `Tuner.Expire()` added, taking no time argument | Acceptable, and better than the alternative. The plan names `Handle` and `Pending` and says the station arms the timer; a timeout has to enter the tuner somewhere, and a method that takes no clock keeps the tuner pure rather than letting it compare times itself. |
| Tuning to the channel already on air reports no change and reloads nothing | Acceptable, and correct. Reloading would restart the current item mid-scene in answer to a button that asked for the channel already playing. Confirmed live: `already on that channel number=5 channel=clips`, no load. |
| `Tuner.Timeout` exported so tests can shorten the 1.5 s wait | Acceptable. The default is still the plan's 1.5 s, pinned by an assertion on `DigitTimeout` inside `TestPendingDeadlineIsTheLastDigitPlusTheTimeout`, and the station only sets it from `stationOptions`. |
| The terminal source raises SIGINT on Ctrl-C itself | Acceptable, and necessary. Raw mode disables the driver's signal handling, so without it the only way out of `serve` would be a kill from another window, which is exactly the case that leaves a terminal in raw mode. Ctrl-D is handled the same way. Measured: exit 0, terminal restored. |
| Mute is not on the terminal map | Acceptable. The plan's terminal list omits it, and the evdev map has it, which is where a remote's mute button arrives. |
| Device detection reads sysfs names rather than `EVIOCGNAME` | Acceptable. The plan offers either. Reading sysfs avoids opening a device the service is not going to read, which would take it from whatever else is using it. |
| A keyed number matching no channel is logged | Acceptable. The Verification checklist asks for it even though the task list does not. |

## Issues

### FAIL (must fix)

None.

### WARN (should review)

1. `internal/input/tty.go:207` — a lone Escape byte delays the next keypress instead of being discarded. `decodeTTY` breaks out of its loop whenever an escape has fewer than three bytes behind it, so after a stray Escape the next key is held until two more bytes arrive. Measured live: after sending Escape then `3`, nothing happened; sending a second `3` released both, tuning channel 3 and then logging `already on that channel`. Nothing is lost and the Pi never sees an escape byte, so this is a development-only annoyance. Fix: when exactly two bytes are buffered and the second is not `[`, consume the escape alone and let the next byte be decoded normally.

2. `cmd/channel3/station.go:348` — a CEC call runs inside the station's select loop. `RealCEC.Power` blocks for up to 3 s and the volume calls for up to 6 s, because the press and the release are two sequential 3 s commands. While the loop waits it handles no end-file event, so a television that ignores CEC can hold a channel on its last frame for several seconds per button press, and repeated presses queue behind each other. The comment at `internal/input/cec.go:15` says nothing about the broadcast may wait with it, which is true of the timeout and not yet true of the caller. It cannot bite today, because `--cec` defaults off and Phase 4 has not run. Fix before `--cec` is ever turned on in Phase 9: hand the call to a single-slot worker goroutine so a second press replaces a queued one rather than stacking.

3. `internal/input/keys.go:100` and `internal/input/evdev.go:189` — `Merge` and `nextReopenBackoff` have no tests, although both are pure Go that runs on a Mac and the plan's ground rules ask for exactly that. `Merge` carries the contract the station depends on at `cmd/channel3/station.go:235`, where a closed stream is what makes it log that the channel can no longer be changed. I verified the fan-in, the nil-source skip, the close-after-all-sources rule and goroutine cleanup with an overlay probe that never touched the tree, and all four hold, but the assertions should live in the repo. Note also that `Merge`'s per-source goroutine exits only when the source closes its channel: both shipped sources do that on context cancel, so nothing leaks today, but a future source that does not would park a goroutine for the life of the process.

4. `internal/input/evdev.go:178` — `Device.Path()` is never called, by the service or by a test. Dead code by the project's own rule. Either drop it or use it in the log line at `internal/input/evdev_linux.go:118`, which currently repeats `d.path` directly.

## Observations

Not defects, recorded because the next phase touches them.

- The executor's first Phase 7 concern is correct and is a Phase 7 requirement, not a Phase 6 defect. `station.channels` is replaced wholesale at the 04:00 rollover at `cmd/channel3/station.go:560` and read only by the loop today, so there is no race in this phase. Phase 7's `/api/channels` and `/api/guide` handlers will read it from the HTTP goroutine, and they need a guarded accessor. `station.tuner`, `excluded`, `day` and `lastLoad` are in the same position. Only `tuned` is guarded, which is what Phase 6's task list asked for.
- The executor's second Phase 7 concern is also correct and also Phase 7's. `serve` builds the station after mpv is up at `cmd/channel3/serve.go:145`, so a failing mpv returns before any listener exists. In Phase 6 there is no API to take down. Phase 7 should decide whether the API outlives a dead player, which is worth answering deliberately since a guide page that still loads is how someone diagnoses a television showing nothing.
- A tune can produce one extra load a few milliseconds before an item boundary. Live at 09:53:59 the station loaded `item=b offset=19.998s` and one second later `item=a offset=64ms`. mpv reached the end of the file marginally before the schedule's boundary, so the end-file event arrived while `schedule.At` still reported the outgoing item. This is Phase 5's end-file path, unchanged by Phase 6, and it is self-correcting and invisible on screen. It shows up here because a channel change loads mid-item, where mpv's seek and the schedule's arithmetic can disagree by a few milliseconds.

## Verdict

PASS WITH WARNINGS
