---
title: "On Demand shelf plan"
project: channel3
---

# On Demand shelf

Channel 0 grows from Movie Night into On Demand: the movies, plus episodes picked by hand from the library, some on their own and some gathered into a folder per show. The point is that when Lane wants something specific, it goes on with the remote instead of YouTube on a phone, and when it ends it stops instead of suggesting the next thing.

## What's settled

- **Same door.** Press 0, enter the PIN, and the menu opens. Everything Movie Mode has stays: number picking, Ch ± to page, pause, seek, subtitles and audio on the number keys, "Resume or Start over?", The End, and 10 minutes of idle closing it.
- **Two levels.** The top of the menu holds folders and single episodes. A folder (Tractor Ted, say) opens to its episodes. 0 inside a folder goes back up; 0 at the top goes back to TV, as today.
- **Movies keep their posters** and live in a Movies folder at the top, filled automatically from the prepared movies as today. Until the shelf has anything on it, the top is the movies themselves, which is exactly today's menu.
- **Episodes are 16:9 tiles,** four across and three down, with the cover drawn edge to edge and the title under it.
- **Covers** are a frame grabbed from the episode when it is added. From the phone you can ask for another frame, or upload a picture of your own. A folder's cover is its first episode's unless you upload one.
- **Resume works for episodes as it does for movies,** with the thresholds scaled to the length: a 7 minute episode offers to resume once it is past about a minute, and counts as watched in its last half minute or so.
- **Managed from the web guide.** The guide page gains an On Demand tab: the shelf as it stands, with rename, reorder, move into or out of a folder, remove, and cover changes, and below it every video in the library by channel with a toggle to put it on the shelf. Each channel on the Guide tab gets "Add to On Demand" for whatever is on now, so something he is into can be saved from the couch.
- **The shelf points at the library,** by channel and video id. Nothing is copied. A video that leaves the library drops off the shelf quietly, and comes back if it returns.

## How it works

### Shelf file

`<root>/shelf.json`, written only by the service. It carries a revision number so two phones editing at once cannot overwrite each other silently: a save names the revision it was based on and is refused if the file has moved on.

```json
{
  "revision": 4,
  "entries": [
    { "folder": { "id": "f1", "name": "Tractor Ted", "items": [ { "channel": "farmtv", "id": "abc123" } ] } },
    { "item": { "channel": "sesame", "id": "BQbjApUBrEc", "title": "Telly and the Fire Bell" } }
  ]
}
```

An item may carry its own title, which wins over the library's. Covers live in `<root>/shelf-covers/`, one JPEG per item or folder.

### API

Reads are open like the rest of the guide. Writes carry the upload PIN in `X-Upload-PIN` and exist only when the box has one, like `/api/upload`. None of them changes what plays; they change only what the On Demand menu offers.

- `GET /api/library`: every video the station can play, by channel.
- `GET /api/shelf`: the shelf, with each item's title and length filled from the library and a flag on anything that has left it.
- `PUT /api/shelf`: the whole shelf, with the revision it was based on. 409 if that is stale.
- `GET /api/shelf/cover?key=…`: a cover as JPEG.
- `PUT /api/shelf/cover?key=…`: replace a cover with an uploaded picture.
- `POST /api/shelf/cover/frame?key=…&at=…`: grab another frame for an item's cover, at `at` seconds.

### Television

The menu reads the shelf each time it opens, like the movies. A cover that is not made yet draws as a plain tile with the title.

## Phases

1. **Shelf package.** Model, load and save with the revision check, validation, resolving against the station's channels, scaled resume thresholds. Table tests.
2. **API and covers.** The endpoints above, frame grabs with ffmpeg at low priority, picture uploads normalised to JPEG. Handler tests with a fake grabber.
3. **Television.** Two-level menu, 16:9 tiles, the Movies folder, episodes playing with Movie Mode's controls and resume. Tests against the fake player.
4. **Web.** On Demand tab, PIN prompt remembered on the phone, library toggles, folders, covers, "Add to On Demand" on the guide. Vitest.
5. **On the Pi.** Deploy when nobody is watching, build a Tractor Ted folder, couch test.

## Later, not in this plan

- Putting something on the shelf that is not on any channel (paste a YouTube link). Today the way in is adding it to a channel's YAML first.
- Choosing the frame by scrubbing on the phone rather than "another frame".
