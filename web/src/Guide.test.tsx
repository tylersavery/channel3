import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { GuideResponse, NowResponse } from './api'
import { Guide } from './Guide'
import guideFixture from './fixtures/guide.json'
import nowFixture from './fixtures/now.json'

const now = nowFixture as NowResponse
const guide: GuideResponse = guideFixture

function renderGuide(elapsedSeconds = 0, withGuide: GuideResponse | null = guide) {
  return render(
    <Guide now={now} guide={withGuide} elapsedSeconds={elapsedSeconds} />,
  )
}

describe('Guide', () => {
  it('lists channels in number order whatever order the API sent', () => {
    // The fixture is deliberately 5, 9, 3.
    expect(now.channels.map((channel) => channel.number)).toEqual([5, 9, 3])

    renderGuide()
    const numbers = screen
      .getAllByTestId('channel-number')
      .map((node) => node.textContent)
    expect(numbers).toEqual(['Channel 3', 'Channel 5', 'Channel 9'])
    const names = screen
      .getAllByRole('heading', { level: 2 })
      .map((node) => node.textContent)
    expect(names).toEqual(['Train TV', 'Short Clips', 'Nothing At All'])
  })

  it('highlights and labels the tuned channel and nothing else', () => {
    renderGuide()
    const tuned = screen.getByTestId('channel-clips')
    expect(tuned).toHaveClass('channel--tuned')
    expect(tuned).toHaveAttribute('aria-current', 'true')
    expect(within(tuned).getByText('On the TV now')).toBeInTheDocument()

    const other = screen.getByTestId('channel-trains')
    expect(other).not.toHaveClass('channel--tuned')
    expect(other).not.toHaveAttribute('aria-current')
    expect(screen.getAllByText('On the TV now')).toHaveLength(1)
  })

  it('shows Please Stand By for a channel with nothing to play', () => {
    renderGuide()
    const standby = screen.getByTestId('channel-standby')
    expect(within(standby).getByText('Please Stand By')).toBeInTheDocument()
    expect(within(standby).queryByRole('progressbar')).not.toBeInTheDocument()
    expect(within(standby).getByText('Nothing else is scheduled.')).toBeInTheDocument()
  })

  it('draws the progress bar from offset over duration, not from end minus start', () => {
    renderGuide()
    const trains = screen.getByTestId('channel-trains')
    // 331.2 of 1368 seconds is 24 percent. The airing is cut at the 04:00
    // rollover, so end minus start would say 75 percent.
    expect(within(trains).getByRole('progressbar')).toHaveAttribute(
      'aria-valuenow',
      '24',
    )
    expect(within(trains).getByText('18 min left')).toBeInTheDocument()
  })

  it('advances the bar between refreshes', () => {
    const { rerender } = renderGuide()
    rerender(<Guide now={now} guide={guide} elapsedSeconds={120} />)
    const trains = screen.getByTestId('channel-trains')
    // 331.2 plus 120 of 1368 seconds is 33 percent.
    expect(within(trains).getByRole('progressbar')).toHaveAttribute(
      'aria-valuenow',
      '33',
    )
    expect(within(trains).getByText('16 min left')).toBeInTheDocument()
  })

  it('shows what is on now and next with the start time', () => {
    renderGuide()
    const trains = screen.getByTestId('channel-trains')
    // Both titles come round again in the later list, so these are scoped to
    // the now and next lines.
    expect(
      within(trains).getByText('Switching Yard', { selector: '.channel__title' }),
    ).toBeInTheDocument()
    expect(
      within(trains).getByText('Steam Engines', { selector: '.channel__nexttitle' }),
    ).toBeInTheDocument()
    expect(within(trains).getByText('at 4:00 AM')).toBeInTheDocument()
  })

  it('lists the rest of the day under the later disclosure', () => {
    renderGuide()
    const trains = screen.getByTestId('channel-trains')
    const later = within(trains).getByRole('group')
    expect(within(later).getByText('Later on Train TV')).toBeInTheDocument()

    const slots = within(later).getAllByRole('listitem')
    expect(slots.length).toBeGreaterThan(1)
    // The airing already shown above the disclosure is not repeated in it.
    expect(slots[0]?.textContent).toBe('4:00 AMSteam Engines')
    expect(slots[1]?.textContent).toBe('4:10 AMLevel Crossings')
  })

  it('says when the cap has hidden the rest of the window', () => {
    renderGuide()
    // The clips channel runs 49 slots after the one on air; the cap is 40.
    const clips = within(screen.getByTestId('channel-clips')).getByRole('group')
    expect(within(clips).getAllByRole('listitem')).toHaveLength(40)
    expect(within(clips).getByText('and more after that')).toBeInTheDocument()

    // Trains runs 20, so nothing is hidden and nothing is claimed.
    const trains = within(screen.getByTestId('channel-trains')).getByRole('group')
    expect(within(trains).getAllByRole('listitem')).toHaveLength(20)
    expect(within(trains).queryByText('and more after that')).not.toBeInTheDocument()
  })

  it('tells a missing channel apart from a guide that has not arrived', () => {
    const partial: GuideResponse = {
      ...guide,
      channels: guide.channels.filter((channel) => channel.id !== 'trains'),
    }
    renderGuide(0, partial)
    const trains = screen.getByTestId('channel-trains')
    expect(within(trains).getByText('This channel is not in the guide.')).toBeInTheDocument()
    expect(
      within(trains).queryByText('The guide has not loaded yet.'),
    ).not.toBeInTheDocument()
  })

  it('says so when the guide has not arrived yet', () => {
    renderGuide(0, null)
    const trains = screen.getByTestId('channel-trains')
    expect(within(trains).getByText('The guide has not loaded yet.')).toBeInTheDocument()
    expect(
      within(trains).queryByText('This channel is not in the guide.'),
    ).not.toBeInTheDocument()
    // The now and next lines do not wait on the guide.
    expect(within(trains).getByText('Switching Yard')).toBeInTheDocument()
  })

  it('says so when no channels are configured', () => {
    render(
      <Guide
        now={{ time: now.time, tuned: null, channels: [] }}
        guide={null}
        elapsedSeconds={0}
      />,
    )
    expect(screen.getByText(/No channels are loaded/)).toBeInTheDocument()
  })
})
