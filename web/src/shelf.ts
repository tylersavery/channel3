// Edits to the On Demand shelf, as pure functions over its entries. Each
// returns a new list for the page to save; none of them talks to the station.

import type { ShelfEntry, ShelfItem } from './api'

/** Names one library video. */
export interface Ref {
  channel: string
  id: string
}

/** The key a video goes by, which is also its cover's key. */
export function refKey(ref: Ref): string {
  return `${ref.channel}/${ref.id}`
}

/** The key a folder's cover goes by. */
export function folderKey(id: string): string {
  return `folder:${id}`
}

/** What an item is called on the menu. */
export function itemTitle(item: ShelfItem): string {
  return item.title ?? item.library_title
}

function same(item: ShelfItem, ref: Ref): boolean {
  return item.channel === ref.channel && item.id === ref.id
}

/** Every video on the shelf, by key. */
export function shelfKeys(entries: ShelfEntry[]): Set<string> {
  const keys = new Set<string>()
  for (const entry of entries) {
    if (entry.item) {
      keys.add(refKey(entry.item))
    }
    for (const item of entry.folder?.items ?? []) {
      keys.add(refKey(item))
    }
  }
  return keys
}

/**
 * Puts a video on the shelf: at the end of a folder, or at the end of the
 * shelf when folder is null. A video already on the shelf is left where it is.
 */
export function addItem(entries: ShelfEntry[], item: ShelfItem, folder: string | null): ShelfEntry[] {
  if (shelfKeys(entries).has(refKey(item))) {
    return entries
  }
  if (folder === null || !entries.some((entry) => entry.folder?.id === folder)) {
    return [...entries, { item }]
  }
  return entries.map((entry) =>
    entry.folder?.id === folder
      ? { folder: { ...entry.folder, items: [...entry.folder.items, item] } }
      : entry,
  )
}

/** Takes a video off the shelf, wherever it is. */
export function removeItem(entries: ShelfEntry[], ref: Ref): ShelfEntry[] {
  return entries
    .filter((entry) => !(entry.item && same(entry.item, ref)))
    .map((entry) =>
      entry.folder
        ? { folder: { ...entry.folder, items: entry.folder.items.filter((it) => !same(it, ref)) } }
        : entry,
    )
}

/** Takes a folder and the videos in it off the shelf. */
export function removeFolder(entries: ShelfEntry[], id: string): ShelfEntry[] {
  return entries.filter((entry) => entry.folder?.id !== id)
}

/** Adds an empty folder at the top of the shelf, where it is easiest to find. */
export function addFolder(entries: ShelfEntry[], name: string): ShelfEntry[] {
  return [{ folder: { id: '', name: name.trim(), cover: '', items: [] } }, ...entries]
}

/** Renames a folder. */
export function renameFolder(entries: ShelfEntry[], id: string, name: string): ShelfEntry[] {
  return entries.map((entry) =>
    entry.folder?.id === id ? { folder: { ...entry.folder, name: name.trim() } } : entry,
  )
}

/** Gives a video a title of its own, or with a blank title goes back to the library's. */
export function renameItem(entries: ShelfEntry[], ref: Ref, title: string): ShelfEntry[] {
  const rename = (item: ShelfItem): ShelfItem => {
    if (!same(item, ref)) {
      return item
    }
    const trimmed = title.trim()
    const renamed: ShelfItem = { ...item, title: trimmed }
    if (trimmed === '' || trimmed === item.library_title) {
      delete renamed.title
    }
    return renamed
  }
  return entries.map((entry) =>
    entry.item
      ? { item: rename(entry.item) }
      : entry.folder
        ? { folder: { ...entry.folder, items: entry.folder.items.map(rename) } }
        : entry,
  )
}

function swap<T>(list: T[], index: number, delta: number): T[] {
  const target = index + delta
  if (index < 0 || target < 0 || target >= list.length) {
    return list
  }
  const next = [...list]
  ;[next[index], next[target]] = [next[target] as T, next[index] as T]
  return next
}

/** Moves the entry at index up (-1) or down (+1) the shelf. */
export function moveEntry(entries: ShelfEntry[], index: number, delta: number): ShelfEntry[] {
  return swap(entries, index, delta)
}

/** Moves a video up or down inside its folder. */
export function moveInFolder(entries: ShelfEntry[], folder: string, ref: Ref, delta: number): ShelfEntry[] {
  return entries.map((entry) => {
    if (entry.folder?.id !== folder) {
      return entry
    }
    const index = entry.folder.items.findIndex((it) => same(it, ref))
    return { folder: { ...entry.folder, items: swap(entry.folder.items, index, delta) } }
  })
}

/** Where a video is: the id of its folder, or null at the top of the shelf. */
export function folderOf(entries: ShelfEntry[], ref: Ref): string | null {
  for (const entry of entries) {
    if (entry.folder?.items.some((it) => same(it, ref))) {
      return entry.folder.id
    }
  }
  return null
}

/** Moves a video into a folder, or out to the end of the shelf when folder is null. */
export function moveToFolder(entries: ShelfEntry[], ref: Ref, folder: string | null): ShelfEntry[] {
  let item: ShelfItem | undefined
  for (const entry of entries) {
    item ??= entry.item && same(entry.item, ref) ? entry.item : entry.folder?.items.find((it) => same(it, ref))
  }
  if (item === undefined || folderOf(entries, ref) === folder) {
    return entries
  }
  return addItem(removeItem(entries, ref), item, folder)
}

/** Somewhere between a tenth and nine tenths of the way in, for "another frame". */
export function randomFrame(duration: number, random: () => number = Math.random): number {
  return duration * (0.1 + 0.8 * random())
}
