// Formatting and arithmetic for the guide page.
//
// Everything here is pure so the page's one piece of judgement, how far into an
// airing the broadcast is between refreshes, can be tested without a browser.

import type { GuideSlot } from './api'

const clockFormat = new Intl.DateTimeFormat(undefined, {
  hour: 'numeric',
  minute: '2-digit',
})

const clockWithSecondsFormat = new Intl.DateTimeFormat(undefined, {
  hour: 'numeric',
  minute: '2-digit',
  second: '2-digit',
})

/**
 * Renders an RFC 3339 instant as a wall clock time in the viewer's zone.
 *
 * The station sends its own offset, so a phone in another zone sees its own
 * time for the same instant, which is the right answer for a clock.
 * An unparseable string renders as an em space rather than "Invalid Date".
 */
export function formatClock(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) {
    return '—'
  }
  return clockFormat.format(at)
}

/**
 * The same, with seconds, for the station clock in the header.
 *
 * `plusSeconds` carries the station's own clock forward between refreshes, so
 * the header ticks instead of jumping thirty seconds at a time. The base is
 * always the time the station reported, never the browser's.
 */
export function formatClockWithSeconds(iso: string, plusSeconds = 0): string {
  const at = advance(iso, plusSeconds)
  if (at === null) {
    return '—'
  }
  return clockWithSecondsFormat.format(at)
}

/**
 * The same instant `formatClockWithSeconds` renders, as a machine readable
 * string for a `<time datetime>` attribute.
 *
 * The rendered text and the attribute have to move together or a screen reader
 * announces a time the sighted reader stopped seeing thirty seconds ago. The
 * result is the same instant in UTC, which `<time>` accepts. A string that
 * cannot be parsed comes back exactly as the station sent it.
 */
export function advancedISO(iso: string, plusSeconds = 0): string {
  const at = advance(iso, plusSeconds)
  if (at === null) {
    return iso
  }
  return at.toISOString()
}

/** The instant `iso` names, moved forward by a usable number of seconds. */
function advance(iso: string, plusSeconds: number): Date | null {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) {
    return null
  }
  if (!Number.isFinite(plusSeconds) || plusSeconds <= 0) {
    return at
  }
  return new Date(at.getTime() + plusSeconds * 1000)
}

/**
 * How far through an airing the broadcast is, as 0 to 1.
 *
 * The fraction is offset over duration and never end minus start: an airing cut
 * at the 04:00 rollover has a shorter span on the clock than the video is long,
 * and a bar drawn from the span would run to the end of a video that is only
 * half played.
 *
 * A duration that is zero, negative or not a number gives 0, because a bar that
 * jumps to full on bad data reads as "nearly over" and that is worse than a bar
 * that sits still.
 */
export function progressFraction(offset: number, duration: number): number {
  if (!Number.isFinite(duration) || duration <= 0 || !Number.isFinite(offset)) {
    return 0
  }
  return Math.min(1, Math.max(0, offset / duration))
}

/**
 * Whole minutes left in an airing, rounded up.
 *
 * Rounding up means a video with one second to go still says "1 min left"
 * rather than "0", and the count only reaches 0 when the item is genuinely
 * over. Never negative, because a stale response can put the offset past the
 * end.
 */
export function minutesLeft(offset: number, duration: number): number {
  if (!Number.isFinite(duration) || !Number.isFinite(offset)) {
    return 0
  }
  const remaining = duration - offset
  if (remaining <= 0) {
    return 0
  }
  return Math.ceil(remaining / 60)
}

/** The minutes-left line under a progress bar. */
export function remainingLabel(offset: number, duration: number): string {
  const minutes = minutesLeft(offset, duration)
  if (minutes === 0) {
    return 'ending now'
  }
  if (minutes === 1) {
    return '1 min left'
  }
  return `${String(minutes)} min left`
}

/**
 * Moves an offset forward by the seconds elapsed since the station reported it.
 *
 * The page refreshes every 30 s and the bar advances every second in between,
 * so between refreshes this is the only thing moving. It is clamped at the
 * item's duration: past that the station has moved on and the page has not
 * heard yet, and a bar past full is a lie either way.
 */
export function advanceOffset(
  offset: number,
  duration: number,
  elapsedSeconds: number,
): number {
  if (!Number.isFinite(offset)) {
    return 0
  }
  const elapsed = Number.isFinite(elapsedSeconds) && elapsedSeconds > 0 ? elapsedSeconds : 0
  const advanced = offset + elapsed
  if (!Number.isFinite(duration) || duration <= 0) {
    return Math.max(0, advanced)
  }
  return Math.min(duration, Math.max(0, advanced))
}

/** How many later slots a channel's disclosure will list. */
export const laterSlotLimit = 40

/** A capped run of slots, and whether the cap left anything out. */
export interface LaterSlots {
  slots: GuideSlot[]
  /** True when the cap hid slots the window really contains. */
  truncated: boolean
}

/**
 * The slots of a channel that start at or after `after`.
 *
 * The API's first slot is whatever is already airing, which the row above
 * already shows, so the disclosure starts at the one after it. The list is
 * capped because a channel of twenty second clips has over a thousand slots in
 * a six hour window and none of them are worth a DOM node. The cap is reported
 * rather than applied silently, so the page can say the list is not the whole
 * window instead of implying the channel stops there.
 */
export function laterSlots(
  slots: readonly GuideSlot[],
  after: string,
  limit: number = laterSlotLimit,
): LaterSlots {
  const from = new Date(after).getTime()
  const later = Number.isNaN(from)
    ? [...slots]
    : slots.filter((slot) => {
        const start = new Date(slot.start).getTime()
        return Number.isNaN(start) || start >= from
      })
  const capped = Math.max(0, limit)
  return { slots: later.slice(0, capped), truncated: later.length > capped }
}
