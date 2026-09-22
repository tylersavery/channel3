# Verification Report: Phase 1 — Bootstrap, channel config, library index

Reviewed 2026-09-22 against `docs/plans/plan.md`, Phase 1, by a reviewer session that did not write the code. The work is uncommitted in the working tree.

## Summary

Phase 1 is complete and correct. Every item in the phase's verification checklist passes, every case named in its Tests section exists as a real test, and the Output contract that Phases 2, 3 and 5 build on holds: the sidecar JSON matches the plan's block field for field in both the ok and the failed shape, and `library.Channel`, `library.Item`, `LoadChannels` and `Scan` have exactly the signatures the plan gives. The code is careful in places the plan did not ask for: times normalised to UTC on write, HTML escaping disabled so playlist ampersands survive, a byte-for-byte round-trip test that pins the on-disk encoding, and a non-regular-file check the plan did not require. Nothing in `internal/library` reaches the network or shells out. The warnings below are all small and none of them blocks Phase 2 or Phase 3 from starting, because none of them changes a contract those phases consume. The one that should be fixed before anyone else reads the repo is the `make run` line in `CLAUDE.md`, which describes a command that cannot work until Phase 7.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | `go test -race -count=1 ./...` green. 23 top-level tests, 11 subtests, 0 failures. 89.7% statement coverage in `internal/library`. `cmd/channel3` has no test files. |
| Matches plan | PASS | All 7 tasks done. All 6 checklist items verified independently. Sidecar schema matches the plan's JSON block field for field, including the failed-item shape. |
| Security | PASS | No network, no `os/exec`, no auth surface in this phase. `go list -deps ./internal/library` contains no `net`, `net/http` or `os/exec`. One defence-in-depth note below. |
| Code quality | PASS | `gofmt -l .` silent, `go vet ./...` clean. No TODOs, no dead code, no swallowed errors. Doc comments explain why, not what. |
| Scope | PASS WITH WARNINGS | Seven helpers were added ahead of Phase 2. Each is small, tested and part of the sidecar contract this phase owns. Ruled acceptable. |
| Integration summary | N/A | No HTTP endpoints in this phase. |

## Checklist verification

| Checklist item | Result |
|----------------|--------|
| `make lint` and `make test` pass with no gofmt output | PASS. Lint prints only the `go vet` line and exits 0. |
| `make build-arm64` produces a binary; `file` reports ARM aarch64, statically linked | PASS. `ELF 64-bit LSB executable, ARM aarch64, version 1 (SYSV), statically linked`. |
| No args prints usage and exits 2; `serve` prints "not implemented" and exits 2 | PASS. No args exits 2 with usage. `serve`, `ingest`, `guide` each print `not implemented in this phase` and exit 2. An unknown subcommand exits 2 with usage. |
| `channels/example.yaml` is the only YAML under `channels/`, `git status` clean after `make dev-root` | PASS. One file. `make dev-root` creates the three directories under `~/srv/channel3` and leaves `git status` unchanged. `.gitignore` already ignores `/channels/*.yaml` with an exception for the example. |
| Sidecar schema in code matches the JSON block field for field | PASS. Key order and names match, and `testdata/sidecars/ok.json` is a byte-for-byte copy of the plan's block. |
| No network calls anywhere in `internal/library` | PASS. Grep and the dependency tree are both clean. |

## Tests section, case by case

| Case the plan names | Where | Result |
|---------------------|-------|--------|
| Valid two-file config loads | `internal/library/config_test.go:33` | PASS, and it also asserts the sort by number. |
| Duplicate number across files rejected | `internal/library/config_test.go:63` | PASS, error names both files. |
| Bad id produces an error naming file and field | `internal/library/config_test.go:88` and `:92` | PASS, covers uppercase and a leading dash. |
| Bad number | `internal/library/config_test.go:96` and `:100` | PASS, covers 0 and 1000. |
| Unknown scheme | `internal/library/config_test.go:117` and `:122` | PASS, covers `ftp://` and a bare path. |
| Missing name | `internal/library/config_test.go:107` and `:112` | PASS, covers absent and whitespace-only. |
| Zero sources loads with a warning | `internal/library/config_test.go:175` | PASS, asserts the log text, not just the absence of an error. |
| Ok and failed sidecars round-trip byte for byte | `internal/library/sidecar_test.go:33` | PASS against both fixtures. |
| Unknown fields ignored on read | `internal/library/sidecar_test.go:129` | PASS, and it proves they are dropped on the next write. |
| Ok item present, absolute path, correct duration | `internal/library/index_test.go:63` | PASS, exact `reflect.DeepEqual` on the whole slice. |
| Missing-file item and failed item excluded | `internal/library/index_test.go:41` | PASS, and it asserts each exclusion was logged. Also covers the zero-duration case. |
| Channel with no directory yields zero items | `internal/library/index_test.go:55` | PASS. |
| Order stable across two scans | `internal/library/index_test.go:102` | PASS, plus a fixture where sidecar file name order and item id order disagree, which is the case that would catch an accidental ReadDir ordering. |

Beyond the plan the suite also covers the missing config directory, an empty config directory, the repo's own `channels/example.yaml`, a `file://` source whose sidecar carries an absolute path, an unreadable sidecar, and UTC normalisation on write. That last group is what gives the sidecar contract its teeth.

## Output contract, for Phases 2, 3 and 5

`library.Channel` at `internal/library/config.go:34` is `{ID, Number, Name, Sources []string}`, exactly as the plan writes it. `library.Item` at `internal/library/index.go:17` is `{ID, Title, Path, Duration time.Duration, Source}` with `Path` absolute, exactly as the plan writes it. `LoadChannels(dir string) ([]Channel, error)` at `internal/library/config.go:89` and `Scan(root string, channels []Channel) (Index, error)` at `internal/library/index.go:57` match their planned signatures. The sidecar struct at `internal/library/sidecar.go:36` emits `id, title, source, file, duration, ingested_at, status` for an ok item and `id, source, status, error, attempted_at` for a failed one, in that order, with no extra keys and none of the success fields present on a failure. Phase 2 can write against `MarshalSidecar` and `WriteSidecar` without reopening anything.

## Executor deviations, ruled

**Unknown key in a channel YAML is a validation error.** Acceptable. `internal/library/config.go:158` catches the `sourses:` typo the executor describes, which would otherwise produce a silent zero-source channel and a Stand By card with no explanation. The cost is that a later phase adding a config field must also extend `configFields` at `internal/library/config.go:50`, and the doc comment makes that obvious. The plan's intent was a loader that tells you everything wrong with your config in one run, and this serves it.

**`LoadChannels` errors on a missing directory.** Acceptable. The plan grants the tolerance to `Scan` and is silent about `LoadChannels`, and the two cases are genuinely different: a channel with nothing ingested yet is a normal state, whereas a root with no `channels/` directory is a misconfigured install that should say so rather than boot into an empty tuner. The error wraps `fs.ErrNotExist`, so a caller that wants the other behaviour can still ask. Documented at `internal/library/config.go:87`.

**Extra Phase 2 helpers.** Acceptable. `ChannelsDir`, `ChannelDir` and `LibraryDir` put the on-disk layout in one place instead of leaving four packages to join paths by hand. `MarshalSidecar`, `UnmarshalSidecar`, `ReadSidecar` and `WriteSidecar` are the sidecar contract, which is a named Phase 1 output. `ReadSidecar` and `ChannelDir` are already used by `Scan`; the rest are exercised by tests. `*time.Time` is the right call, because a plain `time.Time` with `omitempty` would emit the zero time on a failed item and break the failed-item shape the plan specifies.

**`Index` is opaque, callers use `index.Items(channelID)`.** Acceptable. The plan names the type and the constructor but not the shape, and an accessor keeps the per-channel map an implementation detail. See the warning below about the returned slice.

**`make run` fails until Phase 7.** Acceptable in the Makefile, WARN in `CLAUDE.md`. `Makefile:32` is the exact command the plan's task 1 asked for, so the executor was right to write it as specified rather than invent a Phase 1 variant. The problem is only that `CLAUDE.md:22` now tells every future session the command works.

## Context Health

Context scaffold not present. `.claude/context/` does not exist in this repository, so there is nothing to check for staleness. Run `/intel` if a scaffold is wanted.

## Issues

### FAIL (must fix)

None.

### WARN (should review)

- `CLAUDE.md:22` says `make run` serves on :3333, but it exits 1 with `flag provided but not defined: -listen` until Phase 7 adds the flag. Add a clause saying the flag arrives in Phase 7, or the next session will read this as a broken build.
- `internal/library/config.go:97` tracks uniqueness of `number` but not of `id`. Two files with the same id and different numbers load cleanly, and since `id` names the library directory, both channels then play the same items and `Scan` reads the same directory twice. Fix: a second `map[string]string` beside `claimedBy`, populated and checked the same way.
- `internal/library/index.go:124` reports every `os.Stat` failure as `file is missing`. A permission error under the Pi's systemd user would be logged as a missing file and send someone looking for the wrong problem. Fix: add `"err", err` to the log call at `internal/library/index.go:127`.
- `internal/library/index.go:33` returns the backing slice, so a caller that sorts or truncates it mutates the index in place. Phase 3 shuffles item order for the schedule, which is exactly the caller that would hit this. Fix: return `slices.Clone(ix.items[channelID])`, or state read-only in the doc comment and let Phase 3 clone.
- `internal/library/index.go:121` joins a relative `file` against the sidecar directory with no containment check, so a sidecar naming `../../../etc/passwd` would hand mpv a path outside the library. Sidecars are written only by our own ingest, so this is defence in depth rather than a live hole, but Phase 2 is the natural place to reject it at write time.
- `cmd/channel3/main.go:146` exits 1 for a bad flag on a subcommand while `cmd/channel3/main.go:72` exits 2 for a bad flag at top level. The plan only pins the unknown-subcommand case at 2. Fix: return `&exitError{code: 2}` so every usage error has one status, which matters once systemd is reading exit codes in Phase 9.
- `cmd/channel3/` has no test file. The dispatch, the `--version` path, the env override and the exit codes were verified by hand for this report and all behave, but nothing guards them against a Phase 5 or Phase 7 edit. A single table test over the `run` function would cover it. The plan did not ask for one, so this is a note rather than a gap against the plan.

### Suggestions

- `internal/library/config.go:100` matches `.yaml` only, so a config saved as `trains.yml` is skipped in silence apart from the `no channel config found` warning. The plan says `*.yaml`, so this is correct as written; accepting `.yml` would be a kindness later.
- `internal/library/config.go:112` lets a file that already failed validation claim its number, so a later valid file with the same number is reported as the duplicate. Every error is reported either way, so this only affects which file the message blames.

## Verdict

PASS WITH WARNINGS
