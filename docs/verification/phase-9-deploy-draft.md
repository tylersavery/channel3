# Verification Report: Phase 9 — systemd, deploy script, Pi setup, quiet boot (draft)

## Summary

This is a draft review. There is no Pi, no television, no Flirc and no remote, so nothing in `deploy/` has been run against hardware, and every item that only hardware can settle is recorded as WARN rather than FAIL. What can be checked on the Mac was checked: lint, shellcheck, `bash -n`, the Go test suite, the guards on all three make targets, and a line by line read of the unit against the systemd.unit, systemd.service and systemd.exec manual pages. `systemd-analyze verify` cannot run here because systemd does not exist on macOS, so it stays a hands-on step for the day the hardware arrives.

The shape of the phase is right. The unit is careful and its comments explain themselves, `pi-setup.sh` is genuinely idempotent step by step, `deploy.sh` checks for a running ingest before it builds and runs `timeout` on the Pi rather than reaching for a `timeout` that macOS does not ship, and the README reads like something a person could follow at 9pm with a child waiting. The env file indirection is a better decision than the plan's bake-the-flags-into-ExecStart, and the plan records it.

The first pass below found two defects in `deploy/channel3.env.example` and failed the phase on them; both were fixed in a remediation round, so the Re-verification section near the end of this report, not this paragraph, carries the current verdict.

Two defects in `deploy/channel3.env.example` were the reason the first pass was not a pass. That file is the one file the operator edits by hand after Phase 4, and two of its instructions are wrong in ways that end with a dark television: following the "uncomment" instruction produces lines systemd cannot parse, so the Phase 4 flags never reach mpv, and the startup channel example uses a channel number where the flag takes a channel id, which makes `serve` refuse to start and the unit crash loop into the failed state. Both fixes are a few lines of comment in one file. Nothing else needs to change before the hardware arrives.

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Tests pass | PASS | `make test` (`go test -race ./...`) exit 0, every package ok, no Go code changed this phase |
| Lint | PASS | `make lint` exit 0; `bash -n` clean on all three scripts; `shellcheck deploy/*.sh` exit 0 with shellcheck 0.11.0; the three `SC2029` disables in `deploy.sh` are each justified, confirmed by re-running shellcheck with them stripped |
| Matches plan | PASS | Tasks 1 to 4 implemented; the two deviations (flags in `/etc/default/channel3`, `XDG_RUNTIME_DIR` in the unit) are recorded in the plan at task 2 |
| Security | PASS | Runs as a non-login system user, `CAP_NET_BIND_SERVICE` only, no secrets in the env file, nothing formats a disk, `config.txt` untouched |
| Code quality | WARN | Scripts are readable and commented; see the WARN list for the `mount -a` abort path and the ingest exit code conflation |
| Scope | PASS | Only `deploy/`, `Makefile`, `CLAUDE.md` and the Phase 9 plan section changed; no implementation file was touched |
| Integration summary | N/A | No API change in this phase; `docs/integration/guide-api.md:129` asked Phase 9 to design the restart policy around a failed mpv launch, which is the second WARN below |
| Hardware verification | WARN | Plan tasks 5 to 8 unticked with a one line note each, no Verification checklist item ticked, `systemd-analyze verify` not runnable on macOS. This is correct for draft mode |

## Context Health

Context scaffold not present. `.claude/context/` does not exist; run `/intel` if it is wanted.

## Issues

### FAIL (must fix)

- `deploy/channel3.env.example:12` (with the candidate flags at `:20`, `:32`, `:40`, `:45`, `:54`, `:60`, `:65`, `:70` and the assignment at `:72`) The header says "Uncomment what Phase 4 proves and leave the rest alone", but the candidates are bare flag text, not `key=value`. Uncommenting `--mpv-arg=--vo=drm` gives systemd a line it cannot parse, which it logs and ignores, leaving `CHANNEL3_FLAGS` empty. The operator then gets a service that starts with no mpv output flags and a black television, and the journal blames mpv rather than the edit. Fix: change line 12 to say that the flags are copied into the `CHANNEL3_FLAGS` assignment at the bottom, and show one worked example, for instance `CHANNEL3_FLAGS="--mpv-arg=--vo=gpu --mpv-arg=--gpu-context=drm --mpv-arg=--gpu-api=opengl --mpv-arg=--hwdec=no"`.
- `deploy/channel3.env.example:60` `--start-channel=3` is a channel number, but the flag takes a channel id. `cmd/channel3/serve.go:65` documents it as an id, `cmd/channel3/station.go:157` tunes by id, and an id that matches nothing is refused at `cmd/channel3/station.go:151`, which makes `serve` exit 1. Under `Restart=always` that is a crash loop that ends in the failed state after five tries. With the shipped example config the correct value is `--start-channel=trains`. Fix: change the example to an id and add the sentence that this is the `id:` field from the channel YAML, not the `number:` field typed on the remote.

### WARN (should review)

- `deploy/channel3.service:6-7` `Wants=network-online.target` and `After=network-online.target` order the service behind NetworkManager-wait-online, which on Raspberry Pi OS is enabled by default and can hold boot for many seconds on Wi-Fi. That works against the under 30 s power-to-video target of plan task 6 and contradicts the unit's own reason for skipping `time-sync.target` eleven lines later. Playback reads local files only and a wildcard listener binds without a route, so both lines can go.
- `deploy/channel3.service:33-34` `Restart=always` with `RestartSec=1` meets the default start limit of five starts in ten seconds, so a failed mpv launch parks the appliance in the failed state until someone opens a laptop. `docs/integration/guide-api.md:129` explicitly handed that decision to this phase. `StartLimitIntervalSec=0` in the `[Unit]` section, or a longer `RestartSec`, keeps an unattended box trying.
- `deploy/channel3.service:25` `Environment=XDG_RUNTIME_DIR=%t` is what makes `player.RuntimeDir()` at `internal/player/mpv.go:140` land in the `RuntimeDirectory` at `/run/channel3`, and the comment says so, but the agreement is implicit and depends on the Go constant `runtimeDirName` staying `"channel3"`. It also points `XDG_RUNTIME_DIR` at a directory the service user does not own, so anything else that honours the variable and writes directly into it is refused.
- `deploy/channel3.service:16` The `video` and `render` groups cover `/dev/dri`, and `input` covers evdev, which is the right list. What a system service has no equivalent for is a seat, and mpv's DRM context also wants a VT. If Phase 4 finds mpv can get DRM master as root but not as `channel3`, try `tty` in `SupplementaryGroups` before changing anything else, and run the Phase 4 mpv trials as `sudo -u channel3` so the test matches the service.
- `deploy/channel3.service:37` `TimeoutStopSec=10` less the 5 s HTTP shutdown budget at `cmd/channel3/serve.go:43` leaves about 5 s for mpv to quit cleanly. Probably enough, worth 20 for the margin, and worth timing once on the Pi.
- `deploy/channel3.service:8` `RequiresMountsFor=/srv/channel3` means an absent SSD is no service at all rather than a Stand By card on screen. That reads deliberate, and the comment says so, but it is the difference between a television that explains itself and one that is simply black. Confirm which is wanted with the hardware in front of you.
- `deploy/pi-setup.sh:179` `mount -a` runs bare under `set -euo pipefail`, so any unrelated failing fstab entry aborts the script before the mountpoint check on the next line and before the summary. `mount -a || note_warn ...` keeps the diagnosis the script already writes.
- `deploy/pi-setup.sh:187` When the mount did not come up, the loop still creates and chowns `channels`, `library` and `local` on the microSD, and a later successful mount hides them along with anything written into them. Skip the directory step, or stop outright, when `mountpoint -q` failed at line 180.
- `deploy/pi-setup.sh:105` The PARTUUID is checked for existence but not for filesystem type, while the fstab line at 174 hardcodes `ext4`. One `blkid -s TYPE -o value` catches a wrong partition now instead of at the next reboot.
- `deploy/pi-setup.sh:136` yt-dlp is fetched over the network and installed into `/usr/local/bin` on the strength of a `--version` match. The project publishes `SHA2-256SUMS` beside the binary; verifying it costs two lines and this is the one thing in the phase that runs arbitrary downloaded code as root.
- `deploy/pi-setup.sh:205` A re-run that installs a changed unit reaches `daemon-reload` at 219 but never restarts a running service, so a box quietly keeps the old unit until someone reboots. `systemctl try-restart channel3`, or a line in the summary saying a restart is needed, closes that.
- `deploy/pi-setup.sh:269` Disabling `getty@tty1` is what the plan asks for and it is right for the television, but it also removes the only local console. The README should say in one line that recovery is ssh or the microSD in another machine.
- `deploy/ingest.sh:46` Any non-zero exit while the unit is active prints the stop, ingest, start recipe, so exit 2, which `cmd/channel3/ingest.go:48` uses for "some items failed", is reported as though the broadcast guard tripped. Check `systemctl is-active` before the ingest and say plainly that the guard will refuse, and treat exit 2 as a real ingest result.
- `Makefile:84` `$(ARGS)` is unquoted, so every documented flag works and any argument containing a space silently splits. Fine as documented, a trap for anything else.
- `CLAUDE.md:21` still describes `make lint` as gofmt and `go vet`. It now also runs `bash -n` and shellcheck, so a Mac without shellcheck fails lint, and neither `CLAUDE.md` nor `deploy/README.md` says to install it.
- `deploy/README.md:3` says "Five files live here" and lists five, not counting itself. Harmless, and worth a word when the sixth is added.

### Suggestions

- `deploy/deploy.sh:104` prints `git describe --tags --always --dirty` (`Makefile:9`), so today's uncommitted tree would deploy as a `-dirty` id. That is the honest answer and no change is needed, only the awareness that it is not a release tag.
- The unit has no sandboxing directives. `ProtectSystem=full` and `ProtectHome=yes` are probably safe here, but they interact with DRM, evdev and CEC access, so they belong after Phase 4 proves the plain unit works, not before.

## Re-verification

Second pass after the executor's remediation round, still with no hardware. `make lint` exit 0, `bash -n` clean on all three scripts, `shellcheck deploy/*.sh` exit 0 with the same three justified `SC2029` disables, `make test` exit 0. Plan tasks 1 to 4 remain ticked, 5 to 8 remain unticked with their notes, and no Verification checklist item is ticked, which is still correct for a draft.

Both FAIL items are resolved.

- `deploy/channel3.env.example:7` now says in as many words that the candidates are bare flag text, that they must not be uncommented, and that the flags are copied into the quoted value of the assignment. A filled in example sits at `:84`, still commented, directly above the live empty assignment at `:86`. Resolved.
- `deploy/channel3.env.example:68` is now `--start-channel=trains`, with `:65` saying the value is the `id` field from the channel YAML and not the number typed on the remote. That matches `cmd/channel3/serve.go:65` and the shipped `channels/example.yaml`. Resolved.

Fixed warnings, verified in the new files.

- `deploy/channel3.service:8` The `network-online.target` lines are gone and the comment now explains both omissions together.
- `deploy/channel3.service:16` `StartLimitIntervalSec=0` is in the `[Unit]` section, which is the correct section, so the appliance keeps retrying instead of parking in the failed state. `deploy/deploy.sh:97` and `deploy/README.md:126` were both updated to match, and the stale `reset-failed` advice is gone.
- `deploy/channel3.service:47` `TimeoutStopSec=20` leaves real margin over the 5 s HTTP shutdown budget.
- `deploy/pi-setup.sh:220` `mount -a` no longer aborts the run, and the failure is recorded as a warning.
- `deploy/pi-setup.sh:225` The script now stops before the directory loop when `/srv/channel3` is not a mount point, and `die` at `:74` prints the summary on the way out, so an abort still says what was done.
- `deploy/pi-setup.sh:131` The partition must report `TYPE=ext4` before the fstab line is written, with the device resolved from the PARTUUID at `:125`.
- `deploy/pi-setup.sh:169` The yt-dlp download is checked against the release `SHA2-256SUMS` before `chmod`, before it is ever executed and before it is installed, and the version check at `:178` is kept as a second gate. The `awk` field match is right for that file's two column format.
- `deploy/pi-setup.sh:277` `systemctl try-restart channel3` runs when the unit file actually changed, which is a no-op on a first run and picks up the new unit on a box that was already broadcasting.
- `deploy/ingest.sh` is rewritten and does what was asked. It checks `is-active` at `:64` and separates an unreachable host at `:67`, warns that the television goes dark at `:73`, stops the service at `:80`, and restarts it through an `EXIT` trap at `:59` that a Ctrl-C also reaches by way of the `INT` trap at `:60`. The guard against a double start is the `service_stopped` flag reset inside `start_service`, so the explicit call at `:93` and the trap cannot both act. Exit 2 is reported as failed items at `:99`, separately from a real failure at `:103`, and the ingest's status is propagated at `:108`.
- `deploy/README.md:9` records that disabling `getty@tty1` leaves ssh or the microSD as the only way in, `:7` says to install shellcheck, `:3` says six files, and `:85` describes the new stop, ingest, start behaviour with the dark television called out in bold.
- `CLAUDE.md:21` now describes `make lint` accurately and names Homebrew for shellcheck.
- `docs/plans/plan.md` assumption 15 carries the ingest downtime and the rsync alternative.

Warnings that remain, all accepted rather than unaddressed.

- `deploy/channel3.service:35` The `XDG_RUNTIME_DIR=%t` and `RuntimeDirectory=channel3` pairing still depends on `runtimeDirName` in `internal/player/mpv.go:23` staying `"channel3"`, and still points the variable at a directory the service user does not own. Documented in the unit and in `deploy/README.md:142`, which is the reasonable answer for now.
- `deploy/channel3.service:21` The DRM master and VT question is now written into the unit as the first thing to try, with the instruction to run the Phase 4 trials as `sudo -u channel3`. Hardware still has to answer it.
- `deploy/channel3.service:4` An absent SSD is still no service at all rather than a Stand By card. Deliberate, and worth one look with the hardware present.
- `Makefile:83` `$(ARGS)` is still unquoted, so an argument containing a space would split. Fine for every documented flag.

New, introduced by this round, none of them blocking.

- `deploy/ingest.sh:80` stops the broadcast with no confirmation and no grace period. It announces what it is about to do and then does it, which is a sharper edge than the old refuse-and-explain behaviour, and it is the one command in the kit that takes the television down while someone may be watching. A three second pause, or requiring `CHANNEL3_YES=1` for the stop, would match the project's own never-while-the-kids-are-watching rule.
- `deploy/ingest.sh:53` and `:80` run `sudo` over ssh without a tty, so the whole flow assumes the login user has passwordless sudo, which the Raspberry Pi Imager default gives but a hardened box may not. `deploy/deploy.sh:75` uses `ssh -t` for its sudo, so the two differ. Worth one line in the README, or `-t` here as well.
- `deploy/README.md:95` documents an rsync route with `--rsync-path="sudo -u channel3 rsync"`, which needs rsync present on the Pi and the same passwordless sudo. `deploy/pi-setup.sh:27` does not install rsync, so adding it to `APT_PACKAGES` would make the documented no-downtime path true on a fresh box.

## Verdict

PASS WITH WARNINGS
