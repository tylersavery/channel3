// The channel list: one row per channel, in channel-number order.

import type { GuideChannel, GuideResponse, NowChannel, NowResponse } from './api'
import {
  advanceOffset,
  formatClock,
  laterSlots,
  progressFraction,
  remainingLabel,
} from './format'

export interface GuideProps {
  /** The last good /api/now response. */
  now: NowResponse
  /** The last good /api/guide response, or null before the first one lands. */
  guide: GuideResponse | null
  /** Seconds since `now` was fetched, so the bars move between refreshes. */
  elapsedSeconds: number
}

/** Every channel, lowest number first. */
export function Guide({ now, guide, elapsedSeconds }: GuideProps) {
  const channels = [...now.channels].sort((a, b) => a.number - b.number)
  const guideByID = new Map<string, GuideChannel>(
    (guide?.channels ?? []).map((channel) => [channel.id, channel]),
  )

  if (channels.length === 0) {
    return (
      <p className="empty">
        No channels are loaded. Check the channel config on the station.
      </p>
    )
  }

  return (
    <ul className="channels">
      {channels.map((channel) => (
        <li key={channel.id}>
          <ChannelRow
            channel={channel}
            guide={guideByID.get(channel.id)}
            guideLoaded={guide !== null}
            guideFrom={guide?.from}
            tuned={channel.id === now.tuned}
            elapsedSeconds={elapsedSeconds}
          />
        </li>
      ))}
    </ul>
  )
}

interface ChannelRowProps {
  channel: NowChannel
  guide: GuideChannel | undefined
  guideLoaded: boolean
  guideFrom: string | undefined
  tuned: boolean
  elapsedSeconds: number
}

function ChannelRow({
  channel,
  guide,
  guideLoaded,
  guideFrom,
  tuned,
  elapsedSeconds,
}: ChannelRowProps) {
  const { now, next } = channel
  return (
    <article
      className={tuned ? 'channel channel--tuned' : 'channel'}
      aria-current={tuned ? 'true' : undefined}
      data-testid={`channel-${channel.id}`}
    >
      <div className="channel__head">
        <span className="channel__number" data-testid="channel-number">
          <span className="sr-only">Channel </span>
          {channel.number}
        </span>
        <div className="channel__heading">
          <h2 className="channel__name">{channel.name}</h2>
          {tuned ? <p className="channel__badge">On the TV now</p> : null}
        </div>
      </div>

      {now === null ? (
        <p className="channel__standby">Please Stand By</p>
      ) : (
        <NowAiring
          title={now.title}
          offset={advanceOffset(now.offset, now.duration, elapsedSeconds)}
          duration={now.duration}
        />
      )}

      {next === null ? null : (
        <p className="channel__next">
          <span className="channel__nextlabel">Next</span>
          <span className="channel__nexttitle">{next.title}</span>
          <span className="channel__nexttime">at {formatClock(next.start)}</span>
        </p>
      )}

      <Later
        channel={channel}
        guide={guide}
        guideLoaded={guideLoaded}
        after={now?.end ?? guideFrom ?? ''}
      />
    </article>
  )
}

interface NowAiringProps {
  title: string
  offset: number
  duration: number
}

function NowAiring({ title, offset, duration }: NowAiringProps) {
  // The bar is offset over duration. It is never end minus start: an airing cut
  // at the 04:00 rollover is shorter on the clock than the video is long.
  const percent = Math.round(progressFraction(offset, duration) * 100)
  return (
    <>
      <p className="channel__title">{title}</p>
      <div
        className="progress"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        aria-label={`${String(percent)} percent through ${title}`}
      >
        <div className="progress__fill" style={{ width: `${String(percent)}%` }} />
      </div>
      <p className="channel__remaining">{remainingLabel(offset, duration)}</p>
    </>
  )
}

interface LaterProps {
  channel: NowChannel
  guide: GuideChannel | undefined
  /** False until the first /api/guide response lands. */
  guideLoaded: boolean
  after: string
}

/**
 * The per-channel disclosure of what comes after the current airing.
 *
 * Closed by default. The page is meant to answer "what is on" at a glance and
 * the rest is for whoever wants it.
 */
function Later({ channel, guide, guideLoaded, after }: LaterProps) {
  if (guide === undefined) {
    // Two different situations that both leave this channel without slots. The
    // first is the ordinary second after a load. The second means the two
    // endpoints disagree about which channels exist, which is worth saying
    // plainly rather than dressing up as "still loading".
    return (
      <details className="later">
        <summary>Later on {channel.name}</summary>
        <p className="later__empty">
          {guideLoaded
            ? 'This channel is not in the guide.'
            : 'The guide has not loaded yet.'}
        </p>
      </details>
    )
  }

  const { slots, truncated } = laterSlots(guide.slots, after)
  return (
    <details className="later">
      <summary>Later on {channel.name}</summary>
      {slots.length === 0 ? (
        <p className="later__empty">Nothing else is scheduled.</p>
      ) : (
        <>
          <ol className="later__list">
            {slots.map((slot, index) => (
              <li
                key={`${slot.start}-${slot.id}-${String(index)}`}
                className="later__slot"
              >
                <span className="later__time">{formatClock(slot.start)}</span>
                <span className="later__title">{slot.title}</span>
              </li>
            ))}
          </ol>
          {truncated ? <p className="later__more">and more after that</p> : null}
        </>
      )}
    </details>
  )
}
