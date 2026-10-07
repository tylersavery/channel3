---
waypoint: 1
title: "Movie Mode plan"
project: channel3
---

# Movie Mode

Movie Mode is on-demand playback of your own DVD and Blu-ray rips, behind a PIN, with the only playback controls Channel Three has. Broadcast keeps running by the clock while a movie plays; leaving Movie Mode tunes back to a channel exactly as if you had never left.

## What's settled

- **Entry:** press **0** on the remote. A "Movie Night — enter code" screen asks for a 4-digit PIN. A wrong code or 10 s of silence returns to the channel you came from.
- **Menu:** a full-screen poster menu in the guide's style, movies numbered. Press a number to play.
- **Ch Up / Ch Down do nothing** in Movie Mode.
- **Subtitles from day one,** including the picture subtitles DVD and Blu-ray rips carry (VobSub, PGS), text subtitles in the file, and a `.srt` beside the movie.
- **Sources** are rips dropped in a radio-style drop zone on the Mac, moved to the Pi by `make pi-ingest`. No YouTube.
- **1080p ceiling.** Anything larger is scaled to 1080p; the 720p TV is not the target, a future screen is.

## How it works

### Library

- Drop zone on the Mac: `~/srv/channel3/movies/`. One file per movie (`.mkv`, `.mp4`, `.m4v`), optionally with `Name.srt` / `Name.en.srt` beside it and an optional `Name.jpg` poster.
- `pi-ingest` moves them to `/srv/channel3/movies-inbox/` with `--remove-source-files`, like radio.
- A prepare step (the home-video worker's pattern: nice, 2 threads, set aside on failure) turns each into `/srv/channel3/local/movies/<Title (Year)>.mkv` plus a sidecar:
  - **Already ≤1080p H.264 or H.265 8-bit SDR → remux only.** No re-encode, minutes, no quality loss. This is every DVD rip and most Blu-ray rips.
  - **4K, 10-bit or HDR → re-encode** to 1080p H.264 (CRF ~20), HDR tone-mapped with the home-video zscale + hable chain. Roughly 2–3 h per movie on the Pi at low priority, TV unaffected.
  - **All audio and subtitle tracks are kept** (MKV holds picture subtitles; MP4 cannot). `.srt` files beside the movie are muxed in with their language.
  - Sidecar holds title, year, duration, loudness (the existing R128 measure), track list with languages, and the poster path.
- **Poster:** a `.jpg` beside the movie if you drop one; otherwise a frame grabbed at 10% of the runtime, with the title drawn on it.

### Playback

- The station gains a mode: `broadcast` (today) or `movie`. In movie mode the schedule, reconcile, bumpers, track titles and guide paging all step aside. Volume and mute work as everywhere.
- mpv already renders every subtitle format; we only pick the track (`sid`), the audio (`aid`) and style text subtitles to match (white, outlined, lower third, readable on a 40" from the couch).
- A green progress bar and time read-out show on any seek or pause and fade after 4 s.
- When the movie ends: "The End" for 5 s, then the menu.
- Menu idle 10 minutes → back to the channel you came from.

### Remote in Movie Mode (proposed)

| Key | Action |
|---|---|
| 5 | Play / pause |
| 4 / 6 | Back / forward 10 s |
| 1 / 3 | Back / forward 1 min |
| 7 / 9 | Back / forward 5 min |
| 8 | Cycle subtitles (Off → each track) |
| 2 | Cycle audio track |
| 0 | Stop, back to the menu (press 0 on the menu to leave Movie Mode) |
| Ch ± | Nothing |
| Vol ± / Mute | As always |

## Phases

1. **Library and prepare.** Drop zone, `pi-ingest` move, probe, remux-or-encode, subtitle mux, sidecar, poster. `channel3 prepare-movie` CLI for the Mac. Table tests over probe results → decision.
2. **Station mode and player.** Mode switch, PIN screen, play/pause/seek/subs/audio over IPC (fake-socket tests), progress bar overlay, The End, leave Movie Mode.
3. **Menu.** Poster menu rendering (like `RenderGuide`), paging, idle timeout.
4. **On the Pi.** Prepare a real DVD rip and a real Blu-ray rip, check PGS subtitles draw, check HEVC/H.264 decode at 1080p is smooth, couch test.
5. **Later, not in this plan:** phone as remote, resume across days if not chosen below, deleting a movie from the phone.

## Decisions for you

<!-- wp:question id="q1" type="choice" select="single" -->
**Q1 — Resume:** Channel Three's rule is that no playback state is ever stored. Should Movie Mode be an exception and remember where each movie was stopped?
- [x] Yes, as a named exception: one small file of positions, "Resume or Start over?" when you pick a movie that was stopped partway
- [ ] Remember only until the box restarts (in memory), no file
- [ ] No resume at all; every movie starts from the beginning
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q2" type="choice" select="single" -->
**Q2 — Default subtitles:** When a movie starts, what should subtitles be?
- [x] On, English, when the movie has English subtitles (forced-only tracks are used for foreign-language scenes when subtitles are off)
- [ ] Off by default; press 8 to turn them on
- [ ] Remember the last choice across movies
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q3" type="choice" select="single" -->
**Q3 — Default audio:** Rips often carry several audio tracks (English 5.1, a director's commentary, other languages). Which plays first?
- [x] The first English track that is not a commentary; the disc's default if none is marked English
- [ ] Always the file's default track
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q4" type="choice" select="single" -->
**Q4 — Where 4K/HDR rips get converted:** Remuxing is quick anywhere. A full re-encode takes ~2–3 h on the Pi, ~15–25 min on the Mac.
- [x] On the Pi, overnight-style at low priority, so dropping a file is all you do
- [ ] On the Mac before syncing (`channel3 prepare-movie`), so the Pi only ever remuxes
- [ ] Both: the Pi handles it, and the Mac CLI is there when you want it faster
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q5" type="choice" select="single" -->
**Q5 — Keep originals?** After a movie is prepared, what happens to the dropped file on the Pi? (Blu-ray rips are 20–40 GB; the drive is 2 TB.)
- [x] Delete it once the prepared copy is verified; your Dropbox still has it
- [ ] Keep it in `movies-originals/` like home videos
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q6" type="choice" select="single" -->
**Q6 — PIN:** Movie Mode needs a 4-digit code, stored only in `/etc/default/channel3` like the upload PIN.
- [x] Reuse the upload PIN
- [ ] A separate Movie PIN (tell me the digits in chat, not here)
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q7" type="choice" select="single" -->
**Q7 — Posters:** Ingest may touch the network. Where should posters come from?
- [x] Your `.jpg` if dropped, else a frame from the movie with the title drawn on it (no network)
- [ ] Look the poster up online at prepare time (TMDb, needs a free API key), frame as fallback
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q8" type="text" -->
**Q8 — Button map:** The proposed Movie Mode keys are 5 play/pause, 4/6 ±10 s, 1/3 ±1 min, 7/9 ±5 min, 8 subtitles, 2 audio, 0 back to menu, Ch ± nothing. Anything you'd change?
<!-- wp:answer -->
Looks good as proposed.
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q9" type="text" -->
**Q9 — First movies:** Which one or two rips should I test with first (ideally one DVD rip and one Blu-ray rip with picture subtitles)? Drop them in `~/srv/channel3/movies/` when ready.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->
