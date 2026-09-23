// Typed access to the read-only Channel Three API.
//
// The contract lives in docs/integration/guide-api.md. Nothing here changes
// what is playing: the whole API is GET, and the page never asks it to tune.

/** An airing reported by /api/now, with how far into it the broadcast is. */
export interface Airing {
  id: string
  title: string
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
    throw new Error(`${path}: ${await errorMessage(response)}`)
  }
  return (await response.json()) as T
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
