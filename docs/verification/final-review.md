# Final review: Channel Three MVP on the Mac

Reviewer: the orchestrating lead session, after Phases 1, 2, 3, 5, 6, 7 and 8 were executed and verified by separate Opus agents, Phase 9 was drafted and verified the same way, and a final fix pass addressed the lead's own findings. Date: 2026-09-23. Scope: everything that can be verified without the Raspberry Pi. Phase 4 and the hands-on half of Phase 9 stay open until the hardware arrives.

## Verdict

PASS WITH WARNINGS for the Mac-side MVP. Every CLAUDE.md rule holds, every phase report is PASS or PASS WITH WARNINGS after its fix round, the lead's five findings are fixed and committed, and the final binary passes a live end-to-end smoke. The warnings are the hardware-gated tasks, which cannot be closed here, plus a short list of accepted design edges recorded below.

## Rules audit

| Rule | Result | Evidence |
|---|---|---|
| Schedule is a pure function, nothing persisted | holds | `internal/schedule` imports only `hash/fnv`, `math/rand/v2`, `time`; golden order test; `At` boundary math reviewed by hand: exactly on a cumulative boundary the next item starts at offset zero |
| Ingest is the only code that touches the network | holds | `net/http` appears only in `internal/api` and the listener in `serve.go`; `internal/library` uses `net/url` for parsing; `os/exec` is confined to yt-dlp, ffprobe, the mpv launcher and cec-ctl |
| Content stays out of the repo | holds | `channels/example.yaml` is the only tracked YAML; the only tracked media is a one second test fixture |
| Broadcast mode has no controls | holds | the API answers GET and HEAD only and returns 405 otherwise; no endpoint touches the player; the only control is channel change from a key source |
| Tests alongside code | holds | 224 Go test functions across seven packages plus 40 Vitest tests; the schedule is table tested, the player runs against a fake mpv socket, the station against a fake clock |
| No playback state on disk | holds | the only writes outside ingest are the pid guard and the Stand By card extraction into the runtime directory |
| Dev port 3333, never 3000 | holds | `defaultListen` is `:3333`; no 3000 anywhere in code or config |
| One static arm64 binary, no cgo | holds | `make build-arm64` yields a statically linked aarch64 ELF with the web UI embedded |

## Findings from the lead's read, all fixed in the final fix pass

1. Child processes outlived a timeout. `exec.CommandContext` killed yt-dlp but not the ffmpeg it spawned, and `Run` blocked until the child released the pipes. The timeout test showed it by taking 30 seconds in about one run in three. Fixed with `internal/proc.Harden`: process group, `SIGKILL` to the group on cancel, 2 second `WaitDelay`. Applied to yt-dlp, ffprobe and cec-ctl. The mpv launcher was left alone because it hands mpv no pipes and the supervisor's kill path must not change. The test now runs in half a second every time.
2. Zero configured channels exited `serve`. The brainstorm's error handling asks for Stand By. The station now starts empty, shows the card, answers the API with an empty list and `tuned` null, and adopts channels at the first rollover that finds some.
3. A relative sidecar `file` was joined to the channel directory with no containment check. A relative path must now resolve inside the channel directory or the item is excluded with a logged reason. Absolute paths from `file://` sources stay allowed.
4. A replaced local clip kept its stale duration. Ok sidecars now record the file size; ingest re-probes a local item or re-downloads a remote one whose size changed. Older sidecars without a size behave as before.
5. `/api/now` and `/api/guide` ignored the station's exclusions, so after a load error the guide kept naming an item that was not on screen. The API now reads `PlayableChannels()`, the exclusion set is written under the station mutex, and a race test covers a concurrent read during an exclusion. The residual is that an item excluded after the page fetched shows until the next 30 second refresh.

## Accepted edges, recorded rather than fixed

- A tune a few milliseconds before an item boundary can produce one extra load, because mpv reaches the end of the file marginally before the schedule's boundary. Self-correcting and invisible on screen.
- The systemd unit's `XDG_RUNTIME_DIR=%t` pairing with `RuntimeDirectory=channel3` depends on the Go constant `runtimeDirName` staying `channel3`. Documented in the unit and the deploy README.
- `RequiresMountsFor=/srv/channel3` means an absent SSD is no service rather than a Stand By card. Deliberate; worth one look with the hardware present.
- If mpv cannot launch at all, `serve` exits and systemd restarts it without a start limit. The guide never loads in that state; the journal is the diagnostic. Phase 4 exists to find flags that make this impossible.
- `make build` and `make build-arm64` need Node on the build machine because they embed the web UI. `go build ./...` does not. The Pi never needs Node.
- The ingest pid guard refuses to run beside a live `serve`, so `make pi-ingest` stops the service, ingests, and starts it again, darkening the television for the duration. Mac-side ingest plus rsync is the no-downtime route.
- `Makefile` passes `$(ARGS)` to `pi-ingest` unquoted, so an argument containing a space would split. Fine for every documented flag.

## End-to-end smoke on the Mac, final binary, mpv muted

- `bin/channel3 serve --root ~/srv/channel3 --start-channel clips --no-input` with the two throwaway channels: `/api/now` reports `tuned` `clips` with `now` and `next` for both channels; `/api/guide?hours=1` lists the channel; `/` returns the embedded page with status 200; `POST /api/now` returns 405.
- `pkill -x mpv` while playing: one restart event, mpv reconnected within a second, the station reloaded the current item at the schedule's offset, and `/api/now` still reported the tuned channel.
- SIGINT: `serve` exited in two seconds, the pid file was removed, no mpv left running.
- An empty root with no channels: `serve` stayed up, logged "no channels are configured, showing stand by", `/api/channels` returned an empty list and `/api/now` returned `tuned` null.
- Earlier in the run, on Phase 6: channel up, channel down, arrows and digit entry were driven through a pseudo-terminal and every tune matched `channel3 guide` for that minute; a number matching no channel left playback alone.
- Earlier in the run, on Phase 8: the guide page was rendered headless at 320 to 900 pixel viewports with no horizontal overflow, and screenshots at 400 and 1280 pixels showed the tuned highlight, progress bars, next-up lines and the Stand By row for an empty channel.

## Phase 9 draft

The unit, env example, setup, deploy and ingest scripts, and the deploy README were written and linted on the Mac and reviewed twice. The first review returned FAIL on two env example mistakes: candidate flags written as bare lines that systemd would ignore, and a channel number where the `--start-channel` flag takes an id. Both would have put a black screen or a crash loop in front of the operator on hardware day. The remediation round fixed both plus fifteen warnings, including dropping the wait on `network-online.target`, lifting the start limit, checksum-verifying the yt-dlp download, and rewriting `ingest.sh` to stop and restart the service around the run. Re-verification returned PASS WITH WARNINGS.

Nothing in the unit assumes a working mpv output flag or CEC. Every hardware-dependent flag is set in `/etc/default/channel3` after Phase 4 and the unit never changes.

## Hands-on checklist for the day the hardware arrives

1. Flash Raspberry Pi OS Lite 64-bit with Raspberry Pi Imager: hostname `channel3`, your ssh key, time zone, Wi-Fi or Ethernet. Fit the RTC coin cell. Fit the case.
2. Enable Anynet+ on the Samsung UN40H4005AF under Menu, System, Anynet+ (HDMI-CEC).
3. Run the whole Phase 4 checklist in `docs/plans/plan.md`, in order: mpv output flags (run the trials as `sudo -u channel3` once the user exists, so the test matches the service), audio, the service flag set, TV power cycle behaviour, the Flirc key table with `evtest`, `cec-ctl` against the television, boot time, console bleed. Write `docs/hardware.md`.
4. Plug in the USB SSD, `lsblk`, `mkfs.ext4 -L channel3` on its partition by hand, note the `PARTUUID`.
5. `make pi-setup PI_HOST=channel3.local PI_DISK=/dev/sda1`, then run it again and confirm it reports nothing changed.
6. Copy the Phase 4 flags into `CHANNEL3_FLAGS` in `/etc/default/channel3`. Run `systemd-analyze verify /etc/systemd/system/channel3.service` on the Pi.
7. `make deploy PI_HOST=channel3.local`. Confirm the unit is active and the journal shows the Stand By load followed by a schedule load.
8. Phase 9 task 5: copy a real channel config to `/srv/channel3/channels/` and `make pi-ingest` for a small channel. The television goes dark during the ingest.
9. Phase 9 task 6: power cycle with the television on. Stopwatch to video, target under 30 seconds. Confirm no boot text, no cursor, no login prompt.
10. Phase 9 task 7: close out Phase 5 task 9 and Phase 6 tasks 7 and 8: `pkill mpv` recovery on the Pi, Flirc channel up, down and digits from the couch, CEC power and volume if the television answered in step 3. Tick them in the plan and update `docs/hardware.md`.
11. Phase 9 task 8: leave it running overnight, then read `journalctl -u channel3` for restarts, exclusions and the 04:00 rescan line.
12. Work the Phase 9 verification checklist, including `curl http://channel3.local/api/now` from the Mac and a refused `make deploy` during an ingest.

## How this was built

Nine phases, each executed by a fresh Opus agent and verified by a separate fresh Opus agent, committed by the lead on PASS, with one remediation round allowed per phase. Every phase passed with warnings on first review except the Phase 9 draft, which failed once and passed on re-verification. Warnings that were concrete and local were fixed before each commit rather than carried. Phase reports live in this directory, one per phase.

Two process failures cost time and neither touched the code: one reviewer hung for two hours producing nothing and was replaced, and several agents' completion messages never reached the lead, which idled overnight once waiting for one. The fix was to treat files as the signal, never messages: every agent writes its report to a file, and a timed watchdog wakes the lead to read it.
