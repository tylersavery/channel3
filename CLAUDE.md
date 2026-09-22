# Channel Three

Broadcast TV appliance for kids on a Raspberry Pi. Themed channels of approved videos, a schedule that is a pure function of the clock, one remote, no controls. Design doc: `docs/plans/2026-09-22-channel-three-brainstorm.md`. Vault note: `~/tybrain/projects/channel-three.md` (Smitty project 19).

## Stack

- **Service:** Go, one static binary cross-compiled for arm64 (`GOOS=linux GOARCH=arm64`). Owns the scheduler, the player, input, CEC, the HTTP API and the embedded web UI.
- **Player:** mpv on the Pi, driven over its JSON IPC socket. The service never renders video itself.
- **Ingest:** yt-dlp (YouTube, Vimeo) plus plain files. Runs on demand, never during playback.
- **Web UI:** React + Vite in `web/`, built into the binary with `embed.FS`. Phone and laptop only; the TV shows mpv and nothing else.
- **Input:** Flirc USB (any IR remote, presents as a keyboard, read via evdev). TV power and volume over HDMI-CEC (`cec-ctl`), with a universal remote as the no-CEC fallback.
- **Target:** Pi 5, Raspberry Pi OS Lite, mpv from apt, service under systemd.

## Dev port

**3333** for the web UI and API in development. Do not use 3000.

## Rules

- The schedule is a pure function: `(channel, t) -> (item, offset)`. Seeded shuffle per broadcast day, cumulative durations, recomputed at every boundary. No playback state is ever persisted.
- Ingest is the only code that touches the network. Playback reads local files only.
- Real channel config and the media library live outside the repo (`/srv/channel3` on the Pi, `~/srv/channel3` on the Mac). The repo carries `channels/example.yaml` only. The framework may go open source; content never does.
- Broadcast mode has no pause, no seek, no on-screen chrome. Movie mode (later) is the only place controls exist.
- Tests alongside code. The schedule package is table-tested; the player is tested against a fake IPC socket.

## Layout (planned)

```
cmd/channel3/        main: serve | ingest | guide
internal/schedule/   pure scheduling + guide
internal/library/    ingest, sidecars, index
internal/player/     mpv IPC supervisor
internal/input/      evdev keys, CEC
internal/api/        HTTP, embedded UI
web/                 React UI
channels/            example config only
docs/plans/          design docs and plan.md
```
