# Channel Three hardware results

What Phase 4 found on the real Pi and TV. Every flag in `/etc/default/channel3` should trace back to a line here. Started 2026-10-04 with the Pi bare on Wi-Fi. Since 2026-10-05 it is in the case with the fan, the hard drive and a stand-in 1080p monitor. No RTC battery, Flirc or target TV yet. Sections marked **not tested yet** wait for that hardware.

## Pi and OS

- Raspberry Pi 5, 8 GB, booted from the SanDisk 64 GB microSD.
- Raspberry Pi OS Lite 64-bit, Debian 13 (Trixie), kernel `6.18.50+rpt-rpi-2712`, flashed with Raspberry Pi Imager v2.0.11.1.
- Hostname `channel3`, reachable as `channel3.local` over Wi-Fi at 192.168.2.137. Ethernet not connected.
- Time zone `America/Toronto`, clock synced over NTP.
- Imager 2 leaves `sudo` asking for a password, which breaks the deploy scripts' non-interactive sudo. Fixed with `/etc/sudoers.d/010-tyler-nopasswd`; `docs/setup.md` step 4 now includes it.

## mpv

- `mpv v0.40.0` from apt.
- Output flags: **`--vo=gpu --gpu-context=drm --gpu-api=opengl --hwdec=no`** (Try A, the first candidate). Tested 2026-10-05 on a stand-in Samsung 1080p monitor on HDMI 0 (`card1-HDMI-A-1`): full screen at 1920x1080@60, 0 dropped frames over 30 s of 1080p H.264, about 40% of one core, 50 °C with the fan at about 1,650 RPM. Tries B and C were not needed. Recheck on the Samsung UN40H4005AF, which is a 720p panel.
- Service flag set (step 12): `--no-osc --no-osd-bar --no-input-default-bindings --idle=yes --hr-seek=yes --image-display-duration=inf --input-ipc-server=...` work with the output flags. The IPC socket answered `get_property mpv-version`, and a `loadfile ... start=45` landed at 45.000 s in 154 ms.
- Live in `/etc/default/channel3` since 2026-10-05: `CHANNEL3_FLAGS="--mpv-arg=--vo=gpu --mpv-arg=--gpu-context=drm --mpv-arg=--gpu-api=opengl --mpv-arg=--hwdec=no"`.

## Playback

Software decode measured headless with `mpv --no-config --vo=null --aid=no --untimed --hwdec=no`, which decodes as fast as the CPU allows. Pi bare, no fan.

| File | Decode time for 60 s of video | Speed |
|---|---|---|
| 1920x1080 H.264 High, 29.97 fps, 3.8 Mbit/s | 6.0 s | 10.0x real time |
| Same file, one decoder thread | 14.7 s | 4.1x real time |
| 1280x720 H.264 | 2.6 s | 23.2x real time |

Peak temperature 54 °C, `get_throttled=0x0`. Software H.264 decode at 1080p has ample headroom, so ingest stays capped at 1080p. The GPU output path dropped no frames either (see mpv above).

## Service under systemd

Deployed 2026-10-05 with `make deploy` (version `9d4cfba`), with the throwaway `smoke` channel copied to `/srv/channel3`. The unit came up active, connected to mpv over `/run/channel3`, and loaded the scheduled item at its offset. `/api/now` and the guide page answered from the Mac on port 80.

## Service smoke, headless

`channel3 serve --root ~/c3test/root --listen :8080 --no-input --mpv-arg=--vo=null --mpv-arg=--ao=null`, run as `tyler` against a four-item throwaway channel with root-relative `local/` sources:

- Connected to mpv and loaded the scheduled item at the right offset within a second of start.
- `/api/now` answered from the Mac with the tuned channel, now and next. `/` returned 200. `POST /api/now` returned 405.
- `pkill -x mpv`: restarted after 500 ms, reconnected and reloaded the current item at the schedule's offset, about one second in total.
- Item boundary at 15:28:08 loaded the next item at offset 53 ms.
- SIGTERM: exited cleanly, no mpv left, `serve.pid` removed.

This covers Phase 5 task 9 except boot-to-video, which needs HDMI and the service under systemd.

## Audio

**Not tested yet** (the stand-in monitor has no speakers). Under systemd, mpv logs `pw.conf: can't load config client.conf` twice at each start: it tries PipeWire, and the `channel3` system user has no PipeWire session. Expect to need `--mpv-arg=--ao=alsa` plus an `--audio-device` on the Samsung.

## TV power cycle

**Not tested yet** (needs HDMI).

## Flirc key table

Flirc 2.0 (firmware v4.10.7) programmed 2026-10-05 on the Mac with `flirc_util` from the Flirc app 3.27.19 (`brew install --cask flirc`), against the Flipper Big Button remote straight out of the box, with no brand code set. Whichever brand the remote emits by default, the Samsung must be checked to ignore it. Backup: `~/srv/channel3/flirc-flipper.fcfg`, restore with `flirc_util loadconfig`.

On the Pi it appears as `flirc.tv flirc Keyboard`, `/dev/input/by-id/usb-flirc.tv_flirc_DE80B887503243584C2E3120FF04273C-if01-event-kbd`. Codes read with `evtest`:

| Remote button | Recorded as | Pi receives | Service action |
|---|---|---|---|
| Channel up | `record_api 0 75` (keyboard Page Up) | `KEY_PAGEUP` | channel up |
| Channel down | `record_api 0 78` (keyboard Page Down) | `KEY_PAGEDOWN` | channel down |
| 1 to 9, 0 | `record_api 0 30`..`39` (number row) | `KEY_1`..`KEY_0` | digits |
| Volume up | `record vol_up` (consumer page) | `KEY_VOLUMEUP` | CEC volume up |
| Volume down | `record vol_down` | `KEY_VOLUMEDOWN` | CEC volume down |
| Mute | `record mute` | `KEY_MUTE` | CEC mute |
| On/Off | `record wake` | **nothing** | none yet |

- Keyboard-page power, volume and mute (`record_api 0 102/128/129/127`) record without complaint but never reach the Pi. Use the consumer-page `record vol_up` and its siblings instead.
- `record wake` is a USB remote-wakeup signal, not a key, so a running Pi sees nothing. **Open:** On/Off has no working key. If CEC works, re-record it as a spare keyboard key (for example F12) and alias that key to Power in `internal/input/evdev.go`. If CEC does not work, On/Off talks straight to the TV and needs no mapping.
- With no Flirc plugged in, `serve` falls back to `vc4-hdmi-0`, the kernel's HDMI-CEC remote passthrough device. With the Flirc plugged in before the service starts, autodetection picks it by name. A Flirc plugged in later is not noticed until the service restarts.

Tuning from the remote (Phase 6 task 7), with test channels 3, 5 and 12 on the stand-in monitor: channel up 3→5→12→3 wraps, channel down 3→12, `5` tunes 5, `1` `2` tunes 12, `9` logs "no channel has that number, staying put", `3` tunes 3. Every tune loaded within the same second as the press. No key echo was reported on the screen.

## CEC

**Not tested yet** (needs HDMI and Anynet+ on).

## RTC battery

Not plugged in yet. `/sys/class/rtc/rtc0/battery_voltage` reads 0.

## Hard drive

- Seagate Portable 2 TB on a blue USB 3 port, bus powered from the 27 W supply with no undervoltage (`get_throttled=0x0`). Shipped as NTFS with only Seagate's setup files.
- Formatted 2026-10-05: `mkfs.ext4 -L channel3 /dev/sda1`, PARTUUID `017a415f-01`. The MBR type byte is still 0x07, which Linux ignores.
- `hdparm -S 0` is **refused** by the USB bridge (`SG_IO: bad/missing sense data`, illegal request). The spin-down timer cannot be set. Broadcast reads keep the drive busy. Watch for a spun-down stall after a long Stand By stretch.
- `make pi-setup PI_DISK=PARTUUID=017a415f-01`: the first run made 25 changes with 0 warnings, the second made 0 changes. Mounted at `/srv/channel3` with `rw,noatime`.

## Boot time

**Not tested yet.**

## Console

**Not tested yet** (needs HDMI).

## Open issues

None so far.
