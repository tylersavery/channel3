# Verification Report: Phase 7 — HTTP API and embedded UI plumbing

## Summary

Phase 7 is complete and correct. The three read-only endpoints match the Parallel Group B contract field for field, times are RFC 3339 in the broadcast clock's zone, `duration` is always `Item.Duration` and never `end` minus `start`, channels are sorted by number in all three responses, `hours` defaults to 6 with 1 and 48 as inclusive bounds, and every error is `{"error": ...}` with `Content-Type: application/json; charset=utf-8`. Nothing in `internal/api` can change what plays: the package's only non-standard-library import is `internal/schedule`, there is no write endpoint, and every method other than GET and HEAD is a 405 with `Allow: GET, HEAD`. The two requirements handed over by the Phase 6 review are both met: `station.Channels()` copies the slice under the same mutex that guards every write to it, and the HTTP listener opens before mpv is launched. The race test that covers the new accessor was proved real, not decorative. The three warnings below are all forward-looking: a caching heuristic that Phase 8 could trip, an overstated claim in the integration summary, and a plan line whose wording no longer matches its own ticked checkbox.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | `go test -race -count=1 ./...` green across 6 packages, 410 passing cases, 0 failures. `internal/api` contributes 51 (24 top level plus 27 subtests). |
| Matches plan | PASS | Tasks 1 to 5 all implemented; every case in the plan's Tests section maps to a named test; Task 4's startup order is deliberately inverted, see WARN 3. |
| Security | PASS | Read only, no auth by design (home network), no traversal out of the FS, no directory listings, no outbound network, no disk writes. |
| Code quality | PASS | `make lint` clean. Handlers are short, dependency-injected and free of state. |
| Scope | PASS | Only the files Parallel Group B assigns to Phase 7 were touched. `web/` holds `embed.go` and `dist/.gitkeep` and nothing else. |
| Integration summary | PASS | `docs/integration/guide-api.md` matches the handlers on every field, header, status and error string I checked; all seven deviations are called out. One sentence overstates a guarantee, see WARN 1. |

## Context Health

Context scaffold not present. `.claude/context/` does not exist; run `/intel` to generate it.

## Checklist results

| Plan checklist item | Result |
|---|---|
| `make lint`, `make test`, `make build-arm64` on a fresh copy without npm | PASS. Copied the tree without `.git`, `bin` or `node_modules`, ran with `env -i PATH=/opt/homebrew/bin:/usr/bin:/bin` so no Node was reachable. `go build ./...`, `go test ./...`, `make lint` and `make build-arm64` all pass; the arm64 artefact is a statically linked ARM aarch64 ELF. |
| `/api/channels`, `/api/now`, `/api/guide?hours=2` shapes | PASS. Verified against the running service on `~/srv/channel3` with `test` at number 3 and `clips` at number 5. Guide slots are contiguous, the first slot starts before `from`, the last ends at or after `to`, and `to` minus `from` is exactly the requested hours at 1, 2 and 48. |
| `now.offset` agrees with the log | PASS. At 10:44:53 the API reported item `a` at offset 13.0018 s; the log's last load was item `a` at 10:44:40 with offset 18 ms, which predicts 13.018 s. Agreement is within 0.02 s. |
| `tuned` is `clips` | PASS. |
| `hours=99` is a 400 with a JSON error | PASS. Also checked 0, -1, 2.5 and abc: all 400 with an `error` field. 1 and 48 are 200. |
| `GET /` is the fallback page | PASS. 200, `text/html; charset=utf-8`, `Cache-Control: no-cache`, and `/api/channels` still answers 200 behind it. |
| POST, PUT, DELETE are 405 | PASS. Also PATCH. All four methods against all three API paths and `/` return 405 with `Allow: GET, HEAD` and a JSON error body. |
| `lsof` on 3333 shows only `channel3`; nothing of ours on 3000 | PASS. Port 3333 had exactly one listener, `channel3`. Port 3000 is Waypoint's node process, unrelated to this project, and no channel3 process ever bound it. |
| Pid file gone after SIGINT | PASS. Verified for SIGINT and separately for SIGTERM: process exits, `serve.pid` is removed, port 3333 is released, no mpv left behind. |
| `docs/integration/guide-api.md` matches the handlers | PASS, with WARN 1. |

## Additional checks

**Concurrency.** `Channels()` at `cmd/channel3/station.go:196` takes `mu.RLock` and returns a fresh slice; every write goes through `setChannels` at `cmd/channel3/station.go:184`, which takes `mu.Lock`, and both the startup assignment and the rollover assignment use it. `Tuned()` is still guarded by the same mutex. The station's `playable` at `cmd/channel3/station.go:684` builds a new `Items` slice on a copy of the `Channel` value rather than mutating the stored one, so sharing the inner slices with the API's copy is safe. Handlers hold no lock while encoding: `sorted()` clones what `Channels()` already returned and the lock is long released before `json.Marshal` runs.

**The race test is real.** I copied the tree, deleted the `RLock`/`RUnlock` pair from `Channels()`, and `go test -race -run TestChannelsIsSafeWhileTheListIsReplaced` reported `WARNING: DATA RACE` and failed. Restoring the lock passed. The test is not a no-op.

**Startup order.** Forced the pre-station window open with an mpv wrapper that sleeps before exec. During that window `/api/channels` answered 200 with `{"channels":[]}`, `/api/guide` answered 200 with an empty channel list and a correct `from` and `to`, `/api/now` answered `"tuned": null`, and `/` answered 200. The listen address is logged exactly once as `guide listening address=[::]:3333`. Pointing `--mpv` at a nonexistent binary still exits 1 after the listener came up, so a broken player is still a non-zero exit for systemd.

**HTTP server.** `ReadHeaderTimeout` 5 s, `ReadTimeout` 10 s, `WriteTimeout` 30 s, `IdleTimeout` 60 s, and a 5 s graceful `Shutdown`. Confirmed clean shutdown on both SIGINT and SIGTERM.

**Static serving.** Directory paths `/assets`, `/assets/` and `/nested/deeper` all return 404, never a listing. Traversal was tried with `curl --path-as-is` and with a raw socket request: `/../embed.go`, `/../../etc/passwd` and `/nested/../../embed.go` are cleaned by `net/http`'s mux into a 307 whose target then 404s, and the percent-encoded forms `/%2e%2e/...` and `/..%2f...` 404 directly. With `--ui-dir` pointed at a build directory, a file one level above it was unreachable by every form. `index.html` gets `no-cache`, a hashed asset gets `public, max-age=31536000, immutable`, an unhashed file gets `public, max-age=3600`, and the API kept answering throughout.

**Fresh-clone rule.** `web/dist/.gitkeep` is not ignored and is staged for the first commit; `.gitignore` ignores `web/dist/ui/` only; `//go:embed all:dist` compiles with `.gitkeep` as the only file under `dist`. Confirmed in the Node-free copy.

**No new dependencies.** `go.mod` and `go.sum` are unchanged. `go list -deps ./internal/api` reaches `internal/schedule` and the standard library only, never `internal/player`. No `os.WriteFile`, `os.Create` or `exec.Command` anywhere in `internal/api` outside the golden-file `-update` path in the test.

## Issues

### FAIL (must fix)

None.

### WARN (should review)

- `internal/api/ui.go:22` The hashed-asset regex `-[0-9A-Za-z_]{8,}\.[0-9A-Za-z]+$` matches any name with a hyphen followed by eight or more word characters, not just a content hash. I confirmed by calling `cacheControl` directly that `apple-touchicon.png`, `manifest-webmanifest.json` and `logo-horizontal.svg` are all served with a year-long immutable cache. A file so labelled cannot be updated in a browser that has cached it, so a redeploy would silently serve the old bytes. Tighten the pattern to Vite's actual shape, such as requiring a base64url-looking run of exactly 8 to 12 characters and a known asset extension, or give Phase 8 a rule that unhashed assets must not contain a hyphen. The `favicon.ico` case in the existing test passes only because that name has no hyphen at all.
- `internal/api/server.go:8` and `docs/integration/guide-api.md:15` Both state that the guide and the screen can never disagree about what is on. That is not true once the station has excluded an item. `cmd/channel3/station.go:684` drops an item mpv failed to open for the rest of the broadcast day, `Deps` has no way to read that exclusion set, and so `/api/now` goes on reporting the excluded item as airing while a different one is on screen. This is a correct scoping decision for Phase 7, since the plan's contract says nothing about exclusions, but the claim should be softened to something like "the API reads the same schedule the broadcast loop reads" and the exclusion gap recorded as a known divergence, or an exclusions accessor added to `Deps` in a later phase.
- `docs/plans/plan.md:655` Task 4 is ticked but still reads "started after the station is up so `/api/now` never sees a nil tuned channel", which is the opposite of what the code does and of what the Phase 6 review asked for. The executor flagged the inversion in its report and in the integration summary, which satisfies the ground rule about reopening contracts, but the plan line itself should be amended so Phase 9 does not read the tick literally and design the systemd unit around a guarantee that no longer holds.
- `internal/api/handlers.go:248` `parseHours` treats an explicitly empty `?hours=` as omitted and answers with the 6 hour default, while `docs/integration/guide-api.md:91` enumerates what is rejected and does not mention the empty case. Harmless in practice; worth one sentence in the summary or an explicit rejection.

## Ruling on the executor's reported deviations

All seven are acceptable and all seven are documented in the integration summary, which is what the ground rule requires. Taking them in order: the listener starting before mpv is the Phase 6 review's explicit handover requirement and is the better appliance behaviour, subject to WARN 3 about the plan's wording; accepting HEAD alongside GET cannot change state and is answered by `net/http` from the same handler; a JSON 404 for unknown `/api/` paths is what a client parsing the body needs; cutting `now.end` at the 04:00 rollover exactly as guide slots are cut is required for the two endpoints to agree about the same airing, and the `duration` field still carries the item's own length so no client loses information; a JSON 404 rather than the fallback page for a missing asset is correct, since handing a browser HTML where it asked for JavaScript is the worse failure; failing `serve` on an address already in use is right for a boot-time service that has a pid-file guard against a second instance; and the `schedule.Guide(ch, now, 0)` signature in Task 1 was always shorthand, with the real four-argument call being equivalent.

## Verdict

PASS WITH WARNINGS
