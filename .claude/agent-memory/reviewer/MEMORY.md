# Reviewer Memory

- [Plan amendments during execution](project_plan_amendments.md) — the lead rewrites checklist lines mid-phase; verify the amended wording, not the committed text.
- [Manual verification root](project_manual_verification_root.md) — end-to-end checks run against `~/srv/channel3`; back up and restore, keep it out of the repo.
- [Verify without touching the tree](feedback_verify_without_touching_the_tree.md) — go test -overlay both perturbs sources and injects an in-package probe test.
- [Drive raw-mode apps through a pty](feedback_drive_raw_mode_apps_through_a_pty.md) — fork under openpty and compare termios before and after to prove the terminal was restored.
