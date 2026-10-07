---
waypoint_digest: 1
title: "Movie Mode plan"
project: "channel3"
source: "docs/plans/2026-10-06-movie-mode.md"
status: "completed"
reviewed_at: "2026-10-07T00:28:48.848Z"
answered: 8
flagged: 0
skipped: 0
untouched: 1
total: 9
comments: 0
synthetic: 0
---

# Digest — Movie Mode plan

## Questions

### q1 · choice (single) · answered
**Q:** **Q1 — Resume:** Channel Three's rule is that no playback state is ever stored. Should Movie Mode be an exception and remember where each movie was stopped?
**Selected:** Yes, as a named exception: one small file of positions, "Resume or Start over?" when you pick a movie that was stopped partway

### q2 · choice (single) · answered
**Q:** **Q2 — Default subtitles:** When a movie starts, what should subtitles be?
**Selected:** On, English, when the movie has English subtitles (forced-only tracks are used for foreign-language scenes when subtitles are off)

### q3 · choice (single) · answered
**Q:** **Q3 — Default audio:** Rips often carry several audio tracks (English 5.1, a director's commentary, other languages). Which plays first?
**Selected:** The first English track that is not a commentary; the disc's default if none is marked English

### q4 · choice (single) · answered
**Q:** **Q4 — Where 4K/HDR rips get converted:** Remuxing is quick anywhere. A full re-encode takes ~2–3 h on the Pi, ~15–25 min on the Mac.
**Selected:** On the Pi, overnight-style at low priority, so dropping a file is all you do

### q5 · choice (single) · answered
**Q:** **Q5 — Keep originals?** After a movie is prepared, what happens to the dropped file on the Pi? (Blu-ray rips are 20–40 GB; the drive is 2 TB.)
**Selected:** Delete it once the prepared copy is verified; your Dropbox still has it

### q6 · choice (single) · answered
**Q:** **Q6 — PIN:** Movie Mode needs a 4-digit code, stored only in `/etc/default/channel3` like the upload PIN.
**Selected:** Reuse the upload PIN

### q7 · choice (single) · answered
**Q:** **Q7 — Posters:** Ingest may touch the network. Where should posters come from?
**Selected:** Your `.jpg` if dropped, else a frame from the movie with the title drawn on it (no network)

### q8 · text · answered
**Q:** **Q8 — Button map:** The proposed Movie Mode keys are 5 play/pause, 4/6 ±10 s, 1/3 ±1 min, 7/9 ±5 min, 8 subtitles, 2 audio, 0 back to menu, Ch ± nothing. Anything you'd change?
**A:** Looks good as proposed.

### q9 · text · untouched
**Q:** **Q9 — First movies:** Which one or two rips should I test with first (ideally one DVD rip and one Blu-ray rip with picture subtitles)? Drop them in `~/srv/channel3/movies/` when ready.
**A:** _(no answer)_
