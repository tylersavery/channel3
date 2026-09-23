---
name: verify-without-touching-the-tree
description: Use go test -overlay to perturb sources or inject a probe test file, never by editing the implementation
metadata:
  type: feedback
---

When a phase claims a golden or fixture test pins an algorithm, prove it by perturbing the source through `go test -overlay=<json>` with the altered copy in the scratchpad, rather than editing the file and reverting it.

**Why:** The orchestrator lead's reviewer brief says the only writes allowed are the verification report and agent memory. Editing an implementation file to test it, even with the intent to revert, risks leaving the tree dirty before the lead commits, and a `git checkout` to undo it would discard the executor's uncommitted work.

**How to apply:** `sed` the one line into a scratchpad copy, write `{"Replace": {"<abs src>": "<abs copy>"}}`, then `go test -overlay=... -run TestX ./pkg/`. Same idea for any check that needs different source. Build synthetic roots and fixtures in the scratchpad too, never under the repo or [[manual-verification-root]].

When a phase's checklist already asks for a fresh copy of the tree (the `rsync -a --exclude .git` Node-free build check), that copy is also the cheapest place to perturb sources: edit it freely and delete it afterwards, no overlay JSON needed. Phase 7 used it to delete the `RLock` from a new mutex-guarded accessor and confirm the shipped `-race` test then reported `WARNING: DATA RACE`, which is how a concurrency test is shown to be real rather than decorative. Prefer this when a copy already exists; prefer the overlay when one does not, and resist copying a probe test into the real package even for one command.

The overlay map also **adds** files: name a path that does not exist on disk, such as `<pkg>/zz_probe_test.go`, and point it at a scratchpad file. That gives an in-package test with access to unexported identifiers, which is how to turn a claim into a measurement without a single tree edit. Two uses that paid off in Phase 5: asserting which error value a caller actually receives when a connection dies, where the shipped test only checked `err != nil` and hid a wrong error identity; and counting leaked goroutines by filtering `runtime.Stack(buf, true)` for the package's own frames after N create-and-close cycles, which beats `NumGoroutine()` because test helpers keep their own goroutines alive until cleanup. Reuse the package's existing test helpers rather than rewriting them, and check whether they register a `t.Cleanup` that your loop would fight.
