# Verification Report: Phase 2 — `channel3 ingest`

Reviewed 2026-09-22 against `docs/plans/plan.md`, Phase 2, by a reviewer session that did not write the code. The work is uncommitted in the working tree. This reviewer was spawned by an orchestrator lead and does not commit.

## Summary

Phase 2 is complete and correct. All nine tasks are implemented, every case named in the phase's Tests section maps to a real test, and every item in the Verification checklist passes against the live root at `~/srv/channel3`, including the line the lead amended after the executor showed the original could not hold. The three behaviours that matter most for the rest of the plan all hold under hand testing: a failure never aborts a run, the exit codes are 0, 2 and 1 exactly as the plan states, and a failed sidecar carries the first stderr line and `attempted_at` with no `file` and no `duration`. The sidecar written is byte-compatible with Phase 1's contract, so Phase 3 and Phase 5 can read it without reopening anything. Duration matches `ffprobe` exactly rather than to the millisecond: the sidecar records `60.093243` and `ffprobe` reports `60.093243`.

The warnings are all robustness and defence in depth. None of them changes a contract a later phase consumes and none of them blocks Phase 5. The one worth fixing first is the path traversal at `internal/library/ingest.go:449`, which this reviewer reproduced: a yt-dlp entry id containing `../` writes a sidecar outside the channel directory. It is not reachable from a real YouTube playlist, whose ids are eleven safe characters, but the fix is three lines and the same validation closes the sidecar `file` hole Phase 1's reviewer left open.

Two things the executor did beyond the plan deserve credit. Removing a stale source-level failure sidecar on a successful expansion is not optional polish, it is the only thing that ever clears that file, and this reviewer watched it work end to end on the live root. Preferring `ba[ext=m4a]` produced an AAC track in the real download, so the library is checkable in QuickTime, which is what the executor said it was for.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | `go test -race -count=1 ./...` green. 66 top-level tests, 39 subtests, 0 failures, 1 skip (the network-gated test). Coverage 82.9% in `internal/library`, 57.0% in `cmd/channel3`. `CHANNEL3_NETWORK_TESTS=1 go test ./internal/library/ -run Network` passes in 4.79 s. |
| Matches plan | PASS | All 9 tasks done. All 5 checklist items verified independently against `~/srv/channel3`. Every case in the Tests section has a named test. |
| Security | PASS WITH WARNINGS | No `net/http` anywhere. `os/exec` only in `ytdlp.go`, `probe.go` and their tests. `go list -deps` on the whole tree pulls in no `net`. Three defence-in-depth warnings below, one of them reproduced. |
| Code quality | PASS | `gofmt -l .` silent, `go vet ./...` clean. No TODOs, no dead code, no swallowed errors. Doc comments explain why. `make build-arm64` produces a statically linked aarch64 binary. |
| Scope | PASS | Nothing from Phase 3 or Phase 5 is implemented here. `PIDFile` is one function and it is the guard's own constant. |
| Integration summary | N/A | No HTTP endpoints in this phase. |

## Checklist verification

Each of these was run by this reviewer, not taken from the executor's report.

| Checklist item | Result |
|----------------|--------|
| `make lint` and `make test` pass | PASS. Lint prints only the `go vet` line and exits 0. |
| Real config with one YouTube video and one `file://` source, dry run then real run; both get ok sidecars; duration matches `ffprobe` to the millisecond | PASS. A dry run into a clean root printed two `plan` lines and created no `library` directory at all. The video sidecar records `60.093243` against `ffprobe`'s `60.093243`, and the local one `20` against `20.000000`. The media is H.264 at 720p with an AAC track, so it plays in QuickTime. |
| Run again: every entry says `skip`, nothing downloaded or probed, media mtimes unchanged; source expansion still calls yt-dlp, so `--yt-dlp /usr/bin/false` fails at expansion | PASS, in the amended wording. A second run printed `skip   test/8-X8acD_r38` and `skip   test/a` with mtimes byte-identical before and after. `--ffprobe /usr/bin/false` also exits 0, which proves no probe ran on a skip. `--yt-dlp /usr/bin/false` exits 2 and writes a source-keyed failure sidecar, and the next ordinary run logs `the source expanded again, clearing its failure record` and removes it. |
| Bogus URL: exit code 2, failed sidecar with a readable error, ok items untouched | PASS. Exit 2. The sidecar holds `"error": "yt-dlp: ERROR: [youtube] zzzzNOTAVID: This video is unavailable"` with `attempted_at` and no `file` or `duration`. Both ok sidecars and the mp4 kept their original mtimes. |
| `grep -r "net/http" internal/ cmd/` shows nothing | PASS. No match. |
| `CHANNEL3_NETWORK_TESTS=1 go test ./internal/library/ -run Network` passes | PASS. |

Beyond the checklist, this reviewer deleted a sidecar while leaving its media in place and ran ingest again. The item was rebuilt as `ok` with the same duration and the mp4's mtime unchanged, so `--no-overwrites` stopped a nine megabyte re-download and the library repaired itself from the file already on disk. That is the right behaviour and nothing in the plan required it.

## Tests section, case by case

| Case the plan names | Where | Result |
|---------------------|-------|--------|
| Single video produces an ok sidecar with title, duration from the prober, and a file that exists | `internal/library/ingest_test.go:188` | PASS. Compares the whole struct, and separately asserts `ingested_at` came from the run's clock. |
| A playlist of three produces three | `internal/library/ingest_test.go:234` | PASS, and it asserts the downloads happened in playlist order, not sorted order. |
| A failing entry produces a failed sidecar and the run continues | `internal/library/ingest_test.go:270` | PASS. Asserts the exact error string, no `file`, no `duration`, an `attempted_at`, no `ingested_at`, and that both neighbours are still ok. |
| An existing ok sidecar is skipped and the fake runner records no call | `internal/library/ingest_test.go:321` | PASS, and it also asserts the prober was not called. |
| A failed sidecar is retried | `internal/library/ingest_test.go:354` | PASS. |
| `file://` writes an absolute `file` and no copy | `internal/library/ingest_test.go:421` | PASS. Asserts the runner was never reached and that the channel directory holds exactly one file, the sidecar. |
| Duplicate local slugs are rejected before any runner call | `internal/library/ingest_test.go:463` | PASS. Asserts neither the runner nor the prober was called, and that the error names the channel, the slug and the offending file. |
| Exit codes as specified | `cmd/channel3/ingest_test.go:31` | PASS. Six subtests covering 1 for a bad flag, 1 for a stray argument, 1 for a config error, 1 for a missing config directory, 2 for a failed item, and 0 for nothing to do. |
| One real short public-domain video into a temp root, ok sidecar and a playable mp4, skipped by default | `internal/library/ingest_network_test.go:29` | PASS. Also re-probes the downloaded file as the playability check, runs `Scan` over the result, and runs a second ingest to prove the media is not re-fetched. |

Task 5's third clause, a sidecar marked `ok` whose file is missing, is not in the Tests list but is covered at `internal/library/ingest_test.go:390`. The source-level failure lifecycle has three tests of its own at `:507`, `:555` and `:597`, and the format selector's height cap is pinned at `internal/library/ytdlp_test.go:221`.

## Executor deviations, ruled

**Format selector prefers `ba[ext=m4a]` before the plan's `+ba`.** Acceptable. The plan called its own selector "a reasonable starting point", so it invited exactly this. The reason given holds: the real download produced an AAC track rather than Opus in mp4, which is what makes the library checkable on a Mac. The safety property the Pi depends on is preserved and pinned by a test that walks every branch of the selector and fails any branch missing `height<=1080` (`internal/library/ytdlp_test.go:221`). The third branch drops the `avc1` requirement, but only after both H.264 branches have failed, where the alternative is no video at all.

**Source expansion is unconditional.** Acceptable, and the lead's amendment to the checklist is the right resolution. The plan's task 1 requires playlists to be expanded and its Objective requires a video added to a playlist to be noticed, which cannot be done without asking yt-dlp every run. The original checklist line asserted the opposite and could not hold. What the plan actually cares about, that an already-ingested item costs nothing, is preserved and was verified two ways: `--ffprobe /usr/bin/false` still exits 0, and the media mtimes do not move. The cost is that ingest cannot run fully offline, which no phase requires.

**A successful expansion removes a stale source-level failure sidecar.** Acceptable, and closer to the plan's intent than not doing it. A source-level failure is keyed by a hash of the URL rather than a video id, so unlike an entry's own failure it is never overwritten by a later success, and the library would show a failure that had been fixed until someone deleted the file by hand. The removal is narrow: `internal/library/ingest.go:265` removes the file only when its status is `failed` and its `source` matches the URL just expanded, and `internal/library/ingest_test.go:597` proves a failure belonging to a different source survives. This reviewer watched the whole cycle on the live root.

**`library.PIDFile(root)` is defined here for Phase 5 to write.** Acceptable. The guard is Phase 2's task 8, the guard needs the path, and one exported function is better than two packages joining the same string. Phase 5 imports it rather than inventing a second spelling.

## Context Health

Context scaffold not present. `.claude/context/` does not exist in this repository. Run `/intel` if a scaffold is wanted.

## Issues

### FAIL (must fix)

None.

### WARN (should review)

- `internal/library/ingest.go:449` joins a yt-dlp-supplied entry id straight into a file path with no validation, so an id containing `../` writes a sidecar outside the channel directory. Reproduced with a stub yt-dlp returning an entry id of `../../../escaped`: the sidecar landed three levels above the root. Real YouTube ids are eleven safe characters, so this is not reachable from an ordinary playlist, but yt-dlp supports many extractors whose id comes from the remote site. Fix: reject any entry whose id is not `^[A-Za-z0-9_-]+$` at `internal/library/ytdlp.go:189`, alongside the existing empty-id check, and log the skip. The same predicate applied to a sidecar's `file` on write would close the traversal Phase 1's reviewer left open at `internal/library/index.go:121`.
- `internal/library/ytdlp.go:134` and `internal/library/probe.go:40` use `exec.Command` with no context, so a yt-dlp or ffprobe that hangs blocks ingest forever with no way out but a signal. A download has no sensible fixed deadline, but expansion and ffprobe do. Fix: `exec.CommandContext` with a few minutes for expansion and thirty seconds for ffprobe, leaving the download uncapped or generously capped.
- `internal/library/ytdlp.go:272` returns the first non-blank line of stderr, which is a `WARNING:` line whenever yt-dlp emits one before the real `ERROR:`. `internal/library/ytdlp_test.go:207` encodes this as correct, and it is what the plan literally says, but the consequence is a failed sidecar whose stated reason is "unable to extract the thumbnail" for a video that actually needs a sign-in. That message is the whole point of the failed sidecar. Fix: prefer the first line beginning with `ERROR:` and fall back to the first non-blank line.
- `internal/library/ingest_test.go:757` writes `0` into the pid file and calls it a stale pid, but `internal/library/ingest.go:167` treats any pid at or below zero as unreadable and returns before `processAlive` is ever called. The liveness check the plan asks for is therefore untested. This reviewer confirmed all four branches by hand against the built binary: a live pid refuses with exit 1, a dead pid of 99998 logs `ignoring a stale pid file` and proceeds, and both `0` and `not-a-pid` take the unreadable branch. Fix: change the fixture to a high pid that is not running, and keep a separate case for zero.
- `internal/library/ytdlp.go:94` does not pass `--ignore-config`, so a developer's own `~/.config/yt-dlp/config` is read on every run. A global `--write-info-json` there would drop metadata files into the channel directory, which is exactly what the comment at `internal/library/ytdlp.go:87` says must not happen, and a global output template would move the media out from under the sidecar. Fix: add `--ignore-config` to both the expand and the download argument lists.
- `internal/library/ytdlp.go:133` and `internal/library/probe.go:39` collect stderr into an unbounded `bytes.Buffer`. `--no-progress` keeps this small in practice and the buffer is freed when the call returns, so the exposure is one pathological run rather than a leak. Fix if convenient: cap it at a few kilobytes, since only the first line is ever read.
- `internal/library/ingest.go:432` decides a `file://` item is already ingested from the file's existence alone, never its mtime or size. Replacing a local clip in place leaves the old duration in the sidecar, and since the schedule's boundaries are cumulative that shifts everything after it on that channel. Phase 5's thirty-second reconcile corrects the drift at playback time, and the workaround is to delete the sidecar, so this is a note rather than a defect. Worth a line in the eventual operator docs.

### Suggestions

- `internal/library/ingest.go:194` checks slug collisions only between two `file://` sources. A local slug that collides with a remote video id in the same channel would make the second item skip silently and vanish from the channel. Slugify lowercases, so a collision with a YouTube id is close to impossible, but the check could cheaply cover both kinds of id.
- `internal/library/ingest.go:78` prints the summary only on a clean return, so a run that dies writing a sidecar reports nothing about the items it already finished. A deferred summary would be kinder to whoever reads the log.

## Verdict

PASS WITH WARNINGS
