---
waypoint: 1
title: "Channel Three build kickoff: decisions before the autonomous run"
project: channel3
---

# Channel Three build kickoff

Date: 2026-09-22. Context: `docs/plans/plan.md` is committed with nine phases. Tyler asked for an `/orchestrator` run of the full build with Opus executing and reviewing each phase and a final review by the lead session at the end. These are the decisions the lead could not make alone. Each has a recommended answer pre-filled. The run proceeds on the recommendations and applies any changed answer when it pulls this review between phases.

## Scope of this run

<!-- wp:question id="q1" type="choice" select="single" -->
**Q1 — What runs now:** The Pi, TV, Flirc and remote are not purchased. Phase 4 (Pi hardware checklist) and Phase 9 (systemd, deploy, Pi setup) are marked hardware-gated in the plan. Phases 5 and 6 each have one or two Pi-only verification tasks that the reviewer records as WARN when the hardware is absent. Which of these should the autonomous run cover?
- [x] Phases 1, 2, 3, 5, 6, 7, 8 on the Mac, then also draft Phase 9's files (systemd unit, pi-setup.sh, deploy.sh, ingest.sh, deploy/README.md) with shellcheck and a placeholder for the mpv flags Phase 4 will discover. Everything Pi-only stays WARN. Stop there.
- [ ] Phases 1, 2, 3, 5, 6, 7, 8 only. Leave Phase 9 untouched until the hardware exists.
- [ ] Something else (say what below).
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q2" type="choice" select="single" -->
**Q2 — Parallel phases:** The plan has two parallel groups (ingest and schedule; API and web page). Running them at the same time in one working tree risks one executor's half-written file breaking the other's `go build`. Running them in isolated worktrees avoids that but adds a merge step before review. Sequential is slower by roughly an hour or two over the whole run but has no merge step and every phase is verified on a stable base.
- [x] Sequential. One phase at a time, in plan order, skipping Phase 4.
- [ ] Parallel in isolated git worktrees, merged to main before each review.
- [ ] Parallel in the shared working tree as the orchestrator skill describes.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

## Repo and git

<!-- wp:question id="q3" type="choice" select="single" -->
**Q3 — Go module path:** Phase 1 runs `go mod init` and the path is awkward to change later. The repo has no remote yet. Your GitHub remotes elsewhere are split between `tylersavery` (personal) and `theyoungastronauts` (work). Channel Three is a personal project that may go open source as a framework.
- [x] `github.com/tylersavery/channel3`
- [ ] `github.com/theyoungastronauts/channel3`
- [ ] Plain `channel3`, rename when a remote exists.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q4" type="choice" select="single" -->
**Q4 — Commits:** The plan names a branch per phase (`feature/channel-three-mvp-phase-N`). The orchestrator skill commits each passed phase straight to the current branch. This is a solo repo with no remote and nobody else reviewing branches.
- [x] Commit each passed phase directly to `main`, one commit per phase including its verification report. No branches.
- [ ] Branch per phase as the plan says, merged to `main` with a merge commit on PASS.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

## Autonomy

<!-- wp:question id="q5" type="choice" select="single" -->
**Q5 — When the reviewer says FAIL:** The orchestrator skill's default is to stop on the first FAIL and wait for a human. You asked to move autonomously.
- [x] Allow one remediation round per phase: send the reviewer's findings back to the executor, re-run lint and tests, re-verify. Stop and notify only on a second FAIL of the same phase.
- [ ] Stop on the first FAIL and wait for me.
- [ ] Keep remediating until PASS with no cap.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q6" type="choice" select="single" -->
**Q6 — Real network during verification:** Phase 2's verification checklist ingests one short video from YouTube with yt-dlp into `~/srv/channel3` and runs a network-gated Go test against it. Ingest is the only code allowed to touch the network, so this is within the rules, but it does download from YouTube on your Mac during the run.
- [x] Allow it. Use a short public-domain clip (a Prelinger Archive or NASA upload) as the test video.
- [ ] No network. Verify ingest with fixtures and local `file://` sources only; skip the real download.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q7" type="choice" select="single" -->
**Q7 — mpv on the Mac:** mpv is not installed on this Mac and Phase 5 needs it to run the player supervisor against real mpv. Installing it is `brew install mpv`.
- [x] Install it via Homebrew when Phase 5 starts.
- [ ] I will install it myself. Pause before Phase 5 if it is missing.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->

## Content and configuration

<!-- wp:question id="q8" type="text" -->
**Q8 — Channels for the local library:** Verification from Phase 2 onward needs channel YAML files in `~/srv/channel3/channels/` (outside the repo). The run can use throwaway test channels: two generated 20-second colour-bar clips as `file://` sources plus the one public-domain YouTube clip from Q6. If you want the real Train TV and Farm TV libraries to start filling during the build, paste channel names, numbers and YouTube URLs or playlist URLs here and the run will write them into `~/srv/channel3/channels/` for you.
<!-- wp:answer -->
Use throwaway test channels for now. I will add real channels later.
<!-- /wp:answer -->
<!-- /wp:question -->

<!-- wp:question id="q9" type="text" -->
**Q9 — Plan assumptions:** `docs/plans/plan.md` lists 21 assumptions the planner made where the brainstorm was silent. The ones most likely to draw an opinion: digit entry commits after 1.5 s with no further digit; the guide defaults to 6 hours; the Pi service listens on port 80; the library is rescanned only at the 04:00 rollover and at restart, so newly ingested videos appear the next broadcast day; mpv 0.38 or newer is required; the initial channel at boot is the lowest number. Anything to change before the run starts?
<!-- wp:answer -->
None. Proceed as planned.
<!-- /wp:answer -->
<!-- /wp:question -->

## Keeping you informed

<!-- wp:question id="q10" type="choice" select="multi" -->
**Q10 — Notifications:** You started this from your phone. The lead can send a push notification at key moments.
- [x] When the run halts for any reason (second FAIL, unexpected error, or a phase that cannot proceed).
- [x] When the run completes and the lead's final review is written.
- [ ] After every phase passes.
- [ ] No notifications.
<!-- wp:answer -->
<!-- /wp:answer -->
<!-- /wp:question -->
