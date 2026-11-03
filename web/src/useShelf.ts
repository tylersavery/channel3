// The On Demand shelf as the page holds it: read from the station, changed
// with the upload PIN, and read again when another phone got there first.

import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchShelf, saveShelf, StationError, type ShelfEntry, type ShelfResponse } from './api'

const pinStorageKey = 'channel3.upload-pin'

/** How long to wait before looking again for covers the station is still making. */
const coverRetryMs = 3000
/** How many times to look before giving up on a cover that will not come. */
const coverRetries = 10

function readPin(): string {
  try {
    return localStorage.getItem(pinStorageKey) ?? ''
  } catch {
    // Private browsing can refuse storage. The PIN is then asked for once a visit.
    return ''
  }
}

function writePin(pin: string): void {
  try {
    if (pin === '') {
      localStorage.removeItem(pinStorageKey)
    } else {
      localStorage.setItem(pinStorageKey, pin)
    }
  } catch {
    // As above: without storage the PIN lives only as long as the page.
  }
}

/** A change that needs the PIN. */
export type Change = (pin: string) => Promise<void>

export interface ShelfControls {
  /** The shelf, or null before it has loaded or on a box without one. */
  shelf: ShelfResponse | null
  /** Why the shelf could not be read, when it could not. */
  loadError: string | null
  /** The last change's trouble, for a line above the shelf. */
  notice: string | null
  busy: boolean
  /** True while the page is waiting for the PIN to carry on with a change. */
  askingPin: boolean
  /** Saves the shelf as edit makes it from the current one. */
  save: (edit: (entries: ShelfEntry[]) => ShelfEntry[]) => Promise<void>
  /** Runs a change that needs the PIN. */
  run: (change: Change) => Promise<void>
  /** Reads the shelf again, after a cover has changed. */
  reload: () => Promise<void>
  submitPin: (pin: string) => void
  cancelPin: () => void
  dismissNotice: () => void
}

function describe(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

/** Whether any video still on the library is waiting for its cover. */
function waitingForCovers(shelf: ShelfResponse): boolean {
  return shelf.entries.some((entry) =>
    [...(entry.item ? [entry.item] : []), ...(entry.folder?.items ?? [])].some(
      (item) => !item.missing && item.cover === '',
    ),
  )
}

export function useShelf(): ShelfControls {
  const [shelf, setShelf] = useState<ShelfResponse | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [pin, setPin] = useState(readPin)
  const [askingPin, setAskingPin] = useState(false)
  const pending = useRef<Change | null>(null)
  const coverLooks = useRef(0)

  const reload = useCallback(async () => {
    try {
      setShelf(await fetchShelf())
      setLoadError(null)
    } catch (cause) {
      setLoadError(describe(cause))
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const load = async () => {
      try {
        const response = await fetchShelf(controller.signal)
        setShelf(response)
        setLoadError(null)
      } catch (cause) {
        if (!controller.signal.aborted) {
          setLoadError(describe(cause))
        }
      }
    }
    void load()
    return () => {
      controller.abort()
    }
  }, [])

  useEffect(() => {
    if (shelf === null || !waitingForCovers(shelf) || coverLooks.current >= coverRetries) {
      return undefined
    }
    const timer = setTimeout(() => {
      coverLooks.current += 1
      void reload()
    }, coverRetryMs)
    return () => {
      clearTimeout(timer)
    }
  }, [shelf, reload])

  const attempt = useCallback(
    async (withPin: string, change: Change) => {
      setBusy(true)
      try {
        await change(withPin)
        setNotice(null)
        coverLooks.current = 0
      } catch (cause) {
        if (cause instanceof StationError && cause.status === 403) {
          writePin('')
          setPin('')
          pending.current = change
          setAskingPin(true)
          setNotice('That code was not right.')
        } else if (cause instanceof StationError && cause.status === 409) {
          await reload()
          setNotice('The shelf was changed on another phone, so it has been reloaded. Try that again.')
        } else {
          setNotice(describe(cause))
        }
      } finally {
        setBusy(false)
      }
    },
    [reload],
  )

  const run = useCallback(
    async (change: Change) => {
      if (pin === '') {
        pending.current = change
        setAskingPin(true)
        return
      }
      await attempt(pin, change)
    },
    [attempt, pin],
  )

  const save = useCallback(
    (edit: (entries: ShelfEntry[]) => ShelfEntry[]) =>
      run(async (withPin) => {
        if (shelf === null) {
          return
        }
        setShelf(await saveShelf(shelf.revision, edit(shelf.entries), withPin))
      }),
    [run, shelf],
  )

  const submitPin = useCallback(
    (value: string) => {
      const trimmed = value.trim()
      if (trimmed === '') {
        return
      }
      writePin(trimmed)
      setPin(trimmed)
      setAskingPin(false)
      const change = pending.current
      pending.current = null
      if (change !== null) {
        void attempt(trimmed, change)
      }
    },
    [attempt],
  )

  const cancelPin = useCallback(() => {
    pending.current = null
    setAskingPin(false)
  }, [])

  const dismissNotice = useCallback(() => {
    setNotice(null)
  }, [])

  return { shelf, loadError, notice, busy, askingPin, save, run, reload, submitPin, cancelPin, dismissNotice }
}
