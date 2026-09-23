# Guide API

The read-only HTTP interface `channel3 serve` exposes for the guide page and for any other client.

Written by Phase 7. This is the contract as the code finalises it, and it supersedes the sketch in `docs/plans/plan.md` wherever the two differ. Every difference is listed under Deviations below.

## Serving

`channel3 serve --listen :3333` serves both the API and the web interface on the same port. The default is `:3333` and the project never uses 3000.

`channel3 serve --ui-dir web/dist/ui` serves the web interface from disk instead of from the copy embedded in the binary, which is how the page is developed against a running station. Without the flag the binary serves what `//go:embed all:dist` captured at build time, from `dist/ui` inside `web.Dist`.

There is no authentication and no TLS. The service is meant for a home network and binds whatever `--listen` says, so a Pi with a public address would be exposing it.

No endpoint changes what is playing. There is no tune, pause or seek. The API reads the same pure schedule the broadcast loop reads.

## Conventions

All JSON bodies are `application/json; charset=utf-8` and end with a newline.

Times are RFC 3339 with whole seconds, in the broadcast clock's zone, which is the Pi's local zone. Example: `2026-09-22T19:04:11-04:00`.

Durations and offsets are seconds as JSON numbers, fractional where the video is: `612.437`.

`duration` is always the item's own length. It is never `end` minus `start`, because an airing that would run past the 04:00 broadcast day rollover is cut short there. A client drawing a progress bar must use `offset` and `duration`.

Channels are sorted by number, lowest first, in every response.

Errors are `{"error": "<message>"}` with a 4xx or 5xx status.

Only `GET` and `HEAD` are accepted. Every other method is `405` with `Allow: GET, HEAD` and a JSON error body.

Every API response carries `Cache-Control: no-store`. The guide is the clock and nothing about it may be held.

## GET /api/channels

```json
{
  "channels": [
    {"id": "trains", "number": 3, "name": "Train TV", "items": 3}
  ]
}
```

`items` is how many videos the channel draws on, not how many air today.

`channels` is an empty array, never null, when nothing has been loaded.

## GET /api/now

```json
{
  "time": "2026-09-22T19:04:11-04:00",
  "tuned": "trains",
  "channels": [
    {
      "id": "trains", "number": 3, "name": "Train TV",
      "now":  {"id": "def", "title": "Switching Yard", "start": "2026-09-22T18:52:39-04:00", "end": "2026-09-22T19:15:27-04:00", "duration": 1368, "offset": 691.823},
      "next": {"id": "abc", "title": "Steam Engines", "start": "2026-09-22T19:15:27-04:00", "end": "2026-09-22T19:25:39-04:00", "duration": 612.437, "offset": 0}
    },
    {"id": "empty", "number": 9, "name": "Nothing At All", "now": null, "next": null}
  ]
}
```

`tuned` is the channel id on air, or null when nothing is tuned. It is null while the service is starting and mpv has not come up yet, and it is null if the tuned channel disappears from the config.

`now.offset` is how far into the item the broadcast is, from `schedule.At`, the same function that told mpv where to seek.

`next.offset` is always 0. The next slot starts at the end of the current one.

A channel with no playable items has `"now": null` and `"next": null`.

## GET /api/guide?hours=N

```json
{
  "from": "2026-09-22T19:04:11-04:00",
  "to":   "2026-09-22T21:04:11-04:00",
  "channels": [
    {
      "id": "trains", "number": 3, "name": "Train TV",
      "slots": [
        {"id": "def", "title": "Switching Yard", "start": "2026-09-22T18:52:39-04:00", "end": "2026-09-22T19:15:27-04:00", "duration": 1368}
      ]
    }
  ]
}
```

`hours` is an optional whole number, default 6, minimum 1, maximum 48. Anything else is a 400 with a JSON error, including `0`, `49`, `-1`, `2.5` and `abc`.

An empty `?hours=` is treated as an omitted one and gets the default 6, which is what a client building a query string from an empty form field sends.

`to` is exactly `hours` after `from`.

The first slot of each channel usually starts before `from`, because it is whatever is already airing.

Slots are contiguous: each slot's `end` is the next slot's `start`. The last slot of a channel ends at or after `to`, so a timeline can be drawn across the whole window.

An airing that crosses 04:00 is cut there and the next slot is the first item of the new broadcast day's order, which is what the service really does at the rollover.

`slots` is an empty array, never null, for a channel with no playable items.

## GET / and static paths

`GET /` serves `index.html` from the web interface. Any other path is looked up as a file in the same build.

`index.html` is served with `Cache-Control: no-cache`, so a redeployed page is picked up on the next load.

A file under `assets/` whose name carries a content hash, such as `assets/index-DkJ2f8Qa.js`, is served with `Cache-Control: public, max-age=31536000, immutable`. The hash has to be 8 to 12 characters after a dash and the extension has to be one a build emits.

Every other file is served with `Cache-Control: no-cache`, including anything outside `assets/`. A name like `apple-touchicon.png` or `logo-horizontal.svg` has a dash but no content hash, and a browser that held it for a year would keep serving an old one until the file was renamed.

When the build is absent, or has no `index.html`, `GET /` serves a plain HTML page saying the web interface has not been built and linking the three API endpoints. It is a 200, not a 404, because the API behind it is up.

A request for a file the build does not have is a 404 with a JSON error, not the fallback page. A browser must never be handed HTML where it asked for JavaScript.

Directories are never listed, and a path is cleaned before it is looked up, so nothing outside the build can be reached.

There is no single page application rewrite. A path that is not a file 404s rather than falling back to `index.html`, because the guide is one page. Phase 8 must not add client side routes that rely on a rewrite without changing this.

## Startup order and known behaviour

The HTTP listener is opened before mpv is launched. The guide answers as soon as the process is up, which is exactly when someone reaches for a phone to find out why the television is dark.

Until the station has loaded the library and tuned, `/api/channels` and `/api/guide` answer with an empty `channels` array and `/api/now` answers with `"tuned": null`. That window is normally under a second.

If mpv cannot be launched at all, `serve` still exits non-zero, so systemd restarts it, and the API goes down with it. This is carried over from Phase 5 and is deliberate for now: a Pi whose player is broken should keep restarting rather than sit there serving a guide to a black television. Phase 9 should reconsider whether the guide ought to survive a failed mpv launch and report the failure instead, which would be a better appliance but needs the restart policy in the unit file to be designed around it.

A `--listen` address already in use fails the command. The pid file check already refuses a second `serve`, so a bound port means something else is on it, and that is worth failing loudly at boot rather than logging once.

The API reads the station through a mutex guarded accessor, so a request arriving during the 04:00 rollover sees either the old channel list or the new one and never a half replaced slice.

## Known divergence between the guide and the screen

The API does not see the broadcast loop's exclusions.

When mpv cannot open a file, the station drops that item from the tuned channel for the rest of the broadcast day and the day's order is recomputed without it. The API computes the schedule from the channel list alone, so `/api/now` and `/api/guide` go on reporting the order that includes the excluded item, and the guide is then ahead of the screen until the 04:00 rollover clears the exclusions.

This only happens after a real load error, which on a working library is never. The fix is an exclusions accessor on `api.Deps`, guarded the same way `Channels` is, that the handlers apply before calling the schedule. That is future work and not Phase 7.

## Deviations from the plan's sketch

`HEAD` is accepted as well as `GET`. The plan says every method other than `GET` is a 405. `HEAD` is answered by net/http from the same handler with the body dropped and nothing about it can change state.

Unknown paths under `/api/` return a JSON 404 rather than falling through to the web interface. The plan did not say.

`now.end` and a guide slot's `end` are both cut at the 04:00 rollover. The plan only said the guide does this. Doing it in one place and not the other would have the two endpoints disagree about the same airing.

The plan's task list says `schedule.Guide(ch, now, 0)`. The real signature is `schedule.Guide(ch, from, horizon, clock)`. `now` and `next` are two calls to it with a zero horizon, which is `schedule.At` plus the rollover cut.
