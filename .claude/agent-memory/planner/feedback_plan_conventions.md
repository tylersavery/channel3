---
name: channel3-plan-conventions
description: How plan.md files for Channel Three must be structured (hardware-gated markers, four named skills, dedicated assumptions section, writing rules, no commit)
metadata:
  type: feedback
---

Plans for this repo follow the `plan-and-scope` and `phase-breakdown` skills plus these house rules: mark every Pi-dependent phase or task `(hardware-gated)` and let the verifier record unfinished gated tasks as WARN; name `execute`, `verify`, `work-discipline` and `commit-conventions` as the skills for every phase; put every judgment call in a dedicated "Assumptions and open questions" section rather than burying it in tasks; list the brainstorm's Non-goals verbatim under Out of scope; write one line per paragraph with no em-dashes; write the file and stop without committing.

**Why:** Hardware was not purchased when the first plan was written (2026-09-22), so Mac-side and Pi-side work had to be separable. The planner cannot ask questions in a team run, so assumptions must be visible in one place for Tyler to overrule. Tyler's global CLAUDE.md forbids hard-wrapped prose in files.

**How to apply:** Any future plan or plan revision in `docs/plans/` for this project. The first plan is `docs/plans/plan.md` (nine phases; 4 and 9 fully gated; 5 and 6 have gated tasks).
