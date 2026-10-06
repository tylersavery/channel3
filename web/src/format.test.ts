import { describe, expect, it } from 'vitest'
import type { GuideSlot, NowResponse } from './api'
import {
  itemLabel,
  advancedISO,
  advanceOffset,
  formatClock,
  formatClockWithSeconds,
  laterSlots,
  minutesLeft,
  progressFraction,
  remainingLabel,
} from './format'
import nowFixture from './fixtures/now.json'

// The zone is pinned to America/Toronto by the test script and by the vitest
// config, so these strings are the same wherever the tests run.
describe('formatClock', () => {
  it('renders an instant as a local wall clock time', () => {
    expect(formatClock('2026-09-23T19:04:11-04:00')).toBe('7:04 PM')
    expect(formatClock('2026-09-23T03:58:11-04:00')).toBe('3:58 AM')
  })

  it('converts an instant given in another offset', () => {
    expect(formatClock('2026-09-23T23:04:11Z')).toBe('7:04 PM')
  })

  it('renders a dash rather than Invalid Date', () => {
    expect(formatClock('not a time')).toBe('—')
  })
})

describe('formatClockWithSeconds', () => {
  it('shows the station clock to the second', () => {
    expect(formatClockWithSeconds('2026-09-23T03:58:11-04:00')).toBe('3:58:11 AM')
  })

  it('carries the station clock forward between refreshes', () => {
    expect(formatClockWithSeconds('2026-09-23T03:58:11-04:00', 49)).toBe('3:59:00 AM')
  })

  it('ignores a negative or unusable advance', () => {
    expect(formatClockWithSeconds('2026-09-23T03:58:11-04:00', -30)).toBe('3:58:11 AM')
    expect(formatClockWithSeconds('2026-09-23T03:58:11-04:00', Number.NaN)).toBe(
      '3:58:11 AM',
    )
  })
})

describe('advancedISO', () => {
  it('names the same instant the header renders', () => {
    expect(advancedISO('2026-09-23T03:58:11-04:00', 49)).toBe('2026-09-23T07:59:00.000Z')
  })

  it('leaves an instant alone when there is nothing to advance', () => {
    expect(advancedISO('2026-09-23T03:58:11-04:00')).toBe('2026-09-23T07:58:11.000Z')
  })

  it('hands back an unparseable string untouched', () => {
    expect(advancedISO('not a time', 30)).toBe('not a time')
  })
})

describe('progressFraction', () => {
  it('is offset over duration', () => {
    expect(progressFraction(300, 600)).toBeCloseTo(0.5, 10)
  })

  it('clamps to 0 and 1', () => {
    expect(progressFraction(-5, 600)).toBe(0)
    expect(progressFraction(900, 600)).toBe(1)
    expect(progressFraction(600, 600)).toBe(1)
  })

  it('is 0 for a duration that cannot be divided by', () => {
    expect(progressFraction(10, 0)).toBe(0)
    expect(progressFraction(10, -60)).toBe(0)
    expect(progressFraction(10, Number.NaN)).toBe(0)
    expect(progressFraction(Number.NaN, 60)).toBe(0)
  })
})

describe('minutesLeft', () => {
  it('rounds up so a part minute still counts', () => {
    expect(minutesLeft(0, 600)).toBe(10)
    expect(minutesLeft(540, 600)).toBe(1)
    expect(minutesLeft(599, 600)).toBe(1)
    expect(minutesLeft(480, 600)).toBe(2)
  })

  it('reaches 0 only when the item is over, and never goes below', () => {
    expect(minutesLeft(600, 600)).toBe(0)
    expect(minutesLeft(900, 600)).toBe(0)
  })

  it('is 0 on unusable numbers rather than NaN', () => {
    expect(minutesLeft(Number.NaN, 600)).toBe(0)
    expect(minutesLeft(0, Number.NaN)).toBe(0)
  })
})

describe('remainingLabel', () => {
  it('reads as a sentence at every boundary', () => {
    expect(remainingLabel(0, 600)).toBe('10 min left')
    expect(remainingLabel(599, 600)).toBe('1 min left')
    expect(remainingLabel(600, 600)).toBe('ending now')
  })
})

describe('advanceOffset', () => {
  it('moves the offset forward by the seconds since the fetch', () => {
    expect(advanceOffset(100, 600, 30)).toBe(130)
  })

  it('never runs past the end of the item', () => {
    expect(advanceOffset(590, 600, 30)).toBe(600)
  })

  it('ignores an advance that is not a positive number', () => {
    expect(advanceOffset(100, 600, -5)).toBe(100)
    expect(advanceOffset(100, 600, Number.NaN)).toBe(100)
  })
})

describe('laterSlots', () => {
  const slots: GuideSlot[] = [
    { id: 'a', title: 'A', start: '2026-09-23T03:50:00-04:00', end: '2026-09-23T04:00:00-04:00', duration: 900 },
    { id: 'b', title: 'B', start: '2026-09-23T04:00:00-04:00', end: '2026-09-23T04:10:00-04:00', duration: 600 },
    { id: 'c', title: 'C', start: '2026-09-23T04:10:00-04:00', end: '2026-09-23T04:20:00-04:00', duration: 600 },
  ]

  it('drops the slot that is already airing', () => {
    const later = laterSlots(slots, '2026-09-23T04:00:00-04:00')
    expect(later.slots.map((slot) => slot.id)).toEqual(['b', 'c'])
    expect(later.truncated).toBe(false)
  })

  it('caps the list and says that it did', () => {
    const capped = laterSlots(slots, '2026-09-23T03:00:00-04:00', 2)
    expect(capped.slots).toHaveLength(2)
    expect(capped.truncated).toBe(true)

    const none = laterSlots(slots, '2026-09-23T03:00:00-04:00', 0)
    expect(none.slots).toHaveLength(0)
    expect(none.truncated).toBe(true)
  })

  it('reports no truncation when the cap is exactly the length', () => {
    const exact = laterSlots(slots, '2026-09-23T03:00:00-04:00', 3)
    expect(exact.slots).toHaveLength(3)
    expect(exact.truncated).toBe(false)
  })

  it('keeps everything when the reference time is unusable', () => {
    expect(laterSlots(slots, '').slots).toHaveLength(3)
  })
})

describe('the fixture captured from the running station', () => {
  const now = nowFixture as NowResponse

  it('has an airing cut at the 04:00 rollover', () => {
    const trains = now.channels.find((channel) => channel.id === 'trains')
    const airing = trains?.now
    if (airing === undefined || airing === null) {
      throw new Error('the trains channel should be airing something')
    }
    const span = (new Date(airing.end).getTime() - new Date(airing.start).getTime()) / 1000
    expect(span).toBeLessThan(airing.duration)

    // The progress bar must use offset over duration. End minus start would put
    // this airing at three quarters through a video that is a quarter played.
    expect(progressFraction(airing.offset, airing.duration)).toBeCloseTo(0.242, 3)
    expect(progressFraction(airing.offset, span)).toBeGreaterThan(0.7)
    expect(remainingLabel(airing.offset, airing.duration)).toBe('18 min left')
  })
})

describe('itemLabel', () => {
  it('is the title alone for video', () => {
    expect(itemLabel({ title: 'Little Bear: Hiccups' })).toBe('Little Bear: Hiccups')
  })
  it('adds the artist for a song', () => {
    expect(itemLabel({ title: 'Help!', artist: 'The Beatles' })).toBe('Help! · The Beatles')
  })
})
