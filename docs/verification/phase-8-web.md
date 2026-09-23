# Verification Report: Phase 8 — Guide web page

## Summary

Phase 8 is complete and correct. The page consumes `docs/integration/guide-api.md` field for field, draws its progress bar from `offset` over `duration` and never from `end` minus `start`, sorts channels by number client side, renders "Please Stand By" for a channel with `now: null`, tolerates `tuned: null`, and reaches nothing but its own API with nothing but GET. I confirmed the last point on the built bundle rather than on the source: rendered headlessly against payloads captured from a live station, it issued exactly two requests, `GET /api/now` and `GET /api/guide?hours=6`. `npm run lint`, `npm run typecheck`, `npm test` and `npm run build` all pass, the tree is clean afterwards, `web/dist/.gitkeep` survives the build, and `web/embed.go`, `web/dist/.gitkeep` and `.gitignore` are absent from the diff. A binary built from this tree serves the real Vite `index.html` at `/` with `no-cache`, serves both hashed assets with a year long immutable cache, keeps answering `/api/now`, and cleans up its pid file on SIGINT, with `--ui-dir web/dist/ui` producing a byte identical page. The six warnings below are all small: one Makefile side effect the executor correctly flagged rather than fixed, one plan checklist whose literal grep no longer matches its own intent, and four cosmetic or edge case details in the page.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | vitest: 3 files, 34 tests, 0 failures. Go: 6 packages green with `go test -count=1 ./cmd/... ./internal/...`. Lint, typecheck and build all exit 0. |
| Matches plan | PASS | Tasks 1 to 7 all implemented. Every case in the plan's Tests section maps to a named test, and the fixture carries three channels where the plan asked for two. |
| Security | PASS | Read only. One `fetch` in the whole tree, GET only, relative `/api/` paths only, no host literal anywhere, no storage, no eval, no third party script. |
| Code quality | PASS | eslint strict type-checked plus stylistic type-checked and react-hooks, clean. `tsc --noEmit` clean under `strict`, `noUncheckedIndexedAccess` and `noUnusedLocals`. |
| Scope | PASS | Only files Parallel Group B assigns to Phase 8, plus the `Makefile` and `CLAUDE.md` lines it names. Nothing under `internal/`. |
| Integration summary | PASS | Types in `web/src/api.ts:7-58` match `docs/integration/guide-api.md` on every field, and match what the running station actually emitted. |

## Context Health

Context scaffold not present. `.claude/context/` does not exist; run `/intel` to generate it.

## Checklist results

| Plan checklist item | Result |
|---|---|
| `npm run lint && npm run typecheck && npm test && npm run build`, `git status` clean afterwards | PASS. All four exit 0. `git status --short` after the build lists only the three modified files and the new `web/` sources. `web/dist/ui/` is ignored and `web/dist/.gitkeep` is still tracked and still present. |
| A binary embedding the UI serves the guide at `/` with no `--ui-dir` | PASS. `GET /` is 200, `text/html; charset=utf-8`, `Cache-Control: no-cache`, and byte identical to `web/dist/ui/index.html`, not the fallback page. |
| Rows in number order, tuned highlighted, bar moves, updates within 30 s | PARTIAL. Order, highlight and bar are proved by tests and by a headless render of the built bundle. The visual and the live refresh are not run, see below. |
| Readable on a phone with no horizontal overflow | Not run, lead will do in a browser. By construction there is no fixed width above 400 px: `web/src/styles.css:65` is a `max-width`, every other fixed size is under 100 px, and `box-sizing: border-box` is global. See WARN 5 for the one case that could still overflow. |
| Stopping `serve` leaves the last data plus an inline error | Not run, lead will do in a browser. The code path is right: `web/src/App.tsx:33-40` never clears `now` on a failure and `web/src/App.tsx:108-114` renders a `role="status"` line. |
| `grep -rn "fetch(" web/src \| grep -v "/api/"` finds nothing | WARN 2. The grep returns `web/src/api.ts:71`, the single shared helper. The intent holds and was proved at runtime. |
| `web/embed.go`, `web/dist/.gitkeep` and `.gitignore` unchanged in the diff | PASS. `git diff --stat` lists `CLAUDE.md`, `Makefile` and `docs/plans/plan.md` and nothing else. |

## Additional checks

**Contract use.** `web/src/api.ts` declares `Airing`, `GuideSlot`, `NowChannel`, `NowResponse`, `GuideChannel` and `GuideResponse` exactly as the summary specifies, including `next.offset` always 0 and `slots` empty rather than null. `web/src/format.ts:65` computes the fraction as `offset / duration` with a comment explaining why the span is wrong, and `web/src/Guide.tsx:120` repeats it at the call site. `web/src/Guide.tsx:23` sorts a copy of the channel array by number, so response order is irrelevant. `web/src/Guide.tsx:24-26` keys the guide by channel id, so the later disclosure can only ever show that channel's own slots. Times go through `Intl.DateTimeFormat` with no explicit zone, which renders the station's RFC 3339 instant in the viewer's local zone.

**Runtime proof on the built bundle.** I built `cmd/channel3` from this tree, ran it against `~/srv/channel3` with `--start-channel clips --no-input --mpv-arg=--ao=null`, captured `/api/now` and `/api/guide?hours=6`, then imported `web/dist/ui/assets/index-BlVDCAP0.js` into a jsdom window with `fetch` stubbed. The bundle mounted, requested only `/api/now` and `/api/guide?hours=6`, both GET, and rendered channel 3 before channel 5 although the API returned them in that order anyway. The tuned channel carried `channel--tuned`, `aria-current="true"` and the visible "On the TV now" badge, and both progress bars carried `role="progressbar"` with `aria-valuenow` and an `aria-label`. Re-running with `tuned` forced to null rendered the same page with no channel highlighted and no error.

**Serving.** With the embedded build: `/` 200 `no-cache`; `/assets/index-BlVDCAP0.js` 200 `text/javascript; charset=utf-8` `public, max-age=31536000, immutable`; `/assets/index-CMDwDPjx.css` 200 `text/css; charset=utf-8` same cache; `/favicon.svg` 200 `image/svg+xml` `no-cache`, which is right because its name carries no hash; `/api/now` 200 `no-store`; `/assets/nope-12345678.js` 404 JSON. Vite's 8 character hash matches the tightened pattern at `internal/api/ui.go:29`, so Phase 7's caching warning is closed by this build rather than tripped by it. SIGINT removed `~/srv/channel3/serve.pid` and left no mpv behind. Repeating every request with `--ui-dir web/dist/ui` gave a byte identical `index.html` and identical headers. `~/srv/channel3` is exactly as I found it.

**Polling and the tick.** `web/src/App.tsx:10-13` sets 30 s, 5 min and 1 s. Both fetch effects clear their interval and abort their controller on unmount. The tick at `web/src/App.tsx:81-92` cannot drift or double fire: it recomputes `elapsedSeconds` absolutely from `fetchedAt` rather than incrementing, and it is keyed on `fetchedAt`, so each successful fetch tears the old interval down and starts one. A failed fetch leaves `now`, `guide` and `fetchedAt` untouched, so the last good data stays on screen and the bar keeps advancing until `advanceOffset` clamps it at the item's duration.

**Tests.** Every case the plan names is present: rounding at boundaries at `web/src/format.test.ts:67-84`, clamping at `web/src/format.test.ts:53-64`, fixed input local time at `web/src/format.test.ts:16-29`, fixture channels at `web/src/Guide.test.tsx:18-31`, tuned highlight at `web/src/Guide.test.tsx:33-44`, Please Stand By at `web/src/Guide.test.tsx:46-52`, number order regardless of fixture order at `web/src/Guide.test.tsx:20-26`. The suite is `vitest run`, not watch, and it exits. The zone is pinned twice, by `TZ` in the test script and by `test.env` at `web/vite.config.ts:21`; I proved the config alone is enough by running `TZ=UTC npx vitest run`, which still passed 34 of 34.

**Accessibility and layout.** Single column, two columns only above 900 px. Root type is 18 px, channel names 1.15 rem, titles 1.3 rem, the number 2.25 rem. The only interactive element is the disclosure summary, about 48 px tall with its padding, and it has a `:focus-visible` outline. `prefers-color-scheme` and `prefers-reduced-motion` are both handled. The tuned state is carried by a left border, a background shift, `aria-current` and a text badge, so it is never colour alone. Contrast, computed on the actual hex pairs, clears WCAG AA everywhere.

| Pair | Dark | Light |
|---|---|---|
| body text on a channel card | 15.5 | 17.8 |
| muted text on a channel card | 9.1 | 7.6 |
| channel number on a tuned card | 9.4 | 6.9 |
| badge text on the badge | 9.7 | 5.0 |
| bar fill against its track | 6.7 | 3.9 |

**Makefile and Node.** `ui` is `cd web && npm ci && npm run build`, `ui-dev` is `npm run dev` which is `vite build --watch` and not a server, and `build` and `build-arm64` both depend on `ui`. Phase 7's rule still holds: with Node off `PATH` entirely, `go build ./...` and `make lint` both succeed, because `web/dist/.gitkeep` keeps the embed pattern matching.

**Dependencies.** Runtime dependencies are `react` and `react-dom` and nothing else. No router, no state library, no component kit, no CSS framework. Dev dependencies are the Vite, TypeScript, ESLint and Vitest toolchains plus Testing Library. `package-lock.json` is present at lockfile version 3.

| Package | Declared | Locked |
|---|---|---|
| react | ^19.2.8 | 19.3.0 |
| vite | ^8.3.0 | 8.3.0 |
| vitest | ^4.1.11 | 4.1.11 |
| typescript | ~6.0.2 | 6.0.3 |

## Issues

### FAIL (must fix)

None.

### WARN (should review)

- `Makefile:34` and `Makefile:37` The Go targets still say `./...`, and `./...` now resolves one package inside the npm tree: `go list ./...` returns `github.com/tylersavery/channel3/web/node_modules/flatted/golang/pkg/flatted`. Today `go build`, `go vet` and `gofmt -l .` are all clean over it and the lockfile makes that deterministic, so nothing is broken. It does mean an npm dependency can now break `make lint` or `make test` by shipping a Go file that does not compile or is not gofmt clean. The executor was right not to change Phase 1 semantics inside Phase 8. The fix is one line when someone wants it: scope the Go targets to `./cmd/... ./internal/... ./web` and point `gofmt` at `cmd internal web tools` rather than `.`.
- `docs/plans/plan.md:743` The checklist reads "`grep -rn "fetch(" web/src | grep -v "/api/"` finds nothing", and it finds `web/src/api.ts:71`, the one shared helper that both fetchers call with a literal `/api/` path. The check's intent is satisfied and I proved it at runtime rather than by grep. Amend the checklist wording to something the code can pass, such as requiring exactly one `fetch(` in `web/src` and every URL literal beginning `/api/`.
- `web/src/Guide.tsx:154-161` The later disclosure says "The guide has not loaded yet." whenever `guideByID` has no entry for the channel, which also covers the case where the guide did load but does not list that channel. The API returns the same channel set from both endpoints, so this needs a config change mid-poll to happen at all, and the wording would then be wrong rather than harmful. Distinguishing `guide === null` from a missing id would cost two lines.
- `web/src/format.ts:128` and `web/src/Guide.tsx:163` The later list is capped at 40 slots with nothing on screen to say it was cut. On the short clip channel I tested, six hours is over a thousand slots and the cap is clearly right, but a reader sees the list simply stop. A trailing "and more after that" line would close it.
- `web/src/styles.css:217` and `web/src/styles.css:273` Titles have no `overflow-wrap` or `word-break`. Normal titles wrap on spaces and the layout has no fixed width above 400 px, so this is fine for every real title in the library, but one very long unbroken token, which is what a yt-dlp title of a video named after a URL would be, could push a card past the viewport. `overflow-wrap: anywhere` on `.channel__title`, `.channel__nexttitle` and `.later__title` removes the risk.
- `web/src/App.tsx:101` The `datetime` attribute on the header clock stays at the instant the station reported while the visible text ticks forward, so the machine readable value and the rendered one disagree by up to 30 seconds. Nothing consumes the attribute today. Either drop it or recompute it alongside the text.

## Not run

The three checks that need a real browser are not run, not failed. The lead should confirm in a browser and on a phone: that the rows read at arm's length, that the bar visibly moves, that nothing overflows horizontally at phone width, that both colour schemes look right, and that the page picks up a channel change made from the terminal within 30 seconds.

## Ruling on the executor's reported deviations

All seven are acceptable and none needs rework.

Pinning vitest to 4.1.11 rather than 5 is forced: npm 10.8.2 crashes in arborist resolving vitest 5's optional browser peers, and the executor reproduced it in an empty directory, so it is the package manager and not this project. vitest 4 runs the suite and exits, which is all the plan asks of it. jsdom 29 and `@testing-library/jest-dom` 6 are likewise forced by Node 20 engine constraints, and the newer majors would not install at all.

Choosing ESLint over the template's oxlint is the correct reading of the plan, which names `eslint.config.js` by file. Omitting `.prettierrc` is within the plan's own "if desired". Adding `src/api.test.ts` is five tests of coverage the plan did not ask for and is welcome, including the one that proves a non-2xx surfaces the station's own error string.

Generating the fixtures rather than copying the contract examples verbatim is the one deviation worth a sentence of reasoning, and it is right. The contract's own emphasis is that `duration` is never `end` minus `start`, and a fixture where the two agree cannot test it. The generated fixture puts an airing cut at the 04:00 rollover into `web/src/fixtures/now.json`, where the span is 441 s and the duration is 1368 s, and `web/src/format.test.ts:136-150` and `web/src/Guide.test.tsx:54-64` both fail if anyone reaches for the span. Channels out of number order and one channel at `now: null` are in there for the same reason. The cost is that the fixture is no longer a literal copy of the contract, so it would not catch a field rename by inspection alone; I covered that by checking the types against what the running station actually emits, and they match.

Capping the later list at 40 slots is a judgement the plan left open and the right one, subject to WARN 4 about saying so on screen.

The two items the executor flagged for me are both handled above: the checklist grep is WARN 2, and `go build ./...` reaching into `node_modules` is WARN 1.

## Verdict

PASS WITH WARNINGS
