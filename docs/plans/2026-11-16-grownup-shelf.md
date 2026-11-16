---
title: "Grown-up shelf plan"
project: channel3
---

# Grown-up shelf

On Demand gets a second shelf for Tyler: science, learning and making videos that Lane isn't into yet. Nothing on it is inappropriate, because the TV is in the living room and it could be left on, but it's kept off Lane's menu.

## What's settled

- **Same door, different code.** Press 0 and the code screen comes up as it does today. The upload PIN opens Lane's shelf. A second code, `CHANNEL3_GROWNUP_PIN` in `/etc/default/channel3`, opens the grown-up shelf. Both codes must be the same length, because the screen checks the code once enough digits have been typed. If the second code is a different length, is the same as the first, or there is no upload PIN, it is logged and turned off, and the broadcast carries on.
- **Its own shelf file,** `<root>/shelf-grownup.json`, in the same format and with the same revision check as `shelf.json`. Covers share `shelf-covers/` (keys are channel/id, so a video on both shelves has one cover), and resume positions share `movie-positions.json`.
- **No Movies folder.** The grown-up menu is its own shelf and nothing else. When it is empty, the menu is empty.
- **Its content comes from hidden channels.** A channel with `hidden: true` is off the dial but still in the library, so a grown-up channel (say `grownup-science`) is written and ingested like any other, then never appears on the remote or in the guide. The grown-up shelf can also pick from any other channel.
- **The phone gets a Grown-ups tab.** It works like the On Demand tab against the grown-up shelf, and its writes carry the grown-up code. The tab shows only when the box has one.

## API

The shelf endpoints take `?shelf=grownup`. Without it they act on Lane's shelf as before.

- `GET /api/library?shelf=grownup`: `on_shelf` is read against the grown-up shelf.
- `GET`/`PUT /api/shelf?shelf=grownup`: a PUT needs the grown-up code in `X-Upload-PIN`.
- `PUT /api/shelf/cover?shelf=grownup&key=…` and `POST /api/shelf/cover/frame?shelf=grownup&key=…`: the key must be on the grown-up shelf, and the request must carry the grown-up code.
- `?shelf=grownup` on a box without the second code is a 404. Any other value of `shelf` is a 400.

## Phases

1. Shelf store at any path; serve's `--grownup-pin` flag and its checks; the cover maker covering both shelves.
2. API: the `shelf` parameter, a code for each shelf. Handler tests.
3. Television: the second code opens the grown-up menu. Tests against the fake player.
4. Web: the Grown-ups tab. Vitest.
5. On the Pi: set the code, deploy when nobody is watching, ingest the first grown-up channel.
