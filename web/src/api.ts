// Typed access to the Channel Three API.
//
// The contract lives in docs/integration/guide-api.md. Nothing here changes
// what is playing, and the page never asks it to tune. The one thing it writes
// is the On Demand shelf, which changes what channel 0's menu offers, and only
// with the upload PIN.

/** An airing reported by /api/now, with how far into it the broadcast is. */
export interface Airing {
  id: string
  title: string
  /** A song's artist. Absent for video. */
  artist?: string
  /** RFC 3339 with the station's local offset. */
  start: string
  /** RFC 3339. Cut at the 04:00 rollover, so this is not start plus duration. */
  end: string
  /** The item's own length in seconds. Never end minus start. */
  duration: number
  /** Seconds into the item. Always 0 for `next`. */
  offset: number
}

/** One slot of a channel's timeline from /api/guide. Carries no offset. */
export interface GuideSlot {
  id: string
  title: string
  /** A song's artist. Absent for video. */
  artist?: string
  start: string
  end: string
  duration: number
}

export interface NowChannel {
  id: string
  number: number
  name: string
  /** Null when the channel has no playable items. */
  now: Airing | null
  next: Airing | null
}

export interface NowResponse {
  /** The station's clock at the moment it answered. */
  time: string
  /** The channel id on air, or null while the station is still coming up. */
  tuned: string | null
  channels: NowChannel[]
}

export interface GuideChannel {
  id: string
  number: number
  name: string
  /** Empty, never null, for a channel with no playable items. */
  slots: GuideSlot[]
}

export interface GuideResponse {
  from: string
  to: string
  channels: GuideChannel[]
}

/** The default guide window, in hours. The API defaults to the same. */
export const defaultGuideHours = 6

/**
 * Reads one JSON endpoint and throws on anything but a 2xx.
 *
 * An error body is `{"error": "..."}`, so the station's own message is what
 * reaches the page when it has one. A body that is not that shape, which is
 * what a proxy in the way would return, falls back to the status line.
 */
async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, {
    signal,
    headers: { Accept: 'application/json' },
  })
  if (!response.ok) {
    throw new StationError(`${path}: ${await errorMessage(response)}`, response.status)
  }
  return (await response.json()) as T
}

/** A failed request, with its status so a caller can tell a wrong PIN (403) from a stale save (409). */
export class StationError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'StationError'
    this.status = status
  }
}

/** Pulls the station's error message out of a failed response. */
async function errorMessage(response: Response): Promise<string> {
  const status = `HTTP ${String(response.status)}`
  let body: string
  try {
    body = await response.text()
  } catch {
    // The body is gone, which tells us nothing the status does not.
    return status
  }
  try {
    const parsed: unknown = JSON.parse(body)
    if (
      typeof parsed === 'object' &&
      parsed !== null &&
      'error' in parsed &&
      typeof parsed.error === 'string'
    ) {
      return `${status}: ${parsed.error}`
    }
  } catch {
    // Not JSON. A proxy or a crash, and the status is the whole story.
  }
  return status
}

/** What is on now and next on every channel. */
export function fetchNow(signal?: AbortSignal): Promise<NowResponse> {
  return getJSON<NowResponse>('/api/now', signal)
}

/**
 * The next `hours` of every channel's timeline.
 *
 * The API accepts 1 to 48 whole hours and rejects anything else with a 400,
 * so the page only ever asks for its one default.
 */
export function fetchGuide(
  hours: number = defaultGuideHours,
  signal?: AbortSignal,
): Promise<GuideResponse> {
  return getJSON<GuideResponse>(`/api/guide?hours=${String(hours)}`, signal)
}

/** One video of /api/library. */
export interface LibraryItem {
  id: string
  title: string
  artist?: string
  duration: number
  on_shelf: boolean
}

export interface LibraryChannel {
  id: string
  number: number
  name: string
  /** Off the dial, so only On Demand can offer its videos. */
  hidden?: boolean
  /** Empty, never null. */
  items: LibraryItem[]
}

export interface LibraryResponse {
  channels: LibraryChannel[]
}

/** One video on the shelf. */
export interface ShelfItem {
  channel: string
  id: string
  /** The shelf's own title. Absent when the library's is used. */
  title?: string
  library_title: string
  duration: number
  /** The video has left the library. It stays on the shelf and comes back if the video does. */
  missing: boolean
  /** The cover's URL, or empty while it is being made. */
  cover: string
}

export interface ShelfFolder {
  /** Empty for a folder this page has just made; the station gives it one. */
  id: string
  name: string
  /** Its own picture, else its first episode's, else empty. */
  cover: string
  items: ShelfItem[]
}

/** One place at the top of the shelf: an item or a folder. */
export interface ShelfEntry {
  item?: ShelfItem
  folder?: ShelfFolder
}

export interface ShelfResponse {
  revision: number
  entries: ShelfEntry[]
  /** The box has a PIN, so the shelf can be changed. */
  editable: boolean
}

/** Which shelf: Lane's, opened with the upload PIN, or the grown-up one behind its own code. */
export type ShelfName = 'lane' | 'grownup'

/** The query naming a shelf, joined to a path that may already have one. */
function onShelf(path: string, shelf: ShelfName): string {
  if (shelf === 'lane') {
    return path
  }
  return `${path}${path.includes('?') ? '&' : '?'}shelf=${shelf}`
}

/** Every video the station can play, by channel, marked against one shelf. */
export function fetchLibrary(signal?: AbortSignal, shelf: ShelfName = 'lane'): Promise<LibraryResponse> {
  return getJSON<LibraryResponse>(onShelf('/api/library', shelf), signal)
}

/** An On Demand shelf. A box without the grown-up shelf answers 404 for it. */
export function fetchShelf(signal?: AbortSignal, shelf: ShelfName = 'lane'): Promise<ShelfResponse> {
  return getJSON<ShelfResponse>(onShelf('/api/shelf', shelf), signal)
}

/** Sends a change with the PIN and reads the JSON answer. */
async function change<T>(
  method: string,
  path: string,
  pin: string,
  body: BodyInit,
  contentType: string,
): Promise<T> {
  const response = await fetch(path, {
    method,
    body,
    headers: { Accept: 'application/json', 'Content-Type': contentType, 'X-Upload-PIN': pin },
  })
  if (!response.ok) {
    throw new StationError(`${path}: ${await errorMessage(response)}`, response.status)
  }
  return (await response.json()) as T
}

/**
 * Saves the shelf on top of `revision`, the one it was read at. A 409 means
 * another phone saved in between, and the shelf should be read again.
 */
export function saveShelf(
  revision: number,
  entries: ShelfEntry[],
  pin: string,
  shelf: ShelfName = 'lane',
): Promise<ShelfResponse> {
  return change<ShelfResponse>(
    'PUT',
    onShelf('/api/shelf', shelf),
    pin,
    JSON.stringify({ revision, entries: entriesToSave(entries) }),
    'application/json',
  )
}

/** The shelf as the station takes it: references and names, nothing it fills in itself. */
export function entriesToSave(entries: ShelfEntry[]): unknown[] {
  const item = (it: ShelfItem) => ({
    channel: it.channel,
    id: it.id,
    ...(it.title ? { title: it.title } : {}),
  })
  return entries.flatMap((entry): unknown[] => {
    if (entry.folder) {
      return [{ folder: { id: entry.folder.id, name: entry.folder.name, items: entry.folder.items.map(item) } }]
    }
    return entry.item ? [{ item: item(entry.item) }] : []
  })
}

/** Replaces a cover with a picture from the phone. */
export function uploadCover(
  key: string,
  picture: Blob,
  pin: string,
  shelf: ShelfName = 'lane',
): Promise<{ cover: string }> {
  return change<{ cover: string }>(
    'PUT',
    onShelf(`/api/shelf/cover?key=${encodeURIComponent(key)}`, shelf),
    pin,
    picture,
    picture.type || 'application/octet-stream',
  )
}

/** Makes an item's cover from the frame `at` seconds into its video. */
export function grabFrame(
  key: string,
  at: number,
  pin: string,
  shelf: ShelfName = 'lane',
): Promise<{ cover: string }> {
  return change<{ cover: string }>(
    'POST',
    onShelf(`/api/shelf/cover/frame?key=${encodeURIComponent(key)}&at=${at.toFixed(1)}`, shelf),
    pin,
    '',
    'text/plain',
  )
}

/** One pasted link on the grown-up shelf and how far it has got. */
export interface Download {
  key: number
  url: string
  title?: string
  state: 'queued' | 'downloading' | 'done' | 'failed'
  error?: string
  id?: string
}

/** What was pasted recently, newest first. */
export async function fetchDownloads(signal?: AbortSignal): Promise<Download[]> {
  const response = await getJSON<{ downloads: Download[] | null }>('/api/grownup/downloads', signal)
  return response.downloads ?? []
}

/** Queues a link to download onto the grown-up shelf. */
export function addLink(url: string, pin: string): Promise<Download> {
  return change<Download>('POST', '/api/grownup/downloads', pin, JSON.stringify({ url }), 'application/json')
}

/** Takes a downloaded video off the grown-up shelf and deletes it from the box. */
export function deleteGrownupVideo(id: string, pin: string): Promise<{ deleted: string }> {
  return change<{ deleted: string }>(
    'DELETE',
    `/api/grownup/videos?id=${encodeURIComponent(id)}`,
    pin,
    '',
    'text/plain',
  )
}

/** The hidden channel pasted links are downloaded into. */
export const grownupChannel = 'grownup'
