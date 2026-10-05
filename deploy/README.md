# Deploying Channel Three to the Pi

Six files live here, this README and five that do the work. `channel3.service` is the systemd unit. `channel3.env.example` is the flag file it reads, installed once as `/etc/default/channel3` and edited by hand after that. `pi-setup.sh` turns a fresh Raspberry Pi OS Lite install into a Channel Three box and is safe to re-run. `deploy.sh` builds the arm64 binary on the Mac and swaps it in on the Pi. `ingest.sh` runs an ingest on the Pi over ssh.

Everything here is drafted but unrun. There is no Pi yet, so nothing below has been executed against real hardware, and the Phase 4 hardware checklist has to happen before the service can start successfully.

`make lint` runs `shellcheck` over these scripts, so the Mac needs it: `brew install shellcheck`.

Once `pi-setup.sh` has disabled `getty@tty1` there is no login prompt on the television and no keyboard path into the Pi. Recovery is ssh, or pulling the microSD and editing it in another machine. Keep ssh working, and if you are about to change the network or the ssh config, do it while you still have a second way in.

## 1. Flash the card

Use Raspberry Pi Imager with Raspberry Pi OS Lite, 64-bit, Trixie. In the Imager's settings, set the hostname to `channel3`, enable ssh with your public key, set the username, set the locale and time zone, and configure Wi-Fi if the Pi is not on Ethernet. Ethernet is better: the deploy copies a 20 MB binary and the ingest pulls video.

First boot takes a minute. Check it with `ssh channel3.local`.

## 2. Format the library SSD, once

The setup script never formats a disk, on purpose. Do it by hand the first time, with the SSD plugged into the Pi.

```
lsblk -o NAME,SIZE,TYPE,MOUNTPOINT
sudo mkfs.ext4 -L channel3 /dev/sda1
```

Check the device name twice before running that. `/dev/sda1` on a Pi with one USB disk is almost always right, and the microSD is `/dev/mmcblk0`, but a second USB device changes the order.

## 3. Find the PARTUUID

The fstab line is written by PARTUUID so the library never lands on the wrong disk when USB enumeration changes.

```
lsblk -o NAME,SIZE,LABEL,PARTUUID
```

## 4. Run the setup

From the Mac, in the repo root.

```
make pi-setup PI_HOST=channel3.local PI_DISK=/dev/sda1
make pi-setup PI_HOST=channel3.local PI_DISK=PARTUUID=1a2b3c4d-01 PI_TZ=America/Toronto
```

`PI_DISK` takes either form. `PI_TZ` defaults to `America/Toronto`.

The target copies this directory to the Pi and runs `pi-setup.sh` under sudo. It installs mpv, v4l-utils, ffmpeg, rsync and unzip, installs the pinned yt-dlp standalone binary and the pinned Deno that yt-dlp needs for YouTube, creates the `channel3` system user in the `video`, `render`, `input` and `audio` groups, writes the fstab line and mounts the SSD, creates `/srv/channel3/{channels,library,local}`, installs the unit and the flag file, enables the service at boot, adds the quiet boot parameters to `cmdline.txt`, disables the tty1 login prompt and sets the time zone. It prints what it changed and what was already in place, and re-running it changes nothing.

It deliberately does not start the service. The mpv flags are unknown until Phase 4 runs.

## 5. Set the flags after Phase 4

Phase 4 finds which mpv output flags drive this television, which audio device carries HDMI sound, what the Flirc device is called and whether the television answers CEC. All of that goes in one place.

```
sudo nano /etc/default/channel3
sudo systemctl restart channel3
```

`channel3.env.example` lists every candidate as a comment, with what each one is for. The candidates are bare flag text, so do not uncomment them. Copy the ones that worked into the quoted value of the single `CHANNEL3_FLAGS=` assignment at the bottom of the file, which is the only line systemd reads.

The unit itself never changes, so a later `make pi-setup` reinstalls the unit without touching your flags. It also runs `systemctl try-restart channel3` when the unit changed, so a box that was broadcasting picks the new one up.

## 6. Deploy a new build

```
make deploy PI_HOST=channel3.local
```

That builds `bin/channel3-linux-arm64` on the Mac, copies it to the Pi, keeps the old binary as `/usr/local/bin/channel3.prev`, moves the new one into place, restarts the service, follows the journal for ten seconds and fails if the unit is not active afterwards. It prints the deployed version at the end.

The build needs Node and npm on the Mac, because the web interface is built by Vite and embedded in the binary. The Pi never needs Node.

**Never deploy while an ingest is running.** The restart kills it mid download. `deploy.sh` checks for one and refuses, before it even builds.

**Never deploy while the kids are watching.** The restart kills mpv and the television goes to the Stand By card for a few seconds.

## 7. Ingest

```
make pi-ingest PI_HOST=channel3.local
make pi-ingest PI_HOST=channel3.local ARGS="--channel saturday-morning --dry-run"
```

Channel configs are edited on the Mac in `~/srv/channel3/channels/`, one YAML file per channel. `make pi-ingest` first mirrors that directory's `.yaml` files onto the Pi, so the Mac copy is the master: a channel file deleted on the Mac is deleted on the Pi too, while its downloaded videos stay on the drive. Set `CHANNEL3_CHANNELS` to sync a different directory. `settings.yaml` beside that directory (`~/srv/channel3/settings.yaml`) is mirrored to `/srv/channel3/settings.yaml` the same way. It holds the on-screen settings, such as the channel number and the bumpers, and `settings.example.yaml` in the repo documents every field. The service reads it at startup, so a change needs a restart, which `make pi-ingest` does anyway. If the directory is missing or holds no `.yaml` files, the script stops before syncing anything.

Ingest refuses to run while the service is broadcasting on the same machine, which on the Pi is always. So `make pi-ingest` does the whole operation: it syncs the channel configs, stops the service, runs the ingest, and starts it again afterwards, including when the ingest fails or you interrupt it.

**The television is dark for the whole ingest.** A large channel takes minutes. Do not run this while the kids are watching. The script says so and waits three seconds before it stops the service, so a command typed in the middle of a programme can still be interrupted with Ctrl-C. Set `CHANNEL3_YES=1` to skip that pause, which is what a script wants.

Starting the service again rescans the library, so the new items are on air immediately. Left alone, the library is rescanned at the 04:00 rollover and new items appear tomorrow.

The no-downtime route is to ingest on the Mac into `~/srv/channel3` and rsync the library to the Pi, which never touches the running service.

```
bin/channel3 ingest --root ~/srv/channel3 --channel saturday-morning
rsync -av --rsync-path="sudo -u channel3 rsync" ~/srv/channel3/library/ channel3.local:/srv/channel3/library/
```

The `--rsync-path` is what makes the copied files land owned by `channel3` rather than by your ssh user. Without it the service can still read them, but ingest cannot later replace them.

Video you put under `<root>/local/` yourself and reference by a relative path such as `local/steam-engines.mp4` travels the same way: rsync `~/srv/channel3/local/` alongside the library and the sidecars resolve against `/srv/channel3` on the Pi, so there is nothing to ingest again there.

## 8. Logs

```
ssh channel3.local journalctl -u channel3 -f
ssh channel3.local journalctl -u channel3 --since today
ssh channel3.local systemctl status channel3
```

The interesting lines are the Stand By load at startup, the schedule load that follows it, the 04:00 rescan, and any item the player excluded because mpv could not open it.

## 9. Rolling back

```
ssh channel3.local "sudo sh -c 'cp -p /usr/local/bin/channel3.prev /usr/local/bin/channel3 && systemctl restart channel3'"
```

Only the one previous binary is kept. A second deploy overwrites it.

## 10. Enabling Anynet+ on the television

The Samsung UN40H4005AF supports HDMI-CEC under the name Anynet+ and ships with it off. Turn it on under Menu, System, Anynet+ (HDMI-CEC), before testing `--cec`. If the television still ignores `cec-ctl`, leave `--cec` out of the flag file and let the universal remote handle power and volume. That fallback was decided up front and costs nothing.

## 11. If mpv cannot start at all

`serve` opens the HTTP listener before it launches mpv, but if mpv cannot be launched at all, `serve` exits non-zero. With `Restart=always` and `RestartSec=1` systemd restarts it immediately, and the result is a crash loop with no guide and no video.

The unit sets `StartLimitIntervalSec=0`, so systemd never gives up and never parks the unit in the failed state. That is the right trade for a box with nobody in front of it, but it means a crash loop is quiet: the television just stays dark. The journal is where it shows, as the same few lines repeating once a second.

```
ssh channel3.local journalctl -u channel3 --since -5min
ssh channel3.local systemctl status channel3
```

Finding mpv flags that work on this television is exactly what Phase 4 is for, and it is why the service is not started by the setup script. To try flags by hand before committing them to the flag file, stop the service and run the binary in the foreground.

```
ssh channel3.local sudo systemctl stop channel3
ssh -t channel3.local sudo -u channel3 /usr/local/bin/channel3 serve --root /srv/channel3 --listen :8080 --mpv-arg=--vo=gpu --mpv-arg=--gpu-context=drm --mpv-arg=--gpu-api=opengl
```

## 12. What the unit assumes

The binary is at `/usr/local/bin/channel3`. The root is `/srv/channel3` and is a mount point, so the unit will not start without the SSD. The service runs as `channel3` in the `video`, `render`, `input` and `audio` groups, with `CAP_NET_BIND_SERVICE` so it can listen on port 80. The mpv socket and the unpacked Stand By card live in `/run/channel3`, which needs both `RuntimeDirectory=channel3` and `XDG_RUNTIME_DIR=/run` in the unit, because the binary builds that path from `XDG_RUNTIME_DIR` and systemd does not set it for system services. `HOME` is `/srv/channel3` so mpv looks for its config somewhere sane.

The unit waits for `local-fs.target` and nothing else. It does not wait for `time-sync.target`, because the schedule is a function of the clock and corrects itself when NTP lands. It does not wait for `network-online.target` either, because playback is local files only and the listener binds without a route, while `NetworkManager-wait-online` can hold boot for many seconds on Wi-Fi. Both would cost boot-to-video for nothing.
