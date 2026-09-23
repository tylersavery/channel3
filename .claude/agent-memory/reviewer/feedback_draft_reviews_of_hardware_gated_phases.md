---
name: draft-reviews-of-hardware-gated-phases
description: How to review a hardware-gated phase drafted without the Pi, TV, Flirc or remote; what stays WARN and what may still FAIL.
metadata:
  type: feedback
---

When a hardware-gated phase is executed in draft mode (files written and linted on the Mac, nothing run against hardware), every finding that only hardware can settle is a WARN, never a FAIL, and the summary says plainly that it is a draft review. Findings that a Mac can settle, such as a flag value that does not exist in the code or an instruction that produces a file systemd cannot parse, may still be FAIL.

**Why:** the plan's "Ground rules for every phase" already says gated tasks are left unticked with a one-line note and the verifier records them as WARN. Failing a phase for the absence of hardware would stall the whole plan, while letting a wrong flag value through ships a box that crash loops with nobody at a keyboard.

**How to apply:** read the phase's own hands-on checklist as the WARN list, then spend the review budget on what is checkable now: every flag and path in a config or env file against the Go flag definitions, exit codes against the callers that interpret them, and the shell scripts under `shellcheck` and `bash -n`. Do not ssh anywhere, do not run the scripts against a host, do not run `serve`. See [[project_plan_amendments]] for how the plan text itself moves during a phase.
