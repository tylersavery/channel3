// The whole page: a header with the station clock, the channel list, and a
// quiet line when the station cannot be reached.

import { useEffect, useMemo, useState } from 'react'
import { fetchGuide, fetchNow, type Airing, type GuideResponse, type NowResponse } from './api'
import { Guide, type GuideShelf } from './Guide'
import { OnDemand } from './OnDemand'
import { advancedISO, formatClock, formatClockWithSeconds } from './format'
import { addItem, shelfKeys } from './shelf'
import { useShelf, type ShelfControls } from './useShelf'

/** How often each endpoint is re-read. /api/guide is much the larger of the two. */
const nowIntervalMs = 30_000
const guideIntervalMs = 5 * 60_000
/** How often the progress bars and the station clock move between refreshes. */
const tickIntervalMs = 1_000

type View = 'guide' | 'ondemand'

/** The tab the address asks for, so On Demand can be bookmarked. */
function viewFromHash(): View {
  return window.location.hash === '#on-demand' ? 'ondemand' : 'guide'
}

export function App() {
  const [now, setNow] = useState<NowResponse | null>(null)
  const [guide, setGuide] = useState<GuideResponse | null>(null)
  const [nowError, setNowError] = useState<string | null>(null)
  const [guideError, setGuideError] = useState<string | null>(null)
  const [fetchedAt, setFetchedAt] = useState<number | null>(null)
  const [elapsedSeconds, setElapsedSeconds] = useState(0)
  const [view, setView] = useState<View>(viewFromHash)
  const controls = useShelf()

  useEffect(() => {
    const follow = () => {
      setView(viewFromHash())
    }
    window.addEventListener('hashchange', follow)
    return () => {
      window.removeEventListener('hashchange', follow)
    }
  }, [])

  const { shelf: shelfResponse, busy, save } = controls
  const guideShelf = useMemo<GuideShelf | undefined>(() => {
    if (!shelfResponse?.editable) {
      return undefined
    }
    return {
      keys: shelfKeys(shelfResponse.entries),
      busy,
      add: (channel: string, airing: Airing) => {
        const item = {
          channel,
          id: airing.id,
          library_title: airing.title,
          duration: airing.duration,
          missing: false,
          cover: '',
        }
        void save((entries) => addItem(entries, item, null))
      },
    }
  }, [shelfResponse, busy, save])

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
        <nav className="tabs" aria-label="Pages">
          <a className="tabs__tab" href="#guide" aria-current={view === 'guide' ? 'page' : undefined}>
            Guide
          </a>
          <a className="tabs__tab" href="#on-demand" aria-current={view === 'ondemand' ? 'page' : undefined}>
            On Demand
          </a>
        </nav>
        {now === null ? null : (
          <p className="header__clock">
            <span className="sr-only">Station clock </span>
            <time dateTime={advancedISO(now.time, elapsedSeconds)}>
              {formatClockWithSeconds(now.time, elapsedSeconds)}
            </time>
          </p>
        )}
      </header>


      {view === 'ondemand' ? <OnDemand controls={controls} /> : null}

      {view === 'ondemand' || (nowError === null && guideError === null) ? null : (
        <p className="notice" role="status">
          {now === null
            ? `Cannot reach the station. ${nowError ?? guideError ?? ''}`
            : `Cannot reach the station. Showing ${formatClock(now.time)}.`}
        </p>
      )}

      {view === 'ondemand' ? null : now === null ? (
        nowError === null ? (
          <p className="empty">Loading the guide.</p>
        ) : null
      ) : (
        <Guide now={now} guide={guide} elapsedSeconds={elapsedSeconds} shelf={guideShelf} />
      )}

      {/* Pinned to the bottom of the screen, so the PIN and any trouble are
          seen wherever on the page the change was asked for. */}
      {controls.askingPin || controls.notice !== null ? (
        <div className="dock">
          {controls.notice === null ? null : (
            <p className="notice dock__notice" role="status">
              {controls.notice}{' '}
              <button type="button" className="button button--quiet dock__dismiss" onClick={controls.dismissNotice}>
                OK
              </button>
            </p>
          )}
          {controls.askingPin ? <PinPrompt controls={controls} /> : null}
        </div>
      ) : null}
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

/** Asks for the upload PIN once, the first time something on the shelf is changed. */
function PinPrompt({ controls }: { controls: ShelfControls }) {
  const [value, setValue] = useState('')
  const submit = (event: { preventDefault: () => void }) => {
    event.preventDefault()
    controls.submitPin(value)
  }
  return (
    <form className="pin" onSubmit={submit}>
      <label className="field field--grow">
        <span className="field__label">The code, to change On Demand</span>
        <input
          className="input"
          type="password"
          inputMode="numeric"
          autoComplete="off"
          autoFocus
          value={value}
          onChange={(event) => {
            setValue(event.target.value)
          }}
        />
      </label>
      <button type="submit" className="button button--primary">
        OK
      </button>
      <button type="button" className="button button--quiet" onClick={controls.cancelPin}>
        Cancel
      </button>
    </form>
  )
}
