---
name: manual-verification-root
description: Manual verification runs against ~/srv/channel3, which holds a throwaway channel and real downloaded media; never add any of it to the repo
metadata:
  type: project
---

Phase verification on the Mac runs the built binary against `~/srv/channel3`, which the lead seeds with a throwaway channel config (`channels/test.yaml`, id `test`, number 3) pointing at one short public-domain NASA JPL YouTube video and one `file://` clip under `~/srv/channel3/local/`.

**Why:** The repo carries `channels/example.yaml` and fixtures only, so the only way to exercise a real end-to-end run is a root outside the repo. `.gitignore` already blocks `/library/` and `/channels/*.yaml`.

**How to apply:** Back up anything under that root before a destructive check (adding a bogus URL, deleting a sidecar) and restore it afterwards. Confirm `git status` is unchanged before reporting. Network use through yt-dlp during verification has been authorised per phase, not blanket. See [[plan-amendments-during-execution]].
