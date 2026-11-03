// The On Demand tab: the shelf channel 0 offers after the code, and every
// video in the library with a switch to put it there.

import { useEffect, useMemo, useState } from 'react'
import {
  fetchLibrary,
  grabFrame,
  uploadCover,
  type LibraryChannel,
  type LibraryItem,
  type ShelfEntry,
  type ShelfFolder,
  type ShelfItem,
} from './api'
import { formatDuration } from './format'
import {
  addFolder,
  addItem,
  folderKey,
  itemTitle,
  moveEntry,
  moveInFolder,
  moveToFolder,
  randomFrame,
  refKey,
  removeFolder,
  removeItem,
  renameFolder,
  renameItem,
  shelfKeys,
} from './shelf'
import type { ShelfControls } from './useShelf'

export interface OnDemandProps {
  controls: ShelfControls
}

export function OnDemand({ controls }: OnDemandProps) {
  const { shelf, loadError } = controls
  const [library, setLibrary] = useState<LibraryChannel[] | null>(null)
  const [libraryError, setLibraryError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    fetchLibrary(controller.signal)
      .then((response) => {
        setLibrary(response.channels)
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) {
          setLibraryError(cause instanceof Error ? cause.message : String(cause))
        }
      })
    return () => {
      controller.abort()
    }
  }, [])

  const channelNames = useMemo(
    () => new Map((library ?? []).map((channel) => [channel.id, channel.name])),
    [library],
  )

  if (shelf === null) {
    return <p className="empty">{loadError === null ? 'Loading the shelf.' : `Cannot read the shelf. ${loadError}`}</p>
  }

  return (
    <div className="ondemand">
      <p className="ondemand__intro">
        What channel 0 shows after the code, in this order. Movies come first, in a folder of their own.
      </p>
      {shelf.editable ? null : (
        <p className="notice" role="status">
          This box has no upload PIN, so the shelf cannot be changed from here.
        </p>
      )}
      <Shelf controls={controls} entries={shelf.entries} channelNames={channelNames} />
      <Library
        controls={controls}
        entries={shelf.entries}
        channels={library}
        error={libraryError}
      />
    </div>
  )
}

interface ShelfProps {
  controls: ShelfControls
  entries: ShelfEntry[]
  channelNames: Map<string, string>
}

function Shelf({ controls, entries, channelNames }: ShelfProps) {
  const folders = entries.flatMap((entry) => (entry.folder ? [entry.folder] : []))

  const newFolder = () => {
    const name = window.prompt('Name the folder, for the show it holds')
    if (name !== null && name.trim() !== '') {
      void controls.save((current) => addFolder(current, name))
    }
  }

  return (
    <section className="section" aria-labelledby="shelf-heading">
      <div className="section__head">
        <h2 id="shelf-heading" className="section__title">
          On the shelf
        </h2>
        <button type="button" className="button" onClick={newFolder} disabled={controls.busy}>
          New folder
        </button>
      </div>
      {entries.length === 0 ? (
        <p className="empty">
          Nothing yet. Add videos from the library below, or from the Guide while they are on.
        </p>
      ) : (
        <ol className="shelf">
          {entries.map((entry, index) => (
            <li key={entryKey(entry, index)}>
              {entry.folder ? (
                <FolderCard
                  controls={controls}
                  folder={entry.folder}
                  index={index}
                  count={entries.length}
                  folders={folders}
                  channelNames={channelNames}
                />
              ) : entry.item ? (
                <ItemRow
                  controls={controls}
                  item={entry.item}
                  folder={null}
                  folders={folders}
                  channelNames={channelNames}
                  onUp={index > 0 ? () => controls.save((current) => moveEntry(current, index, -1)) : undefined}
                  onDown={
                    index < entries.length - 1
                      ? () => controls.save((current) => moveEntry(current, index, 1))
                      : undefined
                  }
                />
              ) : null}
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

/** A stable React key for an entry. A folder just made has no id yet. */
function entryKey(entry: ShelfEntry, index: number): string {
  if (entry.item) {
    return refKey(entry.item)
  }
  const id = entry.folder?.id ?? ''
  return id === '' ? `new-folder-${String(index)}` : `folder-${id}`
}

interface FolderCardProps {
  controls: ShelfControls
  folder: ShelfFolder
  index: number
  count: number
  folders: ShelfFolder[]
  channelNames: Map<string, string>
}

function FolderCard({ controls, folder, index, count, folders, channelNames }: FolderCardProps) {
  const [open, setOpen] = useState(false)
  const { save, busy } = controls

  const rename = () => {
    const name = window.prompt('Rename the folder', folder.name)
    if (name !== null && name.trim() !== '') {
      void save((current) => renameFolder(current, folder.id, name))
    }
  }
  const remove = () => {
    const episodes = folder.items.length === 1 ? '1 video' : `${String(folder.items.length)} videos`
    if (window.confirm(`Take ${folder.name} and its ${episodes} off the shelf? They stay in the library.`)) {
      void save((current) => removeFolder(current, folder.id))
    }
  }

  return (
    <article className="folder" data-testid={`folder-${folder.name}`}>
      <button
        type="button"
        className="row"
        aria-expanded={open}
        aria-label={`Edit ${folder.name}`}
        onClick={() => {
          setOpen(!open)
        }}
      >
        <Cover src={folder.cover} title={folder.name} />
        <span className="row__text">
          <span className="row__title">{folder.name}</span>
          <span className="row__meta">
            Folder · {folder.items.length === 1 ? '1 video' : `${String(folder.items.length)} videos`}
          </span>
        </span>
        <span className="row__edit" aria-hidden="true">
          {open ? 'Done' : 'Edit'}
        </span>
      </button>
      {open ? (
        <div className="actions">
          <button
            type="button"
            className="button"
            disabled={busy || index === 0}
            onClick={() => void save((current) => moveEntry(current, index, -1))}
          >
            Up
          </button>
          <button
            type="button"
            className="button"
            disabled={busy || index === count - 1}
            onClick={() => void save((current) => moveEntry(current, index, 1))}
          >
            Down
          </button>
          <button type="button" className="button" disabled={busy} onClick={rename}>
            Rename
          </button>
          {folder.id === '' ? null : (
            <PictureButton controls={controls} coverKey={folderKey(folder.id)} />
          )}
          <button type="button" className="button button--danger" disabled={busy} onClick={remove}>
            Remove
          </button>
        </div>
      ) : null}
      {folder.items.length === 0 ? (
        <p className="folder__empty">Empty. Add videos to it from the library below.</p>
      ) : (
        <ol className="folder__items">
          {folder.items.map((item, position) => (
            <li key={refKey(item)}>
              <ItemRow
                controls={controls}
                item={item}
                folder={folder.id}
                folders={folders}
                channelNames={channelNames}
                onUp={
                  position > 0 ? () => save((current) => moveInFolder(current, folder.id, item, -1)) : undefined
                }
                onDown={
                  position < folder.items.length - 1
                    ? () => save((current) => moveInFolder(current, folder.id, item, 1))
                    : undefined
                }
              />
            </li>
          ))}
        </ol>
      )}
    </article>
  )
}

interface ItemRowProps {
  controls: ShelfControls
  item: ShelfItem
  /** The folder it is in, or null at the top of the shelf. */
  folder: string | null
  folders: ShelfFolder[]
  channelNames: Map<string, string>
  onUp: (() => Promise<void>) | undefined
  onDown: (() => Promise<void>) | undefined
}

function ItemRow({ controls, item, folder, folders, channelNames, onUp, onDown }: ItemRowProps) {
  const [open, setOpen] = useState(false)
  const { save, run, reload, busy } = controls
  const title = itemTitle(item)

  const rename = () => {
    const next = window.prompt('Call it this on the TV (leave it empty for its own title)', title)
    if (next !== null) {
      void save((current) => renameItem(current, item, next))
    }
  }
  const anotherFrame = () => {
    void run(async (pin) => {
      await grabFrame(refKey(item), randomFrame(item.duration), pin)
      await reload()
    })
  }

  return (
    <div className="item" data-testid={`item-${refKey(item)}`}>
      <button
        type="button"
        className="row"
        aria-expanded={open}
        aria-label={`Edit ${title}`}
        onClick={() => {
          setOpen(!open)
        }}
      >
        <Cover src={item.cover} title={title} />
        <span className="row__text">
          <span className="row__title">{title}</span>
          <span className="row__meta">
            {channelNames.get(item.channel) ?? item.channel}
            {item.missing ? ' · not in the library now' : ` · ${formatDuration(item.duration)}`}
          </span>
        </span>
        <span className="row__edit" aria-hidden="true">
          {open ? 'Done' : 'Edit'}
        </span>
      </button>
      {open ? (
        <div className="actions">
          <button type="button" className="button" disabled={busy || onUp === undefined} onClick={() => void onUp?.()}>
            Up
          </button>
          <button
            type="button"
            className="button"
            disabled={busy || onDown === undefined}
            onClick={() => void onDown?.()}
          >
            Down
          </button>
          <button type="button" className="button" disabled={busy} onClick={rename}>
            Rename
          </button>
          <label className="field">
            <span className="sr-only">Folder for {title}</span>
            <select
              className="select"
              value={folder ?? ''}
              disabled={busy}
              onChange={(event) => {
                const target = event.target.value === '' ? null : event.target.value
                void save((current) => moveToFolder(current, item, target))
              }}
            >
              <option value="">Not in a folder</option>
              {folders
                .filter((f) => f.id !== '')
                .map((f) => (
                  <option key={f.id} value={f.id}>
                    In {f.name}
                  </option>
                ))}
            </select>
          </label>
          <button type="button" className="button" disabled={busy || item.missing} onClick={anotherFrame}>
            Another frame
          </button>
          <PictureButton controls={controls} coverKey={refKey(item)} />
          <button
            type="button"
            className="button button--danger"
            disabled={busy}
            onClick={() => void save((current) => removeItem(current, item))}
          >
            Remove
          </button>
        </div>
      ) : null}
    </div>
  )
}

interface PictureButtonProps {
  controls: ShelfControls
  coverKey: string
}

/** Replaces a cover with a picture chosen on the phone. */
function PictureButton({ controls, coverKey }: PictureButtonProps) {
  const { run, reload, busy } = controls
  return (
    <label className={busy ? 'button button--disabled' : 'button'}>
      Use a picture
      <input
        type="file"
        accept="image/jpeg,image/png"
        className="sr-only"
        disabled={busy}
        onChange={(event) => {
          const file = event.target.files?.[0]
          event.target.value = ''
          if (file === undefined) {
            return
          }
          void run(async (pin) => {
            await uploadCover(coverKey, file, pin)
            await reload()
          })
        }}
      />
    </label>
  )
}

function Cover({ src, title }: { src: string; title: string }) {
  if (src === '') {
    return (
      <span className="cover cover--empty" aria-hidden="true">
        {title}
      </span>
    )
  }
  return <img className="cover" src={src} alt="" loading="lazy" />
}

interface LibraryProps {
  controls: ShelfControls
  entries: ShelfEntry[]
  channels: LibraryChannel[] | null
  error: string | null
}

/** Songs play on the radio channels and are left out: On Demand is for watching. */
function watchable(item: LibraryItem): boolean {
  return item.artist === undefined
}

function Library({ controls, entries, channels, error }: LibraryProps) {
  const [target, setTarget] = useState('')
  const [search, setSearch] = useState('')
  const onShelf = shelfKeys(entries)
  const folders = entries.flatMap((entry) => (entry.folder && entry.folder.id !== '' ? [entry.folder] : []))
  const query = search.trim().toLowerCase()

  const toggle = (channel: LibraryChannel, item: LibraryItem) => {
    const ref = { channel: channel.id, id: item.id }
    if (onShelf.has(refKey(ref))) {
      void controls.save((current) => removeItem(current, ref))
      return
    }
    const shelved: ShelfItem = {
      ...ref,
      library_title: item.title,
      duration: item.duration,
      missing: false,
      cover: '',
    }
    const folder = folders.some((f) => f.id === target) ? target : null
    void controls.save((current) => addItem(current, shelved, folder))
  }

  const preventSubmit = (event: { preventDefault: () => void }) => {
    event.preventDefault()
  }

  return (
    <section className="section" aria-labelledby="library-heading">
      <h2 id="library-heading" className="section__title">
        Add from the library
      </h2>
      <form className="filters" onSubmit={preventSubmit}>
        <label className="field field--grow">
          <span className="field__label">Add to</span>
          <select
            className="select"
            value={target}
            onChange={(event) => {
              setTarget(event.target.value)
            }}
          >
            <option value="">The shelf, on its own</option>
            {folders.map((f) => (
              <option key={f.id} value={f.id}>
                {f.name}
              </option>
            ))}
          </select>
        </label>
        <label className="field field--grow">
          <span className="field__label">Find</span>
          <input
            className="input"
            type="search"
            placeholder="Title"
            value={search}
            onChange={(event) => {
              setSearch(event.target.value)
            }}
          />
        </label>
      </form>
      {channels === null ? (
        <p className="empty">{error === null ? 'Loading the library.' : `Cannot read the library. ${error}`}</p>
      ) : (
        <div className="library">
          {channels.map((channel) => {
            const items = channel.items
              .filter(watchable)
              .filter((item) => query === '' || item.title.toLowerCase().includes(query))
            if (items.length === 0) {
              return null
            }
            const shelved = items.filter((item) => onShelf.has(refKey({ channel: channel.id, id: item.id }))).length
            return (
              <details key={channel.id} className="later library__channel" open={query !== ''}>
                <summary>
                  {String(channel.number)} {channel.name}
                  <span className="library__count">
                    {' '}
                    · {String(items.length)}
                    {shelved > 0 ? `, ${String(shelved)} on the shelf` : ''}
                  </span>
                </summary>
                <ul className="library__list">
                  {items.map((item) => {
                    const on = onShelf.has(refKey({ channel: channel.id, id: item.id }))
                    return (
                      <li key={item.id} className="library__item">
                        <span className="library__title">
                          {item.title}
                          <span className="library__duration"> · {formatDuration(item.duration)}</span>
                        </span>
                        <button
                          type="button"
                          className={on ? 'button button--on' : 'button'}
                          aria-pressed={on}
                          aria-label={`${on ? 'Take off the shelf' : 'Put on the shelf'}: ${item.title}`}
                          disabled={controls.busy}
                          onClick={() => {
                            toggle(channel, item)
                          }}
                        >
                          {on ? 'On the shelf' : 'Add'}
                        </button>
                      </li>
                    )
                  })}
                </ul>
              </details>
            )
          })}
        </div>
      )}
    </section>
  )
}
