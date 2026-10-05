---
waypoint: 1
title: "Channel Three: Pi setup, start to finish"
project: channel3
---

# Channel Three: Pi setup, start to finish

This is the one guide for turning the parts on the table into a working Channel Three box. Do the steps in order. Each one ends with a **Done when** line, so you know it worked before you move on.

Commands marked **Mac** run in the repo root on the Mac (`~/prj/channel3`). Commands marked **Pi** run in an ssh session on the Pi (`ssh channel3.local`).

As you go, jot down what you find in Part 3 (flags that worked, key codes, CEC yes or no, boot time). Those notes become `docs/hardware.md` at the end.

## Where we are (2026-10-04)

- **Done:** steps 1 and 4. The card is flashed, the Pi is on Wi-Fi at `channel3.local`, passwordless sudo is set, the system is updated, and mpv, ffmpeg, v4l-utils, rsync, evtest, socat and hdparm are installed (step 8's tools too). Headless tests passed: 1080p decodes at 10x real time, and the service runs, recovers from an mpv kill and changes items on time. Results are in `docs/hardware.md`.
- **Not done:** the Pi ran bare. No case, fan, battery, drive or HDMI yet.
- **Next:** shut the Pi down (`ssh channel3.local sudo poweroff`), do **step 2** (assembly, with the drive and HDMI plugged in) and **step 3** (Anynet+ on). Power it up and tell Claude. Claude then checks the battery and fan (end of step 4) and runs **steps 5 to 7** (drive and setup script). After that come the screen trials in **steps 9 to 17**, with you watching the TV. Program the Flirc on the Mac (step 14) any time before step 15.

<!-- wp:question id="q1" type="choice" select="single" -->
**Q1 — Network for the Pi:** The Pi needs a network to deploy and to ingest video. Ethernet is faster and more reliable for copying a 20 MB binary and downloading video. Wi-Fi works and is set in the Imager in step 1. Which will the Pi use?
- [ ] Ethernet
- [ ] Wi-Fi
- [ ] Wi-Fi now, Ethernet later
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q2" type="choice" select="single" -->
**Q2 — Order of setup vs. hardware trials:** The older checklist runs the mpv and remote trials (Part 3) before the setup script. This guide runs the setup script first, because it creates the `channel3` user, mounts the hard drive and installs mpv, so the trials then run exactly as the service will. The service still does not start until the flags are known. Keep this order?
- [x] Yes, setup script first, then trials (this guide's order)
- [ ] No, trials first on a bare Pi, then the setup script
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

## What you need

- Raspberry Pi 5 (8 GB), official case with fan, official 27 W power supply
- SanDisk 64 GB microSD card
- RTC coin cell battery
- Seagate Portable 2 TB hard drive
- Micro-HDMI to HDMI cable
- Flirc USB receiver and the Flipper Big Button remote
- The Samsung TV (UN40H4005AF) and its own remote
- The Mac, with this repo, Raspberry Pi Imager, and the Flirc app (flirc.tv)
- A USB keyboard is not needed. Everything after step 1 happens over ssh.

---

## Part 1: Build and boot the Pi

### Step 1. Flash the microSD card (Mac)

1. Open Raspberry Pi Imager. Choose **Raspberry Pi 5**, then **Raspberry Pi OS (other) → Raspberry Pi OS Lite (64-bit)**. It must be Lite (no desktop) and the Trixie release.
2. Choose the microSD card.
3. Open the settings (Edit Settings / OS customisation) and set:
   - Hostname: `channel3`
   - Username and password: your choice (this is your login, not the service user)
   - Wi-Fi: only if you chose Wi-Fi in Q1
   - Locale and time zone: `America/Toronto`
   - Services: enable SSH, **public-key authentication only**, and paste your Mac's public key
4. Write the card.

**Done when:** the Imager says the write and verify finished.

### Step 2. Assemble

Do this with the Pi powered off and unplugged. If it is running, shut it down first from the Mac with `ssh channel3.local sudo poweroff`. Wait until the green light on the Pi stops flashing, then pull the USB-C power cable.

Work on a table, not carpet, and touch something metal first to discharge static. Hold the board by its edges.

**What is in the case box:** a red base, a white frame with the fan already fitted (the fan sits in a clear plastic surround), a white lid, and an envelope with a small heatsink and four rubber feet.

**Find these on the Pi before you start.** Hold the Pi with the 40 gold GPIO pins along the top edge, pins facing up:

- **FAN socket:** a tiny white 4-pin socket printed `FAN`, in the top-right area, between the right end of the GPIO pins and the USB ports. It may have a small plastic cap on it. Lift the cap off and keep it.
- **BAT socket:** a tiny white 2-pin socket printed `BAT`, on the bottom edge, just inboard from the USB-C power port. Two bare holes near the power button look similar. They are for an external power switch. **Never connect the battery to them.**
- **CPU:** the large square silver chip near the middle of the board, with a raised top.
- **HDMI 0:** of the two micro-HDMI ports on the bottom edge, HDMI 0 is the one closest to the USB-C power port.

**Steps** (1 to 7 follow the official case instructions):

1. **Take out the microSD card.** Push it in gently and let it spring out, or pull it straight out. It goes back in at step 8, through the slot in the case.
2. **Heatsink.** Take the heatsink from the envelope, peel the backing off its sticky pad, and press it flat onto the raised top of the CPU. Line it up with the chip, not at an angle. Press down firmly for a few seconds.
3. **Feet.** Stick the four rubber feet into the four lozenge-shaped recesses on the underside of the red base.
4. **Board into the base.** Unclip the white frame from the red base. Lower the Pi into the base so the end with the microSD slot tucks under the small plastic tab at that end first. Then let the rest drop in. The board should sit flat. The USB-C, micro-HDMI, USB and Ethernet ports should line up with the holes in the base. Do not force it.
5. **RTC battery.** Push the battery's tiny 2-pin plug into the `BAT` socket. The plug only fits one way round. If it will not go in with gentle pressure, turn it over. Then peel the adhesive pad on the battery and stick the battery to the inside wall of the red base, clear of the board and away from the fan. The cable must not touch the fan blades when the frame goes on.
6. **Fan cable.** Turn the white frame upside down and lay it next to the base, fan side down, with the fan cable reaching over to the Pi. Push the fan cable's 4-pin plug into the `FAN` socket. The plug has a ridged side and fits only one way round. Line it up and push it straight down until it seats fully. It should sit flush, with no gap. If it is not going in, check which way round it is. Do not force it or wiggle it, because the socket is small and can break off the board.
7. **Close the case.** Flip the white frame over onto the base and clip it down all round. Check that the fan cable is folded inside and not trapped in the seam. The lid clips on top of the frame.
8. **microSD card.** Slide the card back in through the slot at the end of the case, label side facing down (towards the base), until it clicks or seats.
9. **Cables.** Plug the micro-HDMI cable into **HDMI 0**, the one next to the power port, and the other end into the TV. Note which HDMI input on the TV you used. Plug the Seagate drive into one of the two **blue** USB ports. Plug in Ethernet if you are using it. The Flirc waits until step 15. Plug in the USB-C power last.

**Done when:** the case is closed with nothing rattling, and on power-up the fan spins briefly and then may stop. The fan is temperature controlled, so silence at idle is normal. Step 4 checks it all from the Mac.

### Step 3. Turn on Anynet+ on the TV

On the Samsung's own remote: **Menu → System → Anynet+ (HDMI-CEC) → On**. It ships turned off. Leave the TV on the input the Pi is plugged into.

**Done when:** Anynet+ shows On.

### Step 4. First boot (Mac)

1. Plug in the power supply. The first boot takes a minute or two and may reboot once on its own.
2. From the Mac:

```
ssh channel3.local
```

3. Allow `sudo` without a password. Raspberry Pi Imager 2 sets your account up so `sudo` asks for one, but `make deploy`, the rollback command and the rsync route all run `sudo` over ssh with no way to type it. ssh accepts only your key, so this is safe on this box. It asks for your password once:

```
ssh -t channel3.local "echo 'tyler ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/010-tyler-nopasswd && sudo chmod 0440 /etc/sudoers.d/010-tyler-nopasswd && sudo visudo -c"
```

Replace `tyler` with your username if it differs. Check it with `ssh channel3.local sudo -n true && echo ok`.

4. On the Pi, update the system and reboot once:

```
sudo apt update && sudo apt full-upgrade -y && sudo reboot
```

5. Reconnect, then check that the clock is right and the RTC battery is seen:

```
ssh channel3.local
timedatectl
cat /sys/class/rtc/rtc0/battery_voltage 2>/dev/null || echo "no reading"
cat /sys/devices/platform/cooling_fan/hwmon/*/fan1_input
```

The last line prints the fan speed in RPM. 0 at idle is normal, because the fan only spins up when the chip warms. To prove it works, keep all four cores busy for a minute (`timeout 60 sh -c 'for i in 1 2 3 4; do yes >/dev/null & done; wait'`) and read it again. It should read in the thousands.

The RTC battery does not charge unless you turn charging on, and that is only safe for a rechargeable cell. The official Raspberry Pi RTC battery is a rechargeable lithium manganese ML-2020. Only with that battery, add `dtparam=rtc_bbat_vchg=3000000` to `/boot/firmware/config.txt` and reboot. **Never turn charging on for a non-rechargeable coin cell such as a CR2032.**

**Done when:** `ssh channel3.local` logs in without a password, `timedatectl` shows the right local time in `America/Toronto`, the battery voltage reads around 3 volts (about 3000000, in microvolts), and the fan reads above 0 under load.

---

## Part 2: Hard drive and setup script

### Step 5. Format the hard drive (Pi)

The setup script never formats a disk on purpose. You do it by hand once.

1. The Seagate drive should already be in a **blue** USB port from step 2.
2. Find it:

```
lsblk -o NAME,SIZE,TYPE,LABEL,MOUNTPOINT
```

You should see a disk of about 1.8T, almost always `sda`, with a partition `sda1`. The microSD card is `mmcblk0`. **Do not touch `mmcblk0`.**

3. Check the device name twice, then format the partition. This erases everything on the drive.

```
sudo mkfs.ext4 -L channel3 /dev/sda1
```

4. Note its PARTUUID:

```
lsblk -o NAME,SIZE,LABEL,PARTUUID
```

**Done when:** `sda1` shows label `channel3` and you have its PARTUUID written down.

### Step 6. Stop the drive from spinning down (Pi)

The broadcast plays from this drive around the clock, and a long Stand By stretch must never leave it asleep.

```
sudo apt install -y hdparm
sudo hdparm -S 0 /dev/sda
```

Some portable drives refuse `hdparm`. If it prints an error, write that down and move on. In normal use the broadcast keeps the drive reading constantly.

**Done when:** the command ran without error, or you noted that the drive refused it.

### Step 7. Run the setup script (Mac)

```
make pi-setup PI_HOST=channel3.local PI_DISK=/dev/sda1
```

You can pass the PARTUUID instead: `PI_DISK=PARTUUID=<the one from step 5>`.

The script installs mpv, v4l-utils, ffmpeg and rsync, plus the pinned yt-dlp. It creates the `channel3` service user and mounts the drive at `/srv/channel3`. It creates the `channels`, `library` and `local` folders and installs the systemd unit and the flag file `/etc/default/channel3`. It hides boot text and turns off the login prompt on the TV, and it sets the time zone. **It does not start the service.** The flags are not known until Part 3.

From now on the TV shows no login prompt. ssh is the only way in. If ssh ever breaks, pull the microSD card and fix it from the Mac.

Run it a second time:

```
make pi-setup PI_HOST=channel3.local PI_DISK=/dev/sda1
```

**Done when:** the second run reports that everything was already in place and nothing changed, and `ssh channel3.local df -h /srv/channel3` shows the 1.8T drive mounted.

### Step 8. Install the trial tools (Pi)

The setup script installs only what the service needs. The trials in Part 3 need two more tools:

```
sudo apt install -y evtest socat
```

`cec-ctl` came with v4l-utils in step 7.

**Done when:** `which evtest socat cec-ctl` prints three paths.

---

## Part 3: Hardware trials

This part finds the settings that work on this exact Pi and TV. Every result goes into your notes. Every flag that works ends up in one file in step 18.

### Step 9. Put a test video on the Pi (Mac)

You need one 1080p H.264 `.mp4`. Any video from the Mac's library will do, for example:

```
scp ~/srv/channel3/library/test/8-X8acD_r38.mp4 channel3.local:/tmp/test.mp4
ssh channel3.local "sudo install -o channel3 -g channel3 -m 0644 /tmp/test.mp4 /srv/channel3/local/test.mp4"
```

Check its resolution and codec:

```
ssh channel3.local "ffprobe -v error -select_streams v:0 -show_entries stream=codec_name,width,height -of csv=p=0 /srv/channel3/local/test.mp4"
```

**Done when:** it prints `h264,1920,1080`. If it is smaller, find a 1080p file, because 1080p is what tests the Pi's decoding.

### Step 10. Find the mpv output flags (Pi)

Record mpv and kernel versions first:

```
mpv --version | head -1
uname -a
```

Try each command below **in order**. Stop at the first one that plays the video full screen on the TV, smoothly. Press `q` in the ssh window to quit each one.

Try A:

```
sudo -u channel3 mpv --vo=gpu --gpu-context=drm --gpu-api=opengl --hwdec=no --fullscreen /srv/channel3/local/test.mp4
```

Try B:

```
sudo -u channel3 mpv --vo=drm --fullscreen /srv/channel3/local/test.mp4
```

Try C:

```
sudo -u channel3 mpv --vo=gpu-next --gpu-context=drm --fullscreen /srv/channel3/local/test.mp4
```

While the winning one plays:

- Watch the status line mpv prints in the ssh window. A `Dropped:` count that keeps climbing means the Pi cannot keep up.
- In a second ssh window, run `top` and note mpv's CPU use.

Write down the winning flags, the CPU use, and whether frames dropped.

**Done when:** one command plays full screen with no dropped frames, or few. If all three fail, stop here and bring the error output back. That case changes the plan.

### Step 11. Audio (Pi)

Run the winning command again and listen.

- If there is sound from the TV, the default audio device works. Note "default works".
- If there is no sound, list the devices:

```
sudo -u channel3 mpv --audio-device=help
```

Then try the HDMI 0 one:

```
sudo -u channel3 mpv <winning flags> --audio-device=alsa/sysdefault:CARD=vc4hdmi0 /srv/channel3/local/test.mp4
```

Check the TV volume too, and that the TV is not muted.

**Done when:** sound comes from the TV, and you have noted the device name if the default was wrong.

### Step 12. The full service flag set (Pi)

This checks that the winning flags work alongside the ones the service always adds. Start mpv with your winning flags plus these:

```
sudo -u channel3 mpv <winning flags> --fullscreen --no-osc --no-osd-bar --no-input-default-bindings --idle=yes --hr-seek=yes --image-display-duration=inf --input-ipc-server=/tmp/mpv.sock /srv/channel3/local/test.mp4
```

In a second ssh window, check that mpv answers on its control socket:

```
echo '{"command":["get_property","mpv-version"]}' | sudo -u channel3 socat - /tmp/mpv.sock
```

Then tell it to jump 60 seconds into the file:

```
echo '{"command":["loadfile","/srv/channel3/local/test.mp4","replace",-1,"start=60"]}' | sudo -u channel3 socat - /tmp/mpv.sock
```

Stop it with `sudo pkill mpv`.

**Done when:** the first command returns the mpv version and the second one jumps the picture to the one-minute mark within about a second.

### Step 13. TV power cycle (Pi)

Start the step 12 command again so video is playing. Then:

1. Turn the TV off with its own remote, wait ten seconds, turn it back on. Note whether the video comes back, whether mpv is still running (`pgrep mpv` in the second window), or whether it froze.
2. Unplug the HDMI cable from the TV, wait ten seconds, plug it back in. Note the same three things.

Stop it with `sudo pkill mpv`.

**Done when:** you have a result for both cases. Any result is fine. This only records how the TV behaves.

### Step 14. Program the Flirc (Mac)

Done 2026-10-05; the table is in `docs/hardware.md`. To redo it from scratch, or for a new Flirc:

1. Install the Flirc app (`brew install --cask flirc`). Its command-line tool is `/Applications/Flirc.app/Contents/Resources/flirc_util`, shortened to `flirc_util` below.
2. Plug the Flirc into the Mac. `flirc_util settings` lists what is recorded. `flirc_util format` wipes it.
3. Run each command, then press the matching remote button at the Flirc:
   - Channel up: `flirc_util record_api 0 75` (Page Up)
   - Channel down: `flirc_util record_api 0 78` (Page Down)
   - Digits 1 to 9: `flirc_util record_api 0 30` up to `0 38`. Digit 0: `flirc_util record_api 0 39`
   - Volume up, volume down, mute: `flirc_util record vol_up`, `record vol_down`, `record mute`. Use these media-key forms. Keyboard-page volume codes record fine but never reach the Pi.
   - On/Off: **open**. `record wake` sends nothing to a running Pi. See `docs/hardware.md`.
4. Back it up with `flirc_util saveconfig ~/srv/channel3/flirc-flipper`, which writes `flirc-flipper.fcfg`.

Restoring onto a reset Flirc is `flirc_util loadconfig ~/srv/channel3/flirc-flipper.fcfg`.

**Done when:** `flirc_util settings` lists every button once.

### Step 15. Read the Flirc key codes on the Pi (Pi)

1. Unplug the Flirc from the Mac and plug it into a black USB port on the Pi. Then restart the service (`sudo systemctl restart channel3`), because it only looks for the Flirc at startup.
2. Find its stable device name:

```
ls -l /dev/input/by-id/
```

Note the entry with `flirc` in its name, which should end in `-event-kbd`.

3. Run `evtest` on it:

```
sudo evtest /dev/input/by-id/<the flirc entry>
```

4. Press each remote button. Note the `KEY_*` name each one prints. Press `Ctrl-C` to stop.

**Done when:** you have a table of button → `KEY_*` code covering at least channel up, channel down and any digits.

### Step 16. CEC: can the Pi control the TV? (Pi)

Register the Pi as a playback device:

```
sudo cec-ctl -d /dev/cec0 --playback -S
```

Then try each one and watch the TV:

```
sudo cec-ctl -d /dev/cec0 --to 0 --standby
sudo cec-ctl -d /dev/cec0 --to 0 --image-view-on
sudo cec-ctl -d /dev/cec0 --to 0 --user-control-pressed ui-cmd=volume-up
sudo cec-ctl -d /dev/cec0 --to 0 --user-control-released
```

The first should turn the TV off and the second should turn it back on. The third and fourth together should raise the volume one notch.

If nothing responds, check Anynet+ is still On (step 3) and try again once. If the TV still ignores it, note **"no CEC"** and move on. The TV's own remote or a universal remote will handle power and volume instead, which is fine.

**Done when:** you have noted CEC **yes** (and which commands worked) or **no**.

### Step 17. Boot time and screen check (Pi)

1. Reboot:

```
sudo reboot
```

2. Start a stopwatch when the Pi goes down. Watch the TV during boot and note any text, logo or blinking cursor.
3. Reconnect and run:

```
systemd-analyze
```

**Done when:** you have noted the `systemd-analyze` total and anything that appeared on the TV during boot.

---

## Part 4: Turn on the service

### Step 18. Write the flags (Pi)

Everything Part 3 found goes in **one** line of one file. Nothing goes in the systemd unit.

```
sudo nano /etc/default/channel3
```

The file explains every option in comments. **Do not uncomment those lines.** Edit only the last line, `CHANNEL3_FLAGS=""`, and put the flags inside the quotes, separated by spaces. Each mpv flag gets an `--mpv-arg=` prefix.

Build it from your notes:

- **mpv output (step 10):** for Try A, add `--mpv-arg=--vo=gpu --mpv-arg=--gpu-context=drm --mpv-arg=--gpu-api=opengl --mpv-arg=--hwdec=no`
- **Audio (step 11):** only if the default was wrong, add `--mpv-arg=--audio-device=alsa/sysdefault:CARD=vc4hdmi0`
- **Remote (step 15):** add `--input-device=/dev/input/by-id/<the flirc entry>`
- **CEC (step 16):** only if CEC was yes, add `--cec`

A finished line looks like this:

```
CHANNEL3_FLAGS="--mpv-arg=--vo=gpu --mpv-arg=--gpu-context=drm --mpv-arg=--gpu-api=opengl --mpv-arg=--hwdec=no --input-device=/dev/input/by-id/usb-flirc.tv_flirc-event-kbd --cec"
```

Do not add `--root` or `--listen`. The unit already sets them. No single flag may contain a space.

Check the unit:

```
sudo systemd-analyze verify /etc/systemd/system/channel3.service
```

**Done when:** the file is saved and `systemd-analyze verify` prints nothing.

### Step 19. Deploy (Mac)

```
make deploy PI_HOST=channel3.local
```

This builds the Pi binary on the Mac, copies it over, starts the service, and watches the log for ten seconds. It fails if the service is not running at the end.

**Done when:** it prints the deployed version, and the TV shows the Please Stand By card.

Check the log:

```
ssh channel3.local journalctl -u channel3 --since -5min
```

If the same few lines repeat once a second, mpv cannot start. Go to step 20.

### Step 20. Only if the TV stays dark: debug in the foreground (Pi)

Stop the service and run it by hand, so you can see every message. Put your flags after `--listen :8080`:

```
sudo systemctl stop channel3
sudo -u channel3 /usr/local/bin/channel3 serve --root /srv/channel3 --listen :8080 <your flags>
```

Change the flags until the Please Stand By card appears, then press `Ctrl-C`, copy the working flags into `/etc/default/channel3` (step 18), and start the service again:

```
sudo systemctl start channel3
```

**Done when:** the service is running and the TV shows the Please Stand By card. Skip this step if step 19 already worked.

---

## Part 5: Real content

### Step 21. Add a channel (Mac)

Real channel files live outside the repo. Copy the example and edit it:

```
cp channels/example.yaml ~/srv/channel3/channels/<name>.yaml
```

Give it a unique `id`, a `number` (what you press on the remote), a `name`, and a few `sources`. Start small, with three or four videos.

Use YouTube links or `local/...` paths. **Do not use `file:///Users/...` paths.** Those only exist on the Mac. The `clips.yaml` test channel uses them and will not play on the Pi.

**Done when:** the file is saved.

### Step 22. Ingest (Mac)

There are two ways to do this. Use the first one tonight.

**On the Mac, then copy (TV stays on):**

```
bin/channel3 ingest --root ~/srv/channel3 --channel <id>
scp ~/srv/channel3/channels/<name>.yaml channel3.local:/tmp/
ssh channel3.local "sudo install -o channel3 -g channel3 -m 0644 /tmp/<name>.yaml /srv/channel3/channels/"
rsync -av --rsync-path="sudo -u channel3 rsync" ~/srv/channel3/library/ channel3.local:/srv/channel3/library/
rsync -av --rsync-path="sudo -u channel3 rsync" ~/srv/channel3/local/ channel3.local:/srv/channel3/local/
ssh channel3.local sudo systemctl restart channel3
```

The restart makes the new channel show up now instead of at 04:00.

**On the Pi (TV goes dark during the download):**

```
make pi-ingest PI_HOST=channel3.local ARGS="--channel <id>"
```

This needs the channel file on the Pi first (the `scp` and `install` lines above). It stops the service, downloads, and starts it again.

**Done when:** the TV is playing a video from your channel.

---

## Part 6: Check everything

### Step 23. From the couch

- [ ] Channel up and channel down change the channel within a second.
- [ ] Typing a one-digit channel number tunes it.
- [ ] Typing a two-digit number tunes it, if you have one.
- [ ] Typing a number with no channel leaves the picture alone.
- [ ] If CEC was yes: the remote's power and volume buttons work on the TV.
- [ ] Nothing the remote does shows text on the TV.

### Step 24. From the Mac

```
curl http://channel3.local/api/now
```

- [ ] It returns JSON naming the tuned channel and what is playing.
- [ ] `http://channel3.local` opens the guide on your phone.

Kill mpv and time how long it takes to come back:

```
ssh channel3.local sudo pkill mpv
```

- [ ] Video returns within five seconds at the right point in the programme.

Check that nothing but the pid file is stored:

```
ssh channel3.local ls /srv/channel3
```

- [ ] It shows `channels`, `library`, `local`, `lost+found` and `serve.pid`, and nothing else.

### Step 25. Cold boot

With the TV on, pull the Pi's power, plug it back in, and start a stopwatch.

- [ ] Video appears in under 30 seconds. Write down the time.
- [ ] No boot text, no logo, no blinking cursor, no login prompt on the TV.
- [ ] The right programme is playing for the time of day.

### Step 26. Deploy refusal

Start an ingest on the Pi in one window (`make pi-ingest ...`) and run `make deploy PI_HOST=channel3.local` in another.

- [ ] The deploy refuses to run.

### Step 27. Overnight

Leave it running overnight. The next morning:

```
ssh channel3.local journalctl -u channel3 --since yesterday
```

- [ ] No unexpected restarts.
- [ ] Note any excluded items (videos mpv could not open).
- [ ] The 04:00 rescan line is there.

---

## Afterwards

Bring your notes back to Claude. They become `docs/hardware.md`, and Claude ticks the hardware-gated tasks in `docs/plans/plan.md` (Phase 4, Phase 5 task 9, Phase 6 tasks 7 and 8, Phase 9 tasks 5 to 8).

### Everyday commands

| What | Command (Mac) |
|---|---|
| Watch the log | `ssh channel3.local journalctl -u channel3 -f` |
| Service status | `ssh channel3.local systemctl status channel3` |
| Deploy a new build | `make deploy PI_HOST=channel3.local` |
| Roll back one build | `ssh channel3.local "sudo sh -c 'cp -p /usr/local/bin/channel3.prev /usr/local/bin/channel3 && systemctl restart channel3'"` |
| Change flags | `ssh -t channel3.local sudo nano /etc/default/channel3`, then `ssh channel3.local sudo systemctl restart channel3` |

**Never deploy or ingest on the Pi while the kids are watching, and never deploy while an ingest is running.** Both restart the service.

### If something goes wrong

- **TV stays dark after deploy:** mpv cannot start. Read `journalctl -u channel3 --since -5min`, fix the flags (step 18), and restart.
- **Service will not start at all:** the hard drive is not mounted. The service refuses to run without it. Check `df -h /srv/channel3` and the USB cable.
- **Wrong programme for a few seconds after boot:** the clock was wrong until the network set it. With the RTC battery fitted this should not happen. Check `timedatectl`.
- **Cannot ssh in:** pull the microSD card, read it on the Mac, and fix the network or ssh settings there. The TV has no login prompt by design.

<!-- wp:question id="q3" type="text" -->
**Q3 — Anything missing or unclear:** Is there a step in this setup guide you expect to get stuck on, or something you want added before you start the build?
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->
