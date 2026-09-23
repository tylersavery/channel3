---
name: plan-amendments-during-execution
description: The orchestrator lead amends plan.md verification checklist lines mid-phase when an executor proves the original cannot hold; verify the amended wording
metadata:
  type: project
---

On Channel Three the orchestrator lead edits a phase's Verification checklist in `docs/plans/plan.md` during execution when the executor demonstrates the original line was wrong, and the reviewer must verify the amended wording rather than the text the plan was committed with.

**Why:** Phase 2's checklist said a second ingest run must not invoke yt-dlp at all, which contradicted the same phase's own requirement that a video added to a playlist be noticed. The executor showed the contradiction, the lead rewrote the line, and the plan diff carries both the ticked task boxes and the amendment.

**How to apply:** Read `git diff docs/plans/plan.md` at the start of a review so the amendments are visible, then judge each amended line on whether it still serves the phase's Objective. Record the ruling in the report. See [[phase-2-ingest-findings]].
