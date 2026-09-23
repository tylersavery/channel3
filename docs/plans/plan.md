# Plan: Channel Three MVP

Date: 2026-09-22. Planned from `docs/plans/2026-09-22-channel-three-brainstorm.md` and `CLAUDE.md`. The vault note at `~/tybrain/projects/channel-three.md` is supplementary; the brainstorm wins on any conflict. Stack, rulings and non-goals are fixed by the brainstorm and are not reopened here.

## Goal

A Raspberry Pi 5 that boots straight into a themed channel of approved videos, mid-show at the right timecode, with no menus, spinners or chrome. A few-button remote changes channel. A phone or laptop on the home network can open a guide showing what is on now and next per channel. Curating a video means ingesting it with yt-dlp into a local library; playback is mpv on local files only. What plays is a pure function of the clock and the channel config, so nothing about playback is ever stored.

## How to work this plan

Each phase is executed in a fresh session with `/execute N`, then reviewed in a second fresh session with `/verify N`, which writes a report to `docs/verification/` and commits on pass. Executors do not commit. Read this section, the Ground rules, and your phase. You do not need to read the other phases.

Every phase uses the same four skills: `execute` for implementation, `work-discipline` while implementing, `commit-conventions` for the verifier's commit, and `verify` for review. Phases do not repeat this list unless they add something.

Hardware is not yet purchased. Phases and tasks marked **(hardware-gated)** need the Pi, the TV, the Flirc or the remote. Everything else runs on the Mac. When a phase has gated tasks and the hardware is absent, finish all other tasks, leave the gated ones unchecked with a one-line note, and the verifier records them as WARN rather than FAIL. Phase 9 re-runs every gated check before the plan is closed.

The sizing target from the `phase-breakdown` skill is one to three hours of focused execution per phase, reviewable in one PR. Where this plan splits or merges an item from the brainstorm's MVP list, it says so in that phase's Objective.

## Ground rules for every phase

- **Schedule is pure.** `(channel, t) -> (item, offset)`. Seeded shuffle per broadcast day, cumulative durations, recomputed at every boundary. No playback state is persisted anywhere, ever. No "last channel", no "resume position".
- **Ingest is the only code that touches the network.** `internal/library` ingest code may call yt-dlp. Nothing in `schedule`, `player`, `input` or `api` opens a network connection except the API's own listener.
- **Content stays out of the repo.** Real channel config and media live at `/srv/channel3` on the Pi and `~/srv/channel3` on the Mac. The repo carries `channels/example.yaml` and test fixtures only. `.gitignore` already enforces this; do not weaken it.
- **Broadcast mode has no controls.** No pause, no seek, no on-screen text, no HTTP endpoint that changes what plays. The only control is channel change from the remote or the dev keyboard source.
- **Tests live beside code.** Table tests for `schedule`. Fixture-driven tests for `library`. A fake IPC socket for `player`. Pure logic (tuner, IPC framing, evdev byte parsing) is unit tested on the Mac even when the device that produces the bytes only exists on the Pi.
- **One static binary.** `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...` must succeed at the end of every phase from Phase 1 on. No cgo dependencies.
- **Dev port is 3333.** Never 3000. This plan adds no second port.
- **Do not refactor while building.** Note it in the wrap-up and move on.
- **Docs written in this repo use one line per paragraph.** No hard wraps, no em-dashes.
- **Later phases must not reopen earlier contracts** (sidecar schema, schedule signatures, API shapes) without flagging it in the wrap-up so the plan can be updated.

## Scope

### In scope

1. `channels/example.yaml`, the YAML loader, the sidecar schema and the library index (Phase 1).
2. `channel3 ingest` with yt-dlp for YouTube and any other yt-dlp-supported URL, playlist expansion, local `file://` sources, failure sidecars and retry on the next run (Phase 2).
3. The `internal/schedule` package with table tests, plus a `channel3 guide` text command that exercises it (Phase 3).
4. The one-time hardware checklist: mpv on DRM/KMS without a desktop, 1080p H.264 smoothness, audio over HDMI, TV power cycling, Flirc key codes, `cec-ctl` against the real TV (Phase 4).
5. The mpv supervisor and `channel3 serve` playing one channel from boot, including the "Please Stand By" card, restart on mpv exit, dropping corrupt items for the day, and self-correction after a clock jump (Phase 5).
6. Channel up, channel down and digit entry from any evdev keyboard including the Flirc, a stdin key source for Mac development, and power and volume forwarded to `cec-ctl` when the TV supports CEC (Phase 6).
7. `GET /api/now`, `GET /api/guide?hours=N`, `GET /api/channels`, and serving the embedded web build on :3333 in dev and :80 on the Pi (Phase 7).
8. A read-only React guide page: now and next per channel, progress, tuned channel highlighted, phone-first layout (Phase 8).
9. A systemd unit, a one-time Pi setup script, a deploy script (arm64 build, scp, restart), an ingest wrapper, and quiet boot so the TV never shows console text (Phase 9).

### Out of scope

Everything in the brainstorm's Non-goals section plus a few things that would tempt an executor. Do not build any of these in an MVP phase.

- Admin UI for editing channels. Config is a YAML file edited by hand.
- Authentication of any kind. The box is on the home network only.
- Movie mode, pause, seek, or any playback control beyond channel change.
- Bumps, station idents, "coming up next" cards, dayparts, sign-off at bedtime.
- Playing through YouTube's embed or any browser.
- Multiple TVs or multiple players.
- Changing channel from the web UI or any HTTP endpoint.
- Rendering the guide on the TV as a channel.
- NAS integration, a prune command, storage quotas.
- Transcoding at ingest, HEVC output, subtitles, multiple audio tracks. Ingest caps at 1080p H.264 and stops there.
- Persisting anything about playback: last channel, positions, exclusion lists.
- A Wayland or X kiosk. That is the documented fallback only if Phase 4 proves DRM/KMS unworkable, and it would trigger a plan update, not a silent switch.
- Hot reload of channel config or library while `serve` runs. The library is rescanned at startup and at the 04:00 rollover only.
- Vimeo-specific handling. yt-dlp accepts Vimeo URLs through the same path; nothing special is written or tested for it in MVP.

## Phase map

| Phase | Name | Directory | Hardware | Estimate |
|---|---|---|---|---|
| 1 | Bootstrap, channel config, library index | `cmd/channel3/`, `internal/library/`, `channels/` | No | 2 h |
| 2 | `channel3 ingest` | `internal/library/`, `cmd/channel3/` | No | 3 h |
| 3 | Schedule package and `channel3 guide` | `internal/schedule/`, `cmd/channel3/` | No | 2 h |
| 4 | Pi hardware checklist | `docs/` | Yes, entirely | 3 h hands-on |
| 5 | Player supervisor and `channel3 serve` | `internal/player/`, `cmd/channel3/` | Pi verification task only | 3 h |
| 6 | Channel switching: tuner, keyboard, Flirc, CEC | `internal/input/`, `cmd/channel3/` | evdev, Flirc and CEC tasks only | 2.5 h |
| 7 | HTTP API and embedded UI plumbing | `internal/api/`, `web/embed.go`, `cmd/channel3/` | No | 2 h |
| 8 | Guide web page | `web/` | No | 2.5 h |
| 9 | systemd, deploy script, Pi setup, quiet boot | `deploy/` | Yes, entirely | 2.5 h |

Order and parallelism:

```
Phase 1
  |
  +-- Parallel Group A: Phase 2 (ingest) | Phase 3 (schedule) | Phase 4 (hardware checklist, when the Pi arrives)
  |
Sync Point A
  |
Phase 5 (player + serve)
  |
Phase 6 (channel switching)
  |
  +-- Parallel Group B: Phase 7 (API) | Phase 8 (web)
  |
Sync Point B
  |
Phase 9 (deploy)  <- also closes out every hardware-gated task from Phases 4, 5, 6
```

Phase 4 has no code dependency on anything. Run it the moment the hardware is on the desk, whatever else is in flight. If it finishes before Phase 5 starts, Phase 5 uses its mpv flags; if not, Phase 5 proceeds on the Mac and the Pi run waits.

---

## Phase 1: Bootstrap, channel config, library index [cmd/channel3/, internal/library/, channels/]

### Objective

A Go module that builds and cross-compiles, a `channel3` binary with three subcommand stubs, a validated YAML channel config loader, the sidecar JSON schema, and a library index that scans sidecars and excludes items whose file is missing or whose status is failed. This is MVP item 1 minus the ingest command, split off so that ingest (Phase 2) and schedule (Phase 3) can proceed in parallel against a settled sidecar contract.

### Input

- `CLAUDE.md` for stack, layout and rules.
- Brainstorm sections "Architecture", "Channel config", "Error handling".
- Toolchain present on the Mac: Go 1.26.2 at `/opt/homebrew/bin/go`. `~/srv/channel3` does not exist yet; this phase creates it for local development.

### Files

Create:

- `go.mod`, `go.sum`
- `Makefile` with targets `build`, `build-arm64`, `test`, `lint`, `run`
- `cmd/channel3/main.go` (subcommand dispatch for `serve`, `ingest`, `guide`; a global `--root` flag defaulting to `/srv/channel3`, overridable by `CHANNEL3_ROOT`; `--version`)
- `cmd/channel3/serve.go`, `cmd/channel3/ingest.go`, `cmd/channel3/guide.go` as stubs that print "not implemented in this phase" and exit 2. Later phases fill their own file and never touch `main.go` again.
- `channels/example.yaml`, copied from the brainstorm with a comment header explaining that real config lives outside the repo
- `internal/library/config.go`, `internal/library/config_test.go`
- `internal/library/sidecar.go`, `internal/library/sidecar_test.go`
- `internal/library/index.go`, `internal/library/index_test.go`
- `internal/library/testdata/` with two channel YAML files and a fake library tree of sidecars, including one with a missing mp4 and one with status failed

Touch:

- `CLAUDE.md`: add a short "Commands" section listing the make targets and the local root `~/srv/channel3`. Keep it to a few lines.

### Tasks

1. [x] `go mod init` and a `Makefile`. `build` produces `bin/channel3` for the host. `build-arm64` produces `bin/channel3-linux-arm64` with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64`. `test` runs `go test -race ./...`. `lint` runs `gofmt -l .` and `go vet ./...` and fails on any output. `run` runs `serve --root ~/srv/channel3 --listen :3333`.
2. [x] `cmd/channel3/main.go` dispatches to the three subcommands with the standard library `flag` package. Unknown subcommand prints usage and exits 2.
3. [x] Config loader in `internal/library`: `LoadChannels(dir string) ([]Channel, error)` reads every `*.yaml` in `<root>/channels/`. `Channel{ID, Number, Name, Sources []string}`. Validate: `id` matches `^[a-z0-9][a-z0-9-]*$`, `number` is 1 to 999 and unique across all files, `name` is non-empty, every source is `http://`, `https://` or `file://`. Zero sources is allowed with a logged warning (the channel will show Stand By). Return all errors with file name and field, not just the first.
4. [x] Sidecar type and round-trip in `internal/library/sidecar.go`, exactly this schema. `file` is relative to the sidecar's directory, or absolute for `file://` sources. `duration` is seconds as a float. Times are RFC 3339 UTC.

```json
{
  "id": "dQw4w9WgXcQ",
  "title": "Steam Engines of the Rockies",
  "source": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
  "file": "dQw4w9WgXcQ.mp4",
  "duration": 612.437,
  "ingested_at": "2026-09-23T02:11:00Z",
  "status": "ok"
}
```

A failed item keeps `id`, `source`, `status: "failed"`, `error` (first line of the tool's stderr), and `attempted_at`. No `file`, no `duration`.

5. [x] Library index in `internal/library/index.go`: `Scan(root string, channels []Channel) (Index, error)` walks `<root>/library/<channel-id>/*.json`, parses sidecars, and returns per-channel items in a stable order (by id). Items are excluded, with a logged reason, when status is not `ok`, when the file does not exist, or when duration is not positive. A channel directory that does not exist yields zero items, not an error. `Item{ID, Title, Path (absolute), Duration time.Duration, Source}`.
6. [x] Create `~/srv/channel3/{channels,library,local}` on the Mac if absent (a `make dev-root` target is fine). Do not put anything in it from the repo.
7. [x] Update `CLAUDE.md` Commands section.

### Tests

- `config_test.go`: valid two-file config loads; duplicate number across files is rejected; bad id, bad number, unknown scheme, missing name each produce an error naming the file and field; zero sources loads with a warning.
- `sidecar_test.go`: ok and failed sidecars round-trip through JSON byte-for-byte on the fields above; unknown fields are ignored on read.
- `index_test.go`: against `testdata`, the ok item is present with an absolute path and correct duration, the missing-file item and the failed item are excluded, a channel with no directory yields zero items, order is stable across two scans.

### Output

- A repo that builds for the Mac and cross-compiles for arm64.
- The sidecar contract that Phase 2 writes and Phase 5 reads.
- The `library.Item` and `library.Channel` types that Phase 3's CLI and Phase 5's serve adapt into schedule inputs.

### Branch

`feature/channel-three-mvp-phase-1`

### Verification checklist

- [ ] `make lint` and `make test` pass with no output from gofmt.
- [ ] `make build-arm64` produces a binary; `file bin/channel3-linux-arm64` reports ARM aarch64, statically linked.
- [ ] `bin/channel3` with no args prints usage and exits 2; `bin/channel3 serve` prints "not implemented" and exits 2.
- [ ] `channels/example.yaml` is the only YAML under `channels/` and `git status` stays clean after `make dev-root`.
- [ ] Sidecar schema in code matches the JSON block above field for field.
- [ ] No network calls anywhere in `internal/library` yet (grep for `net/http`, `exec.Command`).

---

## Parallel Group A (Phases 2, 3, 4)

Three phases with no shared files. Phase 2 owns `internal/library/ingest*.go`, `ytdlp.go`, `probe.go` and `cmd/channel3/ingest.go`. Phase 3 owns `internal/schedule/` and `cmd/channel3/guide.go` plus `cmd/channel3/load.go`. Phase 4 owns `docs/hardware.md`.

## Phase 2: `channel3 ingest` [internal/library/, cmd/channel3/]

### Objective

`channel3 ingest --root <root>` reads every channel config, expands playlists, downloads each video once with yt-dlp preferring H.264 at or below 1080p merged to mp4, probes the real duration, writes a sidecar, records failures as failed sidecars, retries them on the next run, and never runs while `serve` is playing on the same machine. This is the second half of MVP item 1.

### Input

- Phase 1's `library.Channel`, sidecar schema and layout `<root>/library/<channel-id>/<id>.mp4` plus `<id>.json`.
- Brainstorm "library" component and "Ingest failure" error handling.
- Tools on the Mac: `yt-dlp` 2026.08.19 and `ffmpeg`/`ffprobe` at `/opt/homebrew/bin/`.

### Files

Create:

- `internal/library/ingest.go`, `internal/library/ingest_test.go`
- `internal/library/ytdlp.go` (a `Runner` interface with `Expand(url) ([]Entry, error)` and `Download(url, destDir) (Result, error)`, and the real implementation that shells out)
- `internal/library/probe.go` (ffprobe duration; `Prober` interface for tests)
- `internal/library/testdata/ytdlp/` fixture JSON: one single-video info JSON, one flat playlist JSON with three entries, one failing stderr sample
- `internal/library/testdata/tiny.mp4`, a one-second file generated once with ffmpeg and committed (a few KB)
- `internal/library/ingest_network_test.go`, skipped unless `CHANNEL3_NETWORK_TESTS=1`

Touch:

- `cmd/channel3/ingest.go`: flags `--channel <id>` (filter), `--dry-run` (list planned work, no downloads), `--yt-dlp <path>`, `--ffprobe <path>`.

### Tasks

1. [x] Playlist expansion. A source URL is expanded with `yt-dlp --flat-playlist --dump-single-json`. A single video yields one entry; a playlist yields its entries in playlist order. Each entry has `id`, `url`, `title`. Unsupported or unreachable URLs produce a failed sidecar keyed by a stable hash of the URL so the failure is visible in the library and retried next run.
2. [x] Download. For each entry whose sidecar is absent or failed: run yt-dlp with a format selector that prefers `avc1` video at height 1080 or lower plus best audio, merged to mp4, output `<channel-dir>/<id>.<ext>`. A reasonable starting point is `-f "bv*[vcodec^=avc1][height<=1080]+ba/b[height<=1080]" --merge-output-format mp4 --no-playlist --no-overwrites`. Pass `--print-json` or read the resulting `.info.json` so the title comes from yt-dlp.
3. [x] Probe. After download, `ffprobe -v error -show_entries format=duration -of json` on the final file gives the duration. Fall back to yt-dlp's `duration` only if ffprobe fails, and log that. Exact durations matter because the schedule's boundaries are cumulative.
4. [x] `file://` sources. Stat the path, probe duration, write a sidecar whose `file` is the absolute path. Do not copy or symlink. The `id` is the slugified basename without extension; two local sources in one channel that slugify to the same id is a config error reported before any downloads start.
5. [x] Skip logic. An entry with an existing `ok` sidecar whose file exists is skipped without touching the network. A failed sidecar is retried. A sidecar marked `ok` whose file is missing is re-downloaded.
6. [x] Failure sidecars. Any yt-dlp or ffprobe error writes a failed sidecar with the first line of stderr, then ingest continues with the next entry. Ingest never aborts a run because one video failed.
7. [x] Summary and exit codes. Print one line per entry (`ok`, `skip`, `failed <reason>`) and a final count. Exit 0 if nothing failed, 2 if any item failed, 1 on config or usage errors.
8. [x] Playback guard. If `<root>/serve.pid` exists and that process is alive, refuse to run and say so. Phase 5's `serve` writes this file. Until Phase 5 exists the check simply finds no file.
9. [x] `--dry-run` performs expansion but no downloads, and prints the plan.

### Tests

- `ingest_test.go` with a fake `Runner` and fake `Prober`: single video produces an ok sidecar with title, duration from the prober, and a file that exists; a playlist of three produces three; a failing entry produces a failed sidecar and the run continues; an existing ok sidecar is skipped and the fake runner records no call; a failed sidecar is retried; `file://` writes an absolute `file` and no copy; duplicate local slugs are rejected before any runner call; exit codes as specified.
- `ingest_network_test.go`: one real short public-domain YouTube video into a temp root, asserting an ok sidecar and a playable mp4. Skipped by default.

### Output

- `channel3 ingest` usable on the Mac against `~/srv/channel3` today.
- Failed items visible in the library as sidecars, never as crashes.

### Branch

`feature/channel-three-mvp-phase-2`

### Verification checklist

- [ ] `make lint` and `make test` pass.
- [ ] Put a real config with one short YouTube video and one `file://` source in `~/srv/channel3/channels/`, run `bin/channel3 ingest --root ~/srv/channel3 --dry-run`, then without `--dry-run`. Both items get ok sidecars; the mp4 plays in QuickTime or mpv; `duration` matches `ffprobe` to the millisecond.
- [ ] Run ingest again. Every entry says `skip`, nothing is downloaded or probed, and media mtimes are unchanged. Source expansion still calls yt-dlp on every run so it notices videos added to a playlist; `--yt-dlp /usr/bin/false` therefore fails at expansion, which is expected, not a skip.
- [ ] Add a bogus URL to the config and run again. Exit code is 2, a failed sidecar exists with a readable error, the ok items are untouched.
- [ ] `grep -r "net/http" internal/ cmd/` shows nothing. Ingest talks to the network only through yt-dlp.
- [ ] `CHANNEL3_NETWORK_TESTS=1 go test ./internal/library/ -run Network` passes on the Mac.

---

## Phase 3: Schedule package and `channel3 guide` [internal/schedule/, cmd/channel3/]

### Objective

A pure `internal/schedule` package where `At(channel, t)` returns the item and offset for any instant, `Guide(channel, from, horizon)` returns the slots ahead, the broadcast day starts at 04:00 local, the order per day is a seeded shuffle, and a golden test pins the algorithm. Plus `channel3 guide`, a text command that prints the schedule for the real library so the package can be exercised before any video plays. This is MVP item 2 with the CLI added because it is twenty minutes of work and it is the natural manual test harness.

### Input

- Brainstorm "schedule" component and "Testing" section.
- Phase 1's `library.Item` for the CLI adapter only. The schedule package itself imports nothing from `library`; it defines its own input types so it stays pure and table-testable.

### Files

Create:

- `internal/schedule/schedule.go` (types, `At`)
- `internal/schedule/day.go` (`BroadcastDay`, `DayStart`)
- `internal/schedule/order.go` (seed, shuffle)
- `internal/schedule/guide.go` (`Guide`)
- `internal/schedule/schedule_test.go`, `day_test.go`, `order_test.go`, `guide_test.go`
- `internal/schedule/testdata/golden_order.json` (fixed channel, fixed day, expected order)
- `cmd/channel3/load.go` (`loadStation(root) ([]schedule.Channel, error)`: config plus index into schedule inputs; shared by `guide` now and `serve` in Phase 5)

Touch:

- `cmd/channel3/guide.go`: flags `--hours N` (default 6), `--at RFC3339` (default now), `--channel <id>`.

### Tasks

1. [x] Types and signatures. Keep to this shape so Phases 5 and 7 can be written against it.

```go
package schedule

type Item struct {
    ID       string
    Title    string
    Path     string
    Duration time.Duration
}

type Channel struct {
    ID     string
    Number int
    Name   string
    Items  []Item
}

type Slot struct {
    Item   Item
    Start  time.Time     // wall clock start of this airing
    End    time.Time     // Start + Item.Duration
    Offset time.Duration // how far into Item at the query time; zero for future slots
}

type Clock struct {
    Location *time.Location // channel's local zone; time.Local on the Pi
    DayStart time.Duration  // 4 * time.Hour
}

func (c Clock) BroadcastDay(t time.Time) time.Time // 04:00 local on the day t belongs to
func Order(ch Channel, day time.Time) []Item        // seeded shuffle; empty in gives empty out
func At(ch Channel, t time.Time, c Clock) (Slot, bool)
func Guide(ch Channel, from time.Time, horizon time.Duration, c Clock) []Slot
```

2. [x] `BroadcastDay`: a time before 04:00 local belongs to the previous calendar day's broadcast day. Build with `time.Date(y, m, d, 4, 0, 0, 0, loc)` so DST days of 23 or 25 hours are handled by the standard library, not by arithmetic on 24 hours.
3. [x] `Order`: seed is FNV-1a 64 of `channelID + "|" + day.Format("2006-01-02")`. Shuffle with a Fisher-Yates over a PCG generator from `math/rand/v2` seeded from that hash so the algorithm is specified and stable across Go versions. Items with zero or negative duration are dropped before shuffling.
4. [x] `At`: `T` is the sum of durations of the day's order. If `T` is zero return `false`. `elapsed = t - BroadcastDay(t)`, `pos = elapsed mod T`, walk cumulative durations to find the item and `offset`. Exactly on a boundary the new item starts at offset zero. `Start` and `End` are wall-clock times of this airing.
5. [x] `Guide`: starting from `At(from)`, append following slots by walking the order, wrapping at the end of the order, and recomputing the order when a slot crosses a broadcast-day boundary, until `horizon` is covered. The first slot carries a non-zero `Offset`; later slots carry zero.
6. [x] `channel3 guide` prints, per channel sorted by number, `NN  Name` then one line per slot as `HH:MM  Title  (remaining Xm for the first slot)` in local time. `--at` lets you look at any instant.
7. [x] `cmd/channel3/load.go` adapts `library.Channel` plus `library.Index` into `[]schedule.Channel` sorted by number. `serve` reuses this in Phase 5.

### Tests

Table-driven throughout. Required cases:

- Same inputs give the same `(item, offset)` across repeated calls and across process runs (golden file).
- Boundaries: one nanosecond before a cumulative duration returns the earlier item at `Duration - 1ns`; exactly on it returns the next item at zero.
- Wrap: `elapsed` many multiples of `T` still lands correctly.
- Day rollover: 03:59:59 and 04:00:00 local on the same calendar day produce different orders; 04:00:00 is offset zero of the new order's first item.
- DST: the spring-forward and fall-back days in `America/Toronto` produce a 23 h and 25 h broadcast day and `At` never panics or returns a negative offset across the transition.
- Removed item: removing one item from `Items` changes the order but every remaining item still appears exactly once and `T` shrinks by that duration.
- Empty channel returns `false`; single-item channel loops on itself.
- `Guide` covers at least `horizon`, slots are contiguous (`End` of one equals `Start` of the next), and a guide that spans 04:00 switches order at exactly 04:00.
- Golden: `testdata/golden_order.json` holds a channel of eight items and the expected order for 2026-09-22. If the algorithm changes, this test fails and the change must be deliberate.

### Output

- The pure scheduling core every later phase calls.
- `channel3 guide` as the first end-to-end command against a real library.

### Branch

`feature/channel-three-mvp-phase-3`

### Verification checklist

- [ ] `go test -race -count=3 ./internal/schedule/` passes (the count catches any hidden nondeterminism).
- [ ] `go list -deps ./internal/schedule/ | grep -v '^[a-z]*$'` shows only standard library packages.
- [ ] `bin/channel3 guide --root ~/srv/channel3 --hours 3` prints a plausible guide against the Phase 2 library; running it twice within a minute gives identical titles and times.
- [ ] `bin/channel3 guide --at 2026-09-23T03:59:00-04:00` and `--at 2026-09-23T04:00:00-04:00` show different orders.
- [ ] The golden test exists and its expected order was generated by the implementation once, then frozen.

---

## Phase 4: Pi hardware checklist [docs/] (hardware-gated)

### Objective

Prove on the real Pi 5, before any Pi-dependent code is written, that mpv can drive the TV over DRM/KMS with no desktop, that 1080p H.264 plays smoothly with audio over HDMI, that the Flirc appears as an evdev keyboard with known key codes, that `cec-ctl` can power and adjust the TV or that it cannot, and that unplugging or powering off the TV does not kill a running mpv. Record everything in `docs/hardware.md`. This is the brainstorm's top risk and its own phase so it can start the day the hardware arrives.

### Input

- Brainstorm "Risks" and "Testing: Hardware checklist".
- Hardware from Smitty task #251: Pi 5 4 GB, cooler, supply, NVMe HAT or USB SSD, micro-HDMI cable, Flirc USB, remote. TV CEC check is Smitty task #252.
- Raspberry Pi OS Lite (Trixie, Debian 13) ships mpv 0.40. Homebrew on the Mac also ships 0.40. Forum reports on Trixie show mpv's default Vulkan path failing at 1080p and `--gpu-api=opengl` fixing it. Pi 5 has no H.264 hardware decoder; H.264 is software decoded and HEVC is the only hardware codec.

### Files

Create:

- `docs/hardware.md` with sections: Pi and OS versions, mpv version, working mpv command line, playback results, audio, TV power cycle behaviour, Flirc key table, CEC results, boot time, open issues.

### Tasks

1. [ ] Flash Raspberry Pi OS Lite 64-bit, enable ssh, set hostname `channel3`, set the local time zone, `apt install mpv v4l-utils ffmpeg evtest`. Record `mpv --version` and `uname -a`.
2. [ ] Copy one 1080p H.264 mp4 from the Phase 2 library to the Pi. Try, in order, until one plays full screen with no desktop running: `mpv --vo=gpu --gpu-context=drm --gpu-api=opengl --hwdec=no`; `mpv --vo=drm`; `mpv --vo=gpu-next --gpu-context=drm`. Record the winning flags, CPU use from `top` during playback, and whether frames drop (`--stats` or `drop-frame-count` over IPC).
3. [ ] Audio. Confirm sound over HDMI with no desktop. Record the working `--audio-device` if the default is wrong.
4. [ ] Flags for the service. Confirm `--fullscreen --no-osc --no-osd-bar --no-input-default-bindings --idle=yes --hr-seek=yes --image-display-duration=inf --input-ipc-server=/tmp/mpv.sock` works with the winning output flags, that `echo '{"command":["get_property","mpv-version"]}' | socat - /tmp/mpv.sock` answers, and that a `loadfile` with `start=` lands within a second.
5. [ ] TV power cycle. With mpv playing, power the TV off and on from its own remote, and separately unplug and replug HDMI. Record whether mpv keeps running, exits, or freezes. This decides how aggressive the Phase 5 supervisor's health check must be.
6. [ ] Flirc. Pair the remote's buttons on the Mac with the Flirc app or `flirc_util record`, aiming for channel up, channel down, digits 0 to 9, power, volume up, volume down, mute. On the Pi run `evtest` on the Flirc device and record the device name and the `KEY_*` code each button emits. Note which buttons could not be mapped.
7. [ ] CEC. `cec-ctl -d /dev/cec0 --playback -S` to register, then `--to 0 --standby`, `--to 0 --image-view-on`, and volume via `--user-control-pressed ui-cmd=volume-up` followed by `--user-control-released`. Record what the TV honours. If the TV's menu has no CEC option or nothing responds, record "no CEC" and stop; the universal-remote fallback is already decided.
8. [ ] Boot time. `systemd-analyze` after a clean boot, plus a stopwatch from power-on to login prompt. Phase 9 targets video within 30 s of power.
9. [ ] Console. Note whether the kernel console draws on the HDMI output while mpv runs (cursor blink, boot text). Phase 9 quiets it.

### Tests

None in code. The deliverable is `docs/hardware.md` with every section filled and a clear go or no-go on DRM/KMS.

### Output

- `docs/hardware.md`.
- The mpv flag set Phase 5 passes via `--mpv-arg` on the Pi and Phase 9 bakes into the unit.
- The Flirc key table Phase 6 maps.
- A CEC yes or no that decides whether Phase 6's CEC task is done or skipped.

### Branch

`feature/channel-three-mvp-phase-4`

### Verification checklist

- [ ] `docs/hardware.md` exists and every section has content or an explicit "not tested because".
- [ ] The recorded mpv command line was run on the Pi and played a file full screen with sound, confirmed by a second person or a phone video.
- [ ] Flirc table lists at least channel up, channel down and digits 0 to 9 with `KEY_*` codes.
- [ ] CEC section says yes with the commands that worked, or no.
- [ ] If DRM/KMS failed on every attempt, the doc says so and this plan gets a note under Risks before Phase 5 starts.

---

## Sync Point A

Before Phase 5 starts: Phases 1, 2 and 3 are merged, so `library` produces items with exact durations and `schedule` answers `At`. If Phase 4 is done, its mpv flags and TV power-cycle behaviour are inputs to Phase 5; if not, Phase 5 develops against Homebrew mpv on the Mac and leaves its Pi task open.

---

## Phase 5: Player supervisor and `channel3 serve` [internal/player/, cmd/channel3/]

### Objective

`channel3 serve` boots, shows the "Please Stand By" card within a second, computes what the lowest-numbered channel is airing right now, and plays it from that offset. At every `end-file` it asks the schedule again rather than chaining "next". If mpv dies it is restarted and re-seeked. A file mpv cannot open is dropped from today's order. A clock jump is corrected at the next tick. This is MVP item 3 with the Stand By card and the restart behaviour from the brainstorm's Error handling section included, not deferred.

### Input

- Brainstorm "player" component, "Error handling", "Testing: internal/player".
- Phase 3 signatures and `cmd/channel3/load.go`.
- `docs/hardware.md` if Phase 4 is done.
- `brew install mpv` on the Mac first; mpv is not installed there today.
- mpv fact that shapes the IPC code: since mpv 0.38 `loadfile` takes `<url> [<flags> [<index> [<options>]]]`, and the index must be `-1` when options are passed. Both the Mac (Homebrew) and the Pi (Trixie apt) ship 0.40, so this plan targets 0.38 or newer and uses the four-argument form. The supervisor reads `mpv-version` on connect and refuses to start with a clear error on anything older.

### Files

Create:

- `internal/player/ipc.go` (line-delimited JSON client over a Unix socket; request ids; a reader goroutine that routes replies and events; command timeout)
- `internal/player/mpv.go` (`Launcher` interface; real implementation starts mpv with the base flags plus `--mpv-arg` extras; socket path in a runtime dir)
- `internal/player/player.go` (`Player` interface and the supervisor: connect with retry, `Load(path, offset)`, `Standby()`, `Position() (path, time-pos)`, `Events() <-chan Event`, restart with backoff)
- `internal/player/standby.go` and `internal/player/assets/standby.png` (1920x1080, dark background, "Please Stand By" text, generated by an ImageMagick or Go command recorded in the Makefile as `standby-card`; embedded with `go:embed`; written to the runtime dir at startup because mpv needs a path)
- `internal/player/ipc_test.go`, `player_test.go`, and `fakempv_test.go` (a fake mpv: Unix socket server that records every command, replies with `request_id` and `"error":"success"`, and can emit events and close the connection on demand)
- `cmd/channel3/station.go` (the broadcast loop: current channel, schedule lookup, exclusions, ticks, rollover rescan; depends on `Player` and a `now func() time.Time` so it is testable) and `cmd/channel3/station_test.go`

Touch:

- `cmd/channel3/serve.go`: flags `--mpv <path>` (default `mpv`), `--mpv-arg` (repeatable), `--start-channel <id>`, `--standby <png>` override. Writes `<root>/serve.pid` on start and removes it on clean exit.

### Tasks

1. [x] IPC client. One connection, newline-delimited JSON, monotonically increasing `request_id`, a reader goroutine that matches replies to waiting callers and pushes events to a channel. `Command(ctx, args ...any) (json.RawMessage, error)`. Errors from mpv (`"error"` not `"success"`) become Go errors with the mpv text.
2. [x] Launcher. Base flags: `--fullscreen --no-osc --no-osd-bar --osd-level=0 --no-input-default-bindings --input-vo-keyboard=no --no-terminal --idle=yes --keep-open=no --hr-seek=yes --image-display-duration=inf --input-ipc-server=<sock>`. Extras from `--mpv-arg` are appended so Phase 9 can add the Pi's DRM flags and a Mac developer can add `--no-fullscreen --geometry=960x540`. Kill on context cancel.
3. [x] Load. `Load(path, offset)` sends `["loadfile", path, "replace", -1, "start=<offset seconds with 3 decimals>"]`. On connect, `get_property mpv-version`; parse the major and minor; error out below 0.38 with a message naming the installed version and the requirement.
4. [x] Events. Translate mpv `end-file` into `Event{Kind: EndFile, Reason, Path}`. Only `reason: eof` and `reason: error` reach the station. `reason: stop` and `reason: redirect` are emitted by mpv for the file being replaced by our own `loadfile` and must be swallowed inside the player, otherwise the station loops.
5. [x] Standby. `Standby()` loads the embedded PNG with `replace`. Because `--image-display-duration=inf` is global, the card stays up until the next `Load`.
6. [x] Supervision. If the mpv process exits or the socket read fails, close everything, back off (500 ms, 1 s, 2 s, cap 5 s, reset after 60 s of health), relaunch, reconnect, and emit `Event{Kind: Restarted}` so the station re-seeks. Health check every 10 s: `get_property idle-active` must answer within 2 s or the process is treated as hung and restarted. Phase 4's TV power-cycle result decides whether that timeout is tightened.
7. [x] Station loop in `cmd/channel3/station.go`. State held in memory only: channels, current channel id, per-channel exclusion set of item ids that failed today, current broadcast day. Behaviour: on start show Standby, load channels, tune to `--start-channel` or the lowest number, `play()`. `play()` calls `schedule.At` with the exclusion set applied; on `false` call `Standby()`, otherwise `Load`. On `EndFile eof` call `play()`. On `EndFile error` add the item to the exclusion set, log it, call `play()`. On `Restarted` call `play()`. Every 30 s reconcile: ask `Position()`, compare with `At(now)`; if the path differs or the offsets differ by more than 5 s, call `play()`. This is what corrects an NTP jump and a sidecar duration that disagrees with the file. When the broadcast day changes, rescan the library, rebuild channels, clear exclusions, then `play()`.
8. [x] `serve` wiring. Parse flags, build the launcher and player, run the station until SIGINT or SIGTERM, then quit mpv cleanly and remove `serve.pid`. Log one line per load: channel, item id, offset, and the `now` it was computed from.
9. [ ] **(hardware-gated)** Run `serve` on the Pi with `--mpv-arg` set from `docs/hardware.md`. Confirm boot-to-video, a boundary transition, and a `pkill mpv` recovery. Record any flag changes in `docs/hardware.md`.

   Not run: there is no Pi yet and Phase 4 has not happened, so Phase 5 was built and verified against Homebrew mpv 0.41.0 on the Mac.

### Tests

- `ipc_test.go` against the fake mpv: request ids correlate replies under concurrent commands; an mpv error string becomes a Go error; events are delivered in order; a command times out when the fake stops replying.
- `player_test.go` against the fake mpv, with the launcher stubbed so no real mpv runs: `Load` sends exactly `["loadfile", "/lib/a.mp4", "replace", -1, "start=83.500"]`; `Standby` sends a `loadfile` of a PNG path; `end-file` with reason `stop` is not surfaced while `eof` and `error` are; closing the fake's connection triggers a relaunch and a `Restarted` event; a version below 0.38 fails startup.
- `station_test.go` with a fake `Player` and a fake clock: startup shows Standby then loads the schedule's answer with the schedule's offset; an `eof` event causes a fresh `At` call at the fake clock's current time, not "next in list"; an `error` event excludes that item and the next load is a different item; when every item is excluded the station shows Standby; advancing the fake clock by three hours between ticks causes a reload at the correct new offset; crossing 04:00 triggers a rescan and a reload; a channel with zero items shows Standby.

### Output

- A binary that plays one channel from boot on the Mac, and on the Pi once the hardware exists.
- The `Player` interface Phase 6 drives on channel change and Phase 7 reads for the tuned channel.

### Branch

`feature/channel-three-mvp-phase-5`

### Verification checklist

- [ ] `make lint`, `make test`, `make build-arm64` pass. No test spawns a real mpv.
- [ ] On the Mac with `brew install mpv` done: generate two 20 s test clips with `ffmpeg -f lavfi -i testsrc=duration=20:size=1280x720:rate=30 -f lavfi -i sine=frequency=440:duration=20 -c:v libx264 -pix_fmt yuv420p -c:a aac -shortest ~/srv/channel3/local/a.mp4` (and `b.mp4` with a different frequency), point a channel at them with `file://` sources, run `bin/channel3 ingest`, then `bin/channel3 serve --root ~/srv/channel3 --mpv-arg=--no-fullscreen --mpv-arg=--geometry=960x540`.
- [ ] The Stand By card appears first, then a clip starts mid-way, matching `bin/channel3 guide` for the same minute.
- [ ] Clips alternate at their boundaries with no gap longer than about a second and no repeated clip.
- [ ] `pkill mpv` while playing: a new mpv window appears within five seconds at the schedule's current offset. The log shows `Restarted` then a load.
- [ ] Replace `b.mp4` with an empty file and restart: `b` is excluded after one error event and `a` loops alone; the log names the excluded item.
- [ ] A channel with no items shows the Stand By card and the process stays up.
- [ ] `~/srv/channel3/serve.pid` exists while running and is gone after Ctrl-C; `bin/channel3 ingest` refuses to run while it exists.
- [ ] `grep -rn "os.WriteFile\|os.Create" cmd/ internal/player/` shows only the pid file and the standby PNG extraction. Nothing about playback is written to disk.
- [ ] (hardware-gated) Task 9 done on the Pi, or marked WARN with the reason.

---

## Phase 6: Channel switching: tuner, keyboard, Flirc, CEC [internal/input/, cmd/channel3/]

### Objective

Channel up, channel down and digit entry work from any evdev keyboard on the Pi, including the Flirc, and from the terminal on the Mac for development. Tuning logic is a pure, table-tested state machine. Power and volume keys are forwarded to `cec-ctl` when `--cec` is on. This is MVP item 4.

### Input

- Brainstorm "input" component and "Data flow on channel change".
- Phase 5's station loop and `Player`.
- `docs/hardware.md` Flirc key table and CEC result, when available.
- Flirc presents as a USB keyboard named like `flirc.tv flirc Keyboard`; a plain USB keyboard is the same code path, which is why "keyboard, then Flirc" is one implementation with one gated verification step.

### Files

Create:

- `internal/input/keys.go` (`Key` with `Action` in {ChannelUp, ChannelDown, Digit, Power, VolumeUp, VolumeDown, Mute} and `Digit int`; a `Source` interface `Keys(ctx) <-chan Key`)
- `internal/input/tuner.go`, `internal/input/tuner_test.go`
- `internal/input/evdev.go` (24-byte `input_event` parsing for 64-bit Linux, `KEY_*` constants, code-to-Action map) and `internal/input/evdev_test.go`
- `internal/input/evdev_linux.go` (open `/dev/input/eventN`, `EVIOCGRAB`, read loop; build tag `linux`) and `internal/input/evdev_other.go` (returns "not supported" off Linux)
- `internal/input/tty.go` (raw-mode stdin source using `golang.org/x/term`: `+` or up arrow for channel up, `-` or down arrow for channel down, digits, `p` for power, `[`/`]` for volume; only when stdin is a terminal)
- `internal/input/cec.go` (`CEC` interface with `Power()`, `VolumeUp()`, `VolumeDown()`, `Mute()`; real implementation shells out to `cec-ctl`; a fake for tests) and `internal/input/cec_test.go`

Touch:

- `cmd/channel3/station.go`: accept a merged key stream; on a committed channel change call `play()` for the new channel; expose `Tuned() string` for Phase 7.
- `cmd/channel3/serve.go`: flags `--input-device <path>` (empty means auto-detect: prefer a device whose name contains "flirc", otherwise the first device advertising `KEY_CHANNELUP` or `KEY_0`), `--no-input`, `--cec` (default off), `--cec-device /dev/cec0`.
- `docs/hardware.md`: Flirc table confirmed against the running service (hardware-gated).

### Tasks

1. [x] Tuner state machine in `internal/input/tuner.go`. Constructed with the channel list sorted by number and a current channel. `Handle(key Key, now time.Time) (changed bool, target Channel)`. Up and down move through channels by number and wrap. Digits accumulate into a pending number; the pending number commits when 1.5 s pass with no further digit, or immediately when no channel number has the pending digits as a prefix beyond the exact match (with channels 3, 5 and 12: pressing 3 commits at once; pressing 1 waits for a 2 or the timeout). A committed number that matches no channel is discarded and the current channel stays. Up or down while digits are pending clears them. Expose `Pending() (digits string, deadline time.Time)` so the station can arm a timer; the tuner itself has no goroutines.
2. [x] evdev parsing. Decode `input_event` structs from a byte stream: 16 bytes of timeval, `type` uint16, `code` uint16, `value` int32. Act on `EV_KEY` with `value == 1` (press) only; ignore release and autorepeat. Map `KEY_CHANNELUP`, `KEY_UP`, `KEY_PAGEUP` to ChannelUp; `KEY_CHANNELDOWN`, `KEY_DOWN`, `KEY_PAGEDOWN` to ChannelDown; `KEY_0` to `KEY_9` and `KEY_KP0` to `KEY_KP9` to Digit; `KEY_POWER` to Power; `KEY_VOLUMEUP`, `KEY_VOLUMEDOWN`, `KEY_MUTE` to their actions. Everything else is dropped.
3. [x] evdev device on Linux. Open the device, grab it with `EVIOCGRAB` so keypresses do not also reach the console, read in a loop, reopen with backoff if the device disappears (Flirc unplugged) and log it. Device auto-detect scans `/dev/input/event*` names via `EVIOCGNAME` or `/sys/class/input/event*/device/name`. Pure Go only; the arm64 static build must still succeed.
4. [x] TTY source for the Mac. Enabled automatically when stdin is a terminal and `--no-input` is not set. Restores the terminal on exit.
5. [x] Station integration. Merge all sources into one channel. On a committed change: look up `schedule.At(newChannel, now)`, `Load` or `Standby`, log `tune <number> <id>`. Pending-digit timeout is a timer inside the station loop driven by the tuner's `Pending()`.
6. [x] CEC. `cec.Real` runs `cec-ctl -d <dev> --to 0 --standby` or `--image-view-on` for Power (toggle based on the last state we sent, since the TV's real state is unknown), and `--user-control-pressed ui-cmd=volume-up` then `--user-control-released` for volume, per what `docs/hardware.md` says worked. Each call has a 3 s timeout and errors are logged, never fatal. Behind `--cec`. When off, Power and Volume keys are logged and dropped.
7. [ ] **(hardware-gated)** On the Pi with the Flirc plugged in: confirm auto-detect picks the Flirc, channel up and down change channels within a second, digit entry works for a one-digit and a two-digit channel, and the console does not echo keypresses. Update `docs/hardware.md`.

   Not run: there is no Pi and no Flirc yet, so the evdev path was built and cross-compiled for arm64 but has never read a real device.
8. [ ] **(hardware-gated, only if Phase 4 recorded CEC yes)** Confirm power and volume from the remote reach the TV with `--cec`. If Phase 4 recorded no CEC, mark this task skipped and leave `--cec` off in Phase 9's unit.

   Not run: Phase 4 has not happened, so there is no television and no CEC result yet. `--cec` stays off by default.

### Tests

- `tuner_test.go`, table-driven: up and down wrap in both directions; single-digit immediate commit when no longer number could match; two-digit wait then commit on the second digit; two-digit wait then commit on timeout; timeout with a number that matches nothing leaves the channel unchanged; up during pending digits clears them and moves; digits on an empty channel list do nothing; `Pending()` deadline equals last digit time plus 1.5 s.
- `evdev_test.go`: a fixture byte slice containing a press, a release and an autorepeat of `KEY_CHANNELUP` yields exactly one ChannelUp; a partial trailing struct is buffered, not misparsed; unknown key codes are dropped; the 24-byte layout is asserted against a hand-built record.
- `cec_test.go` with a fake command runner: Power alternates standby and image-view-on; VolumeUp sends pressed then released; a failing command is logged and returns an error that the caller ignores.
- `station_test.go` additions: a ChannelUp key causes exactly one `Load` for the next channel at the schedule's offset for that channel; digits followed by the fake clock advancing past the timeout tune to the typed channel.

### Output

- Channel changing on the Mac from the terminal and on the Pi from any keyboard or the Flirc.
- `Tuned()` for Phase 7.

### Branch

`feature/channel-three-mvp-phase-6`

### Verification checklist

- [ ] `make lint`, `make test`, `make build-arm64` pass. `GOOS=linux go vet ./internal/input/` passes so the Linux-only file is checked on the Mac.
- [ ] On the Mac, with two channels of test clips from Phase 5: run `serve` in a terminal, press `+` and `-` and see mpv switch clips within a second at the offset `bin/channel3 guide` predicts; type the channel number and see it commit.
- [ ] Typing a number that matches no channel leaves playback untouched and logs it.
- [ ] `--no-input` runs without touching the terminal.
- [ ] `grep -rn "Persist\|WriteFile\|json.Marshal" internal/input/` finds nothing that writes state.
- [ ] (hardware-gated) Tasks 7 and 8 done on the Pi, or WARN with reason. Task 8 may be legitimately skipped if Phase 4 found no CEC.

---

## Parallel Group B (Phases 7, 8)

Phase 7 owns `internal/api/`, `cmd/channel3/serve.go`, `.gitignore`, and exactly two files under `web/`: `web/embed.go` and `web/dist/.gitkeep`. Phase 8 owns everything else under `web/`, plus `Makefile` and `CLAUDE.md` additions. Phase 8 builds against the API contract below and against fixture JSON; its end-to-end verification needs Phase 7 merged, which is Sync Point B.

### API contract shared by Phases 7 and 8

All responses are `application/json`. Times are RFC 3339 with the Pi's local offset. Durations and offsets are seconds as floats. No auth. Errors are `{"error": "<message>"}` with a 4xx or 5xx status.

`GET /api/channels`

```json
{
  "channels": [
    {"id": "trains", "number": 3, "name": "Train TV", "items": 42}
  ]
}
```

`GET /api/now`

```json
{
  "time": "2026-09-22T19:04:11-04:00",
  "tuned": "trains",
  "channels": [
    {
      "id": "trains", "number": 3, "name": "Train TV",
      "now":  {"id": "abc", "title": "Steam Engines", "start": "2026-09-22T18:58:00-04:00", "end": "2026-09-22T19:08:12-04:00", "duration": 612.437, "offset": 371.2},
      "next": {"id": "def", "title": "Switching Yard", "start": "2026-09-22T19:08:12-04:00", "end": "2026-09-22T19:31:00-04:00", "duration": 1368.0, "offset": 0}
    }
  ]
}
```

A channel with no items has `"now": null` and `"next": null`. `tuned` is `null` when nothing is tuned (should not happen after boot, but the shape allows it).

`GET /api/guide?hours=6`

```json
{
  "from": "2026-09-22T19:04:11-04:00",
  "to":   "2026-09-23T01:04:11-04:00",
  "channels": [
    {
      "id": "trains", "number": 3, "name": "Train TV",
      "slots": [
        {"id": "abc", "title": "Steam Engines", "start": "2026-09-22T18:58:00-04:00", "end": "2026-09-22T19:08:12-04:00", "duration": 612.437}
      ]
    }
  ]
}
```

`hours` is an optional integer, default 6, minimum 1, maximum 48. Anything else is a 400. The first slot may start before `from`. Channels are sorted by number in every response.

`GET /` and static asset paths serve the embedded web build. When the build is absent the server returns a plain HTML page saying the UI has not been built, with a 200, so the API is still reachable.

## Phase 7: HTTP API and embedded UI plumbing [internal/api/, web/embed.go, cmd/channel3/]

### Objective

The three read-only endpoints above, served by `channel3 serve` on `--listen` (default `:3333`), plus static serving of an embedded `web/dist` with a fallback page when nothing has been built. This is the backend half of MVP item 5, split from the page because the two live in different toolchains and the `phase-breakdown` skill wants the API contract to precede both.

### Input

- The API contract above.
- Phase 3 `schedule.Guide` and `schedule.At`; Phase 6 `Tuned()`.
- `execute` skill step 5: this phase writes the integration summary.

### Files

Create:

- `internal/api/server.go` (`New(deps Deps) http.Handler`; `Deps{Channels func() []schedule.Channel, Now func() time.Time, Tuned func() string, Clock schedule.Clock, UI fs.FS}`)
- `internal/api/handlers.go`, `internal/api/handlers_test.go`
- `internal/api/ui.go` (static file serving from an `fs.FS`, index at `/`, fallback page, `Cache-Control: no-cache` on `index.html` and long max-age on hashed assets)
- `web/embed.go` (`package web`, `//go:embed all:dist`, `var Dist embed.FS`)
- `web/dist/.gitkeep` so the embed pattern always matches on a fresh clone
- `docs/integration/guide-api.md`, the contract above as finalised by the code, with any deviations called out

Touch:

- `.gitignore`: change `web/dist/` to `web/dist/ui/` so the `.gitkeep` is tracked and Vite's output is not.
- `cmd/channel3/serve.go`: flags `--listen` (default `:3333`), `--ui-dir <path>` (serve the UI from disk instead of the embedded FS, for development). Start the HTTP server alongside the station; shut it down on exit.

### Tasks

1. [x] Handlers for the three endpoints exactly as the contract. `now` and `next` come from `schedule.Guide(ch, now, 0)` limited to two slots, or equivalently `At` plus the following slot. `hours` validation returns 400 with a message.
2. [x] Static UI. `UI` is `fs.Sub(web.Dist, "dist/ui")` in production or `os.DirFS(--ui-dir)` in development. Missing `index.html` yields the fallback page.
3. [x] Fresh-clone rule. `go build ./...` and `go test ./...` must succeed with Node never installed. That is what `web/dist/.gitkeep` and the `dist/ui/` sub-directory are for: Vite in Phase 8 empties `web/dist/ui/` on each build and never touches `.gitkeep`, so builds leave the tree clean.
4. [x] Serve wiring. HTTP server with sane timeouts, started before mpv is launched so `/api/channels` and `/api/guide` answer as soon as the library is loaded; `/api/now` reports tuned null until the station tunes. Log the listen address once.
5. [x] Integration summary at `docs/integration/guide-api.md`.

### Tests

- `handlers_test.go` with `httptest` and fixture channels, a fixed `Now`, a fixed `Tuned`: each endpoint's JSON matches a golden file byte for byte after normalising whitespace; channels come back sorted by number; a channel with no items gives `null` now and next and an empty `slots` array; `hours=0`, `hours=49`, `hours=abc` give 400 with an `error` field; `hours` omitted means 6 and `to` minus `from` is exactly 6 h; the fallback page is served when the FS has no `index.html`; `index.html` is served at `/` when present.

### Output

- A running API on :3333 in dev.
- `docs/integration/guide-api.md` for Phase 8 and for anyone writing a client later.

### Branch

`feature/channel-three-mvp-phase-7`

### Verification checklist

- [ ] `make lint`, `make test`, `make build-arm64` pass on a fresh clone without running npm.
- [ ] `bin/channel3 serve --root ~/srv/channel3 --no-input --mpv-arg=--no-fullscreen` then `curl -s localhost:3333/api/channels | jq`, `/api/now`, `/api/guide?hours=2`: shapes match the contract; `now.offset` agrees with the mpv window within a couple of seconds; `tuned` is the playing channel.
- [ ] `curl -i 'localhost:3333/api/guide?hours=99'` is a 400 with a JSON error.
- [ ] `curl -s localhost:3333/` returns the fallback page (no UI built yet).
- [ ] `lsof -nP -iTCP:3333 -sTCP:LISTEN` shows only `channel3`. Nothing listens on 3000.
- [ ] No endpoint accepts POST, PUT or DELETE (each returns 405).
- [ ] `docs/integration/guide-api.md` exists and matches the handlers.

---

## Phase 8: Guide web page [web/]

### Objective

A single read-only React page, phone-first, that shows every channel with its number, name, what is on now with a progress bar and minutes remaining, what is on next, and highlights the tuned channel; it refreshes every 30 s. Built by Vite into `web/dist/ui/` where Phase 7's embed picks it up. This is the frontend half of MVP item 5.

### Input

- The API contract in Parallel Group B, and `docs/integration/guide-api.md` once Phase 7 lands.
- Node v20.19.5 is installed on the Mac.
- Dev workflow decision: one port. `channel3 serve --ui-dir web/dist/ui` serves the built files from disk on :3333 and `npm run dev` is `vite build --watch`. There is no Vite dev server and no second port. Hot module reload is not worth a second port for one read-only page.

### Files

Create under `web/`:

- `package.json`, `package-lock.json`, `vite.config.ts` (`build.outDir: 'dist/ui'`, `emptyOutDir: true`), `tsconfig.json`, `index.html`
- `src/main.tsx`, `src/App.tsx`, `src/api.ts` (typed fetchers for `/api/now` and `/api/guide`), `src/Guide.tsx` (channel rows), `src/format.ts` (times, remaining minutes, progress fraction)
- `src/fixtures/now.json`, `src/fixtures/guide.json` (copies of the contract examples)
- `src/format.test.ts`, `src/Guide.test.tsx` (Vitest plus React Testing Library)
- `eslint.config.js`, `.prettierrc` if desired

Touch:

- `Makefile`: `ui` (`npm ci && npm run build` in `web/`), `ui-dev` (`npm run dev`), and make `build` and `build-arm64` depend on `ui`.
- `CLAUDE.md`: two lines under Commands for `make ui-dev` plus `bin/channel3 serve --ui-dir web/dist/ui`.

Do not touch `web/embed.go`, `web/dist/.gitkeep`, or `.gitignore`.

### Tasks

1. [ ] Scaffold Vite with the React and TypeScript template. Strip the demo. Set `outDir` and `emptyOutDir` as above. Confirm `npm run build` leaves `git status` clean apart from nothing.
2. [ ] `api.ts`: typed fetch for `/api/now` and `/api/guide?hours=N` with the contract's shapes; a thrown error on non-2xx.
3. [ ] `Guide.tsx`: one row per channel sorted by number: big channel number, name, now title, a progress bar from `offset / duration` advanced client-side each second between refreshes, "N min left", then next title and its start time. A channel with `now: null` shows "Please Stand By". The tuned channel gets a visible highlight. A "later" disclosure per channel expands to the guide slots for the default 6 h.
4. [ ] `App.tsx`: fetch `/api/now` on mount and every 30 s; fetch `/api/guide` on mount and every 5 min; show the server `time` in the header; show a quiet inline error when a fetch fails and keep the last good data.
5. [ ] Layout: single column, large touch targets, readable at arm's length on a phone; a two-column grid above 900 px is optional. No router, no state library, no component kit. Plain CSS or CSS modules.
6. [ ] Lint and type check: `npm run lint` and `npm run typecheck` (`tsc --noEmit`) both clean.
7. [ ] Makefile and CLAUDE.md updates.

### Tests

- `format.test.ts`: remaining minutes rounds sensibly at boundaries; progress fraction clamps to 0 to 1; local time formatting for a fixed RFC 3339 input.
- `Guide.test.tsx`: renders the fixture with two channels; the tuned channel has the highlight class; a channel with `now: null` shows "Please Stand By"; channels appear in number order regardless of fixture order.

### Output

- `web/dist/ui/` produced by `make ui`, embedded by `make build`.

### Branch

`feature/channel-three-mvp-phase-8`

### Verification checklist

- [ ] `cd web && npm ci && npm run lint && npm run typecheck && npm test && npm run build` all pass. `git status` is clean afterwards.
- [ ] `make build` produces a binary that embeds the UI: `bin/channel3 serve --root ~/srv/channel3 --no-input --mpv-arg=--no-fullscreen` then open `http://localhost:3333/` and see the guide with no `--ui-dir`.
- [ ] Rows are in channel-number order, the tuned channel is highlighted, the progress bar moves, and the page updates within 30 s of a channel change made from the terminal.
- [ ] Open from a phone on the same network at `http://<mac-ip>:3333/`. Text is readable and nothing overflows horizontally.
- [ ] Stop `serve` with the page open: the page shows its inline error and keeps the last data instead of blanking.
- [ ] `grep -rn "fetch(" web/src | grep -v "/api/"` finds nothing. The page calls only its own API and only with GET.
- [ ] `web/embed.go`, `web/dist/.gitkeep` and `.gitignore` are unchanged in the diff.

---

## Sync Point B

Phases 7 and 8 merged. `make build` embeds the real UI and `bin/channel3 serve` on the Mac serves the guide at :3333 with correct data for the channel that is playing. This is the last all-Mac milestone.

---

## Phase 9: systemd, deploy script, Pi setup, quiet boot [deploy/] (hardware-gated)

### Objective

A fresh Raspberry Pi OS Lite install becomes a Channel Three box with one setup script, gets new builds with `make deploy`, boots to video with no console text, restarts `channel3` on failure, and closes out every hardware-gated task left open in Phases 4, 5 and 6. This is MVP item 6.

### Input

- `docs/hardware.md`: mpv flags, audio device, Flirc device name, CEC yes or no, boot time, console behaviour.
- Phases 5 and 6 flags: `--root`, `--listen`, `--mpv-arg`, `--input-device`, `--cec`.
- The global Porter rules in the user's CLAUDE.md are a useful analogue even though this is not Porter: a deploy restarts the service and kills anything in flight, so never deploy while an ingest is running or while the kids are watching.

### Files

Create:

- `deploy/channel3.service` (systemd unit)
- `deploy/pi-setup.sh` (one-time, idempotent, run as root over ssh)
- `deploy/deploy.sh` (build arm64, scp, atomic swap, restart, tail the journal for 10 s)
- `deploy/ingest.sh` (run `channel3 ingest` on the Pi over ssh with the right root, refuse if `serve.pid` guard trips, print the summary)
- `deploy/README.md` (how to use the four files, what the unit assumes, how to roll back by copying the previous binary back)

Touch:

- `Makefile`: `deploy` (calls `deploy/deploy.sh`, requires `PI_HOST`), `pi-setup`, `pi-ingest`.
- `CLAUDE.md`: add `deploy/` to the layout and a Deploy section with the three make targets and the "never deploy while ingest runs or kids watch" rule.
- `docs/hardware.md`: final confirmation of every gated task.

### Tasks

1. [ ] `pi-setup.sh`. `apt install mpv v4l-utils ffmpeg`. Install yt-dlp as the pinned standalone binary from its GitHub release into `/usr/local/bin` with the version recorded in the script, because apt's yt-dlp is stale and the brainstorm says to pin and update deliberately. Create system user `channel3` in groups `video`, `render`, `input`, `audio`. Create `/srv/channel3/{channels,library,local}` owned by that user. Install the unit and enable it. Add to `/boot/firmware/cmdline.txt`: `quiet loglevel=0 logo.nologo vt.global_cursor_default=0 consoleblank=0`. Disable `getty@tty1` so nothing draws on the HDMI console. Set `hdmi_force_hotplug` or the Pi 5 equivalent in `config.txt` only if Phase 4 found the TV power cycle drops the output. Print what changed. Safe to re-run.
2. [ ] `channel3.service`. `After=local-fs.target` and `RequiresMountsFor=/srv/channel3`. `User=channel3`. `ExecStart=/usr/local/bin/channel3 serve --root /srv/channel3 --listen :80 --mpv-arg=<flags from hardware.md>` with `--cec` only if Phase 4 said yes. `AmbientCapabilities=CAP_NET_BIND_SERVICE` for :80. `RuntimeDirectory=channel3` for the mpv socket and standby PNG. `Restart=always`, `RestartSec=1`. `Environment=HOME=/srv/channel3` for mpv's config lookup. Do not wait for `time-sync.target`: the station corrects itself when NTP lands, and waiting would delay boot-to-video.
3. [ ] `deploy.sh`. `make build-arm64`, `scp` to `/usr/local/bin/channel3.new`, `ssh` to `mv` it over the old one (keep `channel3.prev`), `systemctl restart channel3`, then `journalctl -u channel3 -f` for ten seconds and exit non-zero if the unit is not active. Refuse to run if an `ingest` process is running on the Pi (`ssh $PI_HOST pgrep -f "channel3 ingest"`), since the restart would kill it mid-download. `serve.pid` is not the signal here: it exists whenever the service is up.
4. [ ] `ingest.sh`. `ssh $PI_HOST sudo -u channel3 channel3 ingest --root /srv/channel3 "$@"`, stream the summary, propagate the exit code. Document that new items appear at the next 04:00 rollover or after a restart, by design.
5. [ ] Copy a real channel config to `/srv/channel3/channels/` on the Pi and run `make pi-ingest` for a small channel.
6. [ ] Power on the Pi with the TV on. Measure power-to-video with a stopwatch. Target under 30 s. Record it.
7. [ ] Close out gated tasks: Phase 5 task 9 (serve on the Pi, boundary transition, `pkill mpv` recovery), Phase 6 tasks 7 and 8 (Flirc tuning, CEC if applicable). Tick them in this plan and update `docs/hardware.md`.
8. [ ] Leave it running for an evening. Check `journalctl -u channel3` the next morning for restarts, excluded items and the 04:00 rescan line.

### Tests

- `bash -n` and `shellcheck` clean on all three scripts (add `shellcheck` to `make lint` for `deploy/*.sh`).
- `systemd-analyze verify deploy/channel3.service` passes on the Pi.
- No Go tests are added in this phase.

### Output

- A Pi that is a Channel Three box.
- `deploy/` and the CLAUDE.md Deploy section.
- All hardware-gated tasks in this plan checked or explicitly waived.

### Branch

`feature/channel-three-mvp-phase-9`

### Verification checklist

- [ ] On a freshly flashed Lite image, `make pi-setup PI_HOST=channel3.local` completes; re-running it changes nothing.
- [ ] `make deploy PI_HOST=channel3.local` finishes with the unit active and the journal showing the Stand By load followed by a schedule load.
- [ ] Power cycle the Pi: no boot text, no cursor, no login prompt on the TV; video within the recorded time.
- [ ] `curl http://channel3.local/api/now` from the Mac works on port 80; the guide page loads on a phone.
- [ ] `ssh channel3.local pkill mpv`: video returns within five seconds.
- [ ] Flirc channel up, down and digits work from the couch. CEC power and volume work, or the doc says the universal remote handles them.
- [ ] `ssh channel3.local ls /srv/channel3` shows no state files other than `serve.pid`.
- [ ] `make deploy` while an ingest is running is refused.
- [ ] Every `(hardware-gated)` task in Phases 4, 5, 6 and 9 is checked in this file, or has a one-line waiver.

---

## Repo Strategy

Monorepo. One Go module at the root with `web/` as a Node sub-project whose build output is embedded into the Go binary. No separate repos, no submodules. Sub-projects for the purpose of parallel groups: `internal/*` and `cmd/` (Go), `web/` (Node), `deploy/` (shell and systemd), `docs/`.

## Dependencies

- Go 1.26.2, yt-dlp 2026.08.19, ffmpeg and ffprobe, Node v20.19.5 are installed on the Mac. mpv is not; `brew install mpv` before Phase 5. Port 3333 is free.
- Go modules expected: `gopkg.in/yaml.v3` (Phase 1), `golang.org/x/term` and `golang.org/x/sys/unix` for the evdev ioctls (Phase 6). Everything else is the standard library. No cgo.
- Hardware purchase (Smitty #251) gates Phases 4 and 9 and one task each in Phases 5 and 6. TV CEC check (Smitty #252) is folded into Phase 4.
- `~/srv/channel3` is created in Phase 1 and populated by hand with real config for verification. It is never committed.

## Risks & Open Questions

- **Pi 5 video output with no desktop.** Top risk, owned by Phase 4. Trixie's mpv 0.40 has reported Vulkan failures at 1080p that `--gpu-api=opengl` fixes, and `--vo=drm` is a second fallback. If none work, the brainstorm's fallback is a minimal Wayland kiosk, which changes Phase 9 and must be written into this plan first.
- **H.264 software decode on the Pi 5.** No hardware H.264. 1080p30 should be comfortable; Phase 4 measures it. If it drops frames, the cheap fix is to cap ingest at 720p; transcoding to HEVC at ingest is the expensive fix and is out of scope until proven necessary.
- **mpv `loadfile` signature.** Changed in 0.38. Both targets ship 0.40 today. Phase 5 pins the four-argument form and checks the version at startup. If a Pi image ends up on Bookworm's 0.35, add a version branch in one place.
- **TV power cycling and HDMI hotplug.** Unknown until Phase 4. The supervisor's health check and `RestartSec=1` are the mitigation; Phase 4's result may tighten them.
- **yt-dlp breakage.** Ingest-time only, by design. Pinned as a standalone binary in Phase 9's setup script. Playback never depends on it.
- **Clock at boot.** The Pi 5 has an RTC but ships without a battery. Without one the clock is wrong until NTP, and the station shows the wrong item for up to 30 s after boot then corrects. A coin cell for the RTC header is a cheap fix worth adding to the hardware order.
- **Ingest CPU contention with playback.** Running yt-dlp and ffmpeg on the Pi while it software-decodes 1080p may stutter. The `serve.pid` guard prevents it on the same machine; the documented practice is to ingest at night or from the Mac and rsync the library.
- **Embedded UI and fresh clones.** Handled by the `.gitkeep` plus `dist/ui/` layout in Phase 7. If an executor changes that layout, `go build` breaks on fresh clones.
- **Flirc key repertoire.** Flirc may or may not emit consumer-page keys such as `KEY_CHANNELUP`. Phase 6 maps arrows and page keys as aliases so any remote button that Flirc can record will work.
- **Storage creep.** Capped at 1080p H.264. A prune command is out of scope until it matters.

## Assumptions and open questions

Decisions this plan made where the brainstorm was silent. Each is a reasonable default, none blocks work, and any can be changed by editing the phase that owns it.

1. **Broadcast day starts at 04:00 local** in the Pi's configured time zone (decided in the brainstorm; restated because the `schedule.Clock` type carries it).
2. **Digit-entry timeout is 1.5 s**, with immediate commit when no channel number can extend the typed prefix, and up or down cancels pending digits. Owned by Phase 6.
3. **Guide horizon default is 6 h, minimum 1, maximum 48**, integer hours. Owned by Phase 7.
4. **The "Please Stand By" asset is a committed 1920x1080 PNG** at `internal/player/assets/standby.png`, embedded in the binary and displayed with mpv's `--image-display-duration=inf`. It is framework, not content. Owned by Phase 5.
5. **`channel3 guide` is in MVP** as a text subcommand built in Phase 3 because it is the cheapest way to exercise the schedule before the player exists.
6. **Sidecar schema and library layout** are as written in Phase 1. Local `file://` items are referenced in place, not copied; their id is the slugified basename.
7. **Durations come from ffprobe on the final file**, with yt-dlp's integer duration as a logged fallback. Owned by Phase 2.
8. **Initial channel at boot is the lowest channel number**, overridable with `--start-channel`. No "last channel" is ever stored.
9. **The library is rescanned at startup and at the 04:00 rollover only.** No SIGHUP, no file watching. Ingested items appear the next broadcast day or after a restart.
10. **mpv 0.38 or newer is required.** Both the Mac and Trixie ship 0.40.
11. **Reconcile tick every 30 s with a 5 s tolerance** corrects clock jumps and sidecar drift. Owned by Phase 5.
12. **One dev port.** `serve --ui-dir web/dist/ui` on :3333 plus `vite build --watch`. No Vite dev server.
13. **Go module path** is chosen in Phase 1. Use the GitHub path if the remote exists by then, otherwise `channel3`; renaming later is mechanical.
14. **On the Pi the service listens on :80** as user `channel3` with `CAP_NET_BIND_SERVICE`, in groups `video`, `render`, `input`, `audio`.
15. **Ingest normally runs on the Pi** over ssh via `deploy/ingest.sh`, with yt-dlp pinned as a standalone binary. Ingesting on the Mac into `~/srv/channel3` and rsyncing the library is also fine.
16. **Key aliases:** channel up accepts `KEY_CHANNELUP`, `KEY_UP`, `KEY_PAGEUP`; channel down accepts the mirror set; digits accept both the number row and the keypad.
17. **CEC is in MVP only if Phase 4 finds the TV supports it.** Otherwise `--cec` stays off and the universal remote carries power and volume, as the brainstorm already decided.
18. **Config root** is `--root`, default `/srv/channel3`, containing `channels/` and `library/`. `CHANNEL3_ROOT` is honoured. On the Mac it is `~/srv/channel3`.
19. **Corrupt-file exclusions live in memory** for the current process and broadcast day. They are rebuilt from scratch at the rollover and on restart, never written down.
20. **`serve.pid` is the only file `serve` writes** under the root, and only to stop ingest from running concurrently on the same machine. It is not playback state.
21. **evdev is pure Go**, hand-rolled or a pure-Go library at the executor's discretion, so the static arm64 build holds.

Open questions that stay open and do not block:

- Does the TV support CEC? Smitty #252, answered in Phase 4.
- NVMe HAT or USB SSD? Decided at purchase; Phase 9's `RequiresMountsFor` covers either.
- Is 1080p H.264 software decode smooth enough on the Pi 5? Phase 4 measures; the fallback is a 720p ingest cap.
- Which remote buttons can Flirc record as which keys? Phase 4 records the table.
- GitHub org and module path, if the framework goes public later.

## Cross-Repo Notes

- Single repo, so there are no cross-repo contract changes. The one internal contract handoff is the API in Parallel Group B: Phase 7 finalises it in `docs/integration/guide-api.md` and Phase 8 consumes it.
- Backend contract changes needed: N (defined in this plan before either side is built).
- Frontend integration summary required: Y (`docs/integration/guide-api.md`, written by Phase 7).
