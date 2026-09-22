# Channel Three: brainstorm

Date: 2026-09-22. Source: Tyler's voice note plus rulings the same day. Vault: `~/tybrain/projects/channel-three.md`.

## Problem

Streaming is infinite choice. A kid asks for airplanes, gets an approved video, and changes his mind minutes or seconds in. Broadcast TV had a constraint that was good for kids: these are the channels, this is what's on, that is all there is. Channel Three brings that back on purpose, to introduce TV as a habit and to teach patience and attention. Built for Tyler's kids; the framework can be open source later, the content never.

## Goals

- Themed channels (Train TV, Farm TV) of approved videos, mostly YouTube, later Vimeo and local files.
- Deterministic: what's playing on a channel is a pure function of the clock and the config. Turn it on and it is mid-show at the right timecode. When a video ends, the next begins.
- No controls except changing channel. No pause, no scrub, no ads, no related videos, no on-screen chrome.
- One simple remote for everything: power, volume, channel.
- A guide: what's on now and next per channel.
- Seamless. It must never show a menu, a spinner, or a YouTube page.

## Non-goals (for now)

- Admin UI for editing channels. Config is a file.
- Auth. The box is on the home network only.
- Movie mode with pause. Later.
- Bumps, station idents, "coming up next" cards, dayparts, sign-off at bedtime. Later, and all cheap once the core exists.
- Playing through YouTube's embed. Ruled out.
- Multiple TVs.

## Rulings (2026-09-22)

1. **Library route.** Approving a video means ingesting it with yt-dlp into a local library. Playback is mpv on local files. No ads, no account, exact seek, offline. Storage is not a problem: 1080p H.264 runs 1 to 2 GB per hour, a 1 TB drive holds about 500 hours. NAS is optional (backup, movies later).
2. **Hardware is new.** Pi 5 4 GB, active cooler, 27 W supply, case, M.2 HAT+ with 1 TB NVMe or a USB SSD, micro-HDMI cable, Flirc USB, a few-button remote. Roughly $200 to $250.
3. **TV is a basic 1080p set** with HDMI. CEC unknown until checked in its menu. Fallback is a universal remote that sends the TV's own power and volume codes, with Flirc learning only the channel buttons.
4. **Schedule shifts per day** (seeded shuffle). No fixed grid; viewing times are random anyway. Revisit when channels exist.
5. **Name:** Channel Three. Repo `channel3`, binary `channel3`.

## Approaches considered

- **Kiosk browser + YouTube IFrame API + Premium login.** Compliant, but every failure mode breaks the illusion: ads when the session drops, end-screen overlays, hover chrome, keyframe-only seeks, autoplay policy, buffering. Rejected.
- **Existing simulators** (FieldStation42, 8008tub3, ErsatzTV, Tunarr). Close in spirit; wrong shape (Plex-side IPTV, or Python with hour-block schedules and no YouTube ingest). Worth reading FieldStation42's catalog and schedule code. Building custom.
- **Library + mpv + Go service.** Chosen. Curation and caching become the same act; Vimeo and local files ride the same pipeline.

## Architecture

One Go binary, `channel3`, running as a systemd service on the Pi.

```
channels/*.yaml  --ingest-->  library/<channel>/<id>.mp4 + <id>.json (title, duration, source)
                                     |
clock + config  --schedule-->  (item, offset)  --player-->  mpv (JSON IPC)  -->  HDMI  -->  TV
                                     ^                                             ^
Flirc (evdev keys) ---- input -------+                     cec-ctl (power/volume) -+
                                     |
                        HTTP API + embedded React UI (guide) on :3333 (dev) / :80 (Pi)
```

Components:

- **schedule** (pure). Input: channel config with per-item durations, local date, time `t`. Order for the day = items shuffled with seed `hash(channel.id, broadcastDay)`. Total `T` = sum of durations. `offset = (t - dayStart) mod T`; walk cumulative durations to find the item and its offset. Broadcast day starts at 04:00 local, so the daily reshuffle happens when nobody is watching. Guide = same function evaluated forward N hours. Nothing persisted.
- **library**. `channel3 ingest` reads `channels/*.yaml`, expands playlists, runs yt-dlp per source (prefer `avc1` at or below 1080p, merge to mp4), writes the file and a JSON sidecar with title, duration, source URL, ingest date. The index is a scan of sidecars. Items whose file is missing are excluded from the day's order.
- **player**. Supervises one mpv process: `--fullscreen --no-osc --no-osd-bar --no-input-default-bindings --idle=yes --input-ipc-server=<sock>`, on the Pi via DRM/KMS with no desktop. Commands: `loadfile <path> replace start=<offset>`. Observes `end-file`; on every boundary it asks schedule for the current item again rather than chaining "next", so load latency never accumulates into drift.
- **input**. Reads Flirc keypresses from `/dev/input/event*`. Channel up/down and digits. Power and volume go out over CEC (`cec-ctl` on `/dev/cec0`). If CEC is absent, the remote itself carries the TV's codes for those buttons and the Pi never sees them.
- **api**. `/api/now`, `/api/guide?hours=6`, `/api/channels`. Serves the embedded React build. MVP UI is the guide, read-only.

Data flow on channel change: key event, schedule lookup for the new channel at `now`, `loadfile` with the offset, done. Under a second on local files.

## Channel config

```yaml
# channels/example.yaml
id: trains
number: 3
name: Train TV
sources:
  - https://www.youtube.com/watch?v=...
  - https://www.youtube.com/playlist?list=...
  - file:///srv/channel3/local/steam-engines.mp4
```

Real config lives in `/srv/channel3/channels/` on the Pi (`~/srv/channel3/` on the Mac); the repo carries the example only.

## Error handling

- **Missing or corrupt file:** drop the item from today's order, log it, keep the channel running.
- **mpv exits:** supervisor restarts it and re-seeks to the schedule's current answer. The kid sees a blink.
- **Ingest failure:** sidecar marks the item failed with the yt-dlp error; the channel plays without it; `channel3 ingest` retries next run. yt-dlp breakage only ever affects ingest.
- **No channels or empty library:** show a "Please Stand By" card (a static image loop in mpv), not a blank screen.
- **Clock jump (NTP after boot):** schedule is recomputed on the next tick, so the box self-corrects.

## Testing

- `internal/schedule`: table-driven tests. Same inputs always give the same item and offset; boundaries land on exact cumulative durations; the day rollover reshuffles; a removed item shifts the order predictably.
- `internal/library`: ingest against a fixture yt-dlp info JSON and a tiny mp4; sidecar round-trip.
- `internal/player`: fake IPC socket asserting the exact `loadfile` command and offset, and that an `end-file` event triggers a fresh schedule lookup.
- Hardware checklist, done once on the real Pi: mpv on DRM/KMS without a desktop, Flirc key codes, `cec-ctl` power and volume against the actual TV, H.264 1080p playback smooth.

## MVP scope

1. Example config, `channel3 ingest`, library with sidecars.
2. Schedule package with tests.
3. Player supervisor, `channel3 serve` playing one channel from boot.
4. Channel switching from a keyboard, then Flirc.
5. Guide page on :3333.
6. systemd unit and a deploy script (`GOARCH=arm64` build, scp, restart).

## Risks

- **Pi 5 video output with no desktop.** Verify mpv on DRM/KMS first, before anything else. Fallback is a minimal Wayland kiosk.
- **CEC flakiness.** Fallback is the universal-remote route; decided already.
- **yt-dlp breakage.** Ingest-time only. Pin the version, update deliberately.
- **Storage creep.** Cap ingest at 1080p H.264; add a prune command when it matters.

## Open questions

- Pi 5 vs Pi 4: Pi 5 chosen (HEVC hardware decode, fast software H.264). Confirm the NVMe HAT vs USB SSD choice at purchase.
- Whether the guide should also render on the TV as a channel (the old "preview channel"). Later.
- Where the NAS fits, if at all.

## Next step

Run the planner against this document to produce `docs/plans/plan.md` with phases matching the MVP list. Buy the hardware in parallel; phases 1 and 2 need no Pi.
