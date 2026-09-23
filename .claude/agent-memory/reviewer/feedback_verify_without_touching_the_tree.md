---
name: verify-without-touching-the-tree
description: Prove a pinning test really fails on an algorithm change by using go test -overlay, never by editing the implementation
metadata:
  type: feedback
---

When a phase claims a golden or fixture test pins an algorithm, prove it by perturbing the source through `go test -overlay=<json>` with the altered copy in the scratchpad, rather than editing the file and reverting it.

**Why:** The orchestrator lead's reviewer brief says the only writes allowed are the verification report and agent memory. Editing an implementation file to test it, even with the intent to revert, risks leaving the tree dirty before the lead commits, and a `git checkout` to undo it would discard the executor's uncommitted work.

**How to apply:** `sed` the one line into a scratchpad copy, write `{"Replace": {"<abs src>": "<abs copy>"}}`, then `go test -overlay=... -run TestX ./pkg/`. Same idea for any check that needs different source. Build synthetic roots and fixtures in the scratchpad too, never under the repo or [[manual-verification-root]].
