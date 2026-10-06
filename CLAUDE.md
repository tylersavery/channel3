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

## Commands

- `make build` builds `bin/channel3` for the Mac. `make build-arm64` builds `bin/channel3-linux-arm64` for the Pi, static and cgo-free.
- `make test` runs `go test -race ./...`. `make lint` runs gofmt and `go vet`, then `bash -n` and `shellcheck` over `deploy/*.sh`, and fails on any output. `shellcheck` comes from Homebrew: `brew install shellcheck`.
- `make run` serves against the local root on :3333. It works once `serve` accepts `--listen` in Phase 7.
- `make ui` builds the guide page into `web/dist/ui`, which `make build` and `make build-arm64` embed in the binary. `go build ./...` on its own never needs Node.
- `make ui-dev` rebuilds the page on every save. Serve it from disk in another terminal with `bin/channel3 serve --root ~/srv/channel3 --ui-dir web/dist/ui`, which is one port and no Vite dev server.
- `cd web && npm run lint`, `npm run typecheck` and `npm test` cover the page. The tests pin `TZ=America/Toronto` and an en_US locale so the formatted times are the same everywhere.
- `make dev-root` creates `~/srv/channel3/{channels,library,local}`, the local root that holds real channel config and media. Nothing from this repo goes in it.
- Every subcommand takes `--root`, which defaults to `/srv/channel3`, is overridden by `$CHANNEL3_ROOT`, and may appear before or after the subcommand name.

## Rules

- The schedule is a pure function: `(channel, t) -> (item, offset)`. Seeded shuffle per broadcast day, cumulative durations, recomputed at every boundary. No playback state is ever persisted.
- Ingest is the only code that touches the network. Playback reads local files only.
- Real channel config and the media library live outside the repo (`/srv/channel3` on the Pi, `~/srv/channel3` on the Mac). The repo carries `channels/example.yaml` only. The framework may go open source; content never does.
- Broadcast mode has no pause and no seek. Movie mode (later) is the only place controls exist. On-screen chrome is kept to what the owner approves, case by case: today that is the channel number for a moment after a change. Anything more (a channel bumper, say) is asked for first, never added on initiative.
- The guide API is read only, with one exception: `POST /api/upload` hands a home video to the Home Movies inbox. It exists only when serve has an upload PIN (`CHANNEL3_UPLOAD_PIN` in `/etc/default/channel3`, never in the repo) and changes nothing about what plays. Nothing else may write.
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
deploy/              systemd unit, Pi setup, deploy and ingest scripts
docs/plans/          design docs and plan.md
```

## Deploy

- `make pi-setup PI_HOST=channel3.local PI_DISK=/dev/sda1` turns a freshly flashed Pi into a Channel Three box. It is idempotent.
- `make deploy PI_HOST=channel3.local` builds arm64, copies the binary over, keeps the previous one as `channel3.prev` and restarts the service.
- `make pi-ingest PI_HOST=channel3.local ARGS="--channel bluey"` runs an ingest on the Pi as the service user. Ingest cannot run while `serve` holds the pid guard, so it stops the service, ingests and starts it again, and the TV is dark for the duration. Ingesting on the Mac and rsyncing the library is the no-downtime route.
- **Never deploy while ingest runs or while the kids are watching.** A deploy restarts the service and kills anything in flight.
- Every flag Phase 4 discovers (mpv output, audio device, `--cec`, `--input-device`) lives in `/etc/default/channel3` as `CHANNEL3_FLAGS`, never in the unit. `deploy/README.md` has the full procedure.
