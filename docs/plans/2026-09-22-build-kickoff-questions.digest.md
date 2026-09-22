---
waypoint_digest: 1
title: "Channel Three build kickoff"
project: "channel3"
source: "docs/plans/2026-09-22-build-kickoff-questions.md"
status: "completed"
reviewed_at: "2026-09-22T23:29:55.012Z"
answered: 10
flagged: 0
skipped: 0
untouched: 0
total: 10
comments: 0
synthetic: 0
---

# Digest — Channel Three build kickoff

## Questions

### q1 · choice (single) · answered
**Q:** **Q1 — What runs now:** The Pi, TV, Flirc and remote are not purchased. Phase 4 (Pi hardware checklist) and Phase 9 (systemd, deploy, Pi setup) are marked hardware-gated in the plan. Phases 5 and 6 each have one or two Pi-only verification tasks that the reviewer records as WARN when the hardware is absent. Which of these should the autonomous run cover?
**Selected:** Phases 1, 2, 3, 5, 6, 7, 8 on the Mac, then also draft Phase 9's files (systemd unit, pi-setup.sh, deploy.sh, ingest.sh, deploy/README.md) with shellcheck and a placeholder for the mpv flags Phase 4 will discover. Everything Pi-only stays WARN. Stop there.

### q2 · choice (single) · answered
**Q:** **Q2 — Parallel phases:** The plan has two parallel groups (ingest and schedule; API and web page). Running them at the same time in one working tree risks one executor's half-written file breaking the other's `go build`. Running them in isolated worktrees avoids that but adds a merge step before review. Sequential is slower by roughly an hour or two over the whole run but has no merge step and every phase is verified on a stable base.
**Selected:** Sequential. One phase at a time, in plan order, skipping Phase 4.

### q3 · choice (single) · answered
**Q:** **Q3 — Go module path:** Phase 1 runs `go mod init` and the path is awkward to change later. The repo has no remote yet. Your GitHub remotes elsewhere are split between `tylersavery` (personal) and `theyoungastronauts` (work). Channel Three is a personal project that may go open source as a framework.
**Selected:** `github.com/tylersavery/channel3`

### q4 · choice (single) · answered
**Q:** **Q4 — Commits:** The plan names a branch per phase (`feature/channel-three-mvp-phase-N`). The orchestrator skill commits each passed phase straight to the current branch. This is a solo repo with no remote and nobody else reviewing branches.
**Selected:** Commit each passed phase directly to `main`, one commit per phase including its verification report. No branches.

### q5 · choice (single) · answered
**Q:** **Q5 — When the reviewer says FAIL:** The orchestrator skill's default is to stop on the first FAIL and wait for a human. You asked to move autonomously.
**Selected:** Allow one remediation round per phase: send the reviewer's findings back to the executor, re-run lint and tests, re-verify. Stop and notify only on a second FAIL of the same phase.

### q6 · choice (single) · answered
**Q:** **Q6 — Real network during verification:** Phase 2's verification checklist ingests one short video from YouTube with yt-dlp into `~/srv/channel3` and runs a network-gated Go test against it. Ingest is the only code allowed to touch the network, so this is within the rules, but it does download from YouTube on your Mac during the run.
**Selected:** Allow it. Use a short public-domain clip (a Prelinger Archive or NASA upload) as the test video.

### q7 · choice (single) · answered
**Q:** **Q7 — mpv on the Mac:** mpv is not installed on this Mac and Phase 5 needs it to run the player supervisor against real mpv. Installing it is `brew install mpv`.
**Selected:** Install it via Homebrew when Phase 5 starts.

### q8 · text · answered
**Q:** **Q8 — Channels for the local library:** Verification from Phase 2 onward needs channel YAML files in `~/srv/channel3/channels/` (outside the repo). The run can use throwaway test channels: two generated 20-second colour-bar clips as `file://` sources plus the one public-domain YouTube clip from Q6. If you want the real Train TV and Farm TV libraries to start filling during the build, paste channel names, numbers and YouTube URLs or playlist URLs here and the run will write them into `~/srv/channel3/channels/` for you.
**A:** Use throwaway test channels for now. I will add real channels later.

### q9 · text · answered
**Q:** **Q9 — Plan assumptions:** `docs/plans/plan.md` lists 21 assumptions the planner made where the brainstorm was silent. The ones most likely to draw an opinion: digit entry commits after 1.5 s with no further digit; the guide defaults to 6 hours; the Pi service listens on port 80; the library is rescanned only at the 04:00 rollover and at restart, so newly ingested videos appear the next broadcast day; mpv 0.38 or newer is required; the initial channel at boot is the lowest number. Anything to change before the run starts?
**A:** None. Proceed as planned.

### q10 · choice (multi) · answered
**Q:** **Q10 — Notifications:** You started this from your phone. The lead can send a push notification at key moments.
**Selected:** When the run halts for any reason (second FAIL, unexpected error, or a phase that cannot proceed)., When the run completes and the lead's final review is written., After every phase passes.
