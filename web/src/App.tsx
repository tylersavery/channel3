// The whole page: a header with the station clock, the channel list, and a
// quiet line when the station cannot be reached.

import { useEffect, useState } from 'react'
import { fetchGuide, fetchNow, type GuideResponse, type NowResponse } from './api'
import { Guide } from './Guide'
import { advancedISO, formatClock, formatClockWithSeconds } from './format'

/** How often each endpoint is re-read. /api/guide is much the larger of the two. */
const nowIntervalMs = 30_000
const guideIntervalMs = 5 * 60_000
/** How often the progress bars and the station clock move between refreshes. */
const tickIntervalMs = 1_000

export function App() {
  const [now, setNow] = useState<NowResponse | null>(null)
  const [guide, setGuide] = useState<GuideResponse | null>(null)
  const [nowError, setNowError] = useState<string | null>(null)
  const [guideError, setGuideError] = useState<string | null>(null)
  const [fetchedAt, setFetchedAt] = useState<number | null>(null)
  const [elapsedSeconds, setElapsedSeconds] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    const load = async () => {
      try {
        const response = await fetchNow(controller.signal)
        setNow(response)
        setFetchedAt(Date.now())
        setElapsedSeconds(0)
        setNowError(null)
      } catch (cause) {
        if (controller.signal.aborted) {
          return
        }
        // The last good response stays on screen. A station that has stopped
        // is exactly when someone wants to see what was on a moment ago.
        setNowError(describeError(cause))
      }
    }

    void load()
    const timer = setInterval(() => {
      void load()
    }, nowIntervalMs)
    return () => {
      controller.abort()
      clearInterval(timer)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()

    const load = async () => {
      try {
        setGuide(await fetchGuide(undefined, controller.signal))
        setGuideError(null)
      } catch (cause) {
        if (controller.signal.aborted) {
          return
        }
        // Tracked apart from the now error so that a guide that is five
        // minutes between tries cannot keep re-raising a line the working
        // thirty second poll has already cleared.
        setGuideError(describeError(cause))
      }
    }

    void load()
    const timer = setInterval(() => {
      void load()
    }, guideIntervalMs)
    return () => {
      controller.abort()
      clearInterval(timer)
    }
  }, [])

  useEffect(() => {
    if (fetchedAt === null) {
      return undefined
    }
    // The offset is reset where the fetch lands; this only keeps it moving.
    const timer = setInterval(() => {
      setElapsedSeconds((Date.now() - fetchedAt) / 1000)
    }, tickIntervalMs)
    return () => {
      clearInterval(timer)
    }
  }, [fetchedAt])

  return (
    <div className="page">
      <header className="header">
        <h1 className="header__title">Channel Three</h1>
        {now === null ? null : (
          <p className="header__clock">
            <span className="sr-only">Station clock </span>
            <time dateTime={advancedISO(now.time, elapsedSeconds)}>
              {formatClockWithSeconds(now.time, elapsedSeconds)}
            </time>
          </p>
        )}
      </header>

      {nowError === null && guideError === null ? null : (
        <p className="notice" role="status">
          {now === null
            ? `Cannot reach the station. ${nowError ?? guideError ?? ''}`
            : `Cannot reach the station. Showing ${formatClock(now.time)}.`}
        </p>
      )}

      {now === null ? (
        nowError === null ? (
          <p className="empty">Loading the guide.</p>
        ) : null
      ) : (
        <Guide now={now} guide={guide} elapsedSeconds={elapsedSeconds} />
      )}
    </div>
  )
}

/** A message for the inline error line, whatever the fetch threw. */
function describeError(cause: unknown): string {
  if (cause instanceof Error) {
    return cause.message
  }
  return String(cause)
}
