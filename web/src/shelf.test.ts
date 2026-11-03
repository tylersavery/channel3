import { describe, expect, it } from 'vitest'
import { entriesToSave, type ShelfEntry, type ShelfItem } from './api'
import {
  addFolder,
  addItem,
  folderOf,
  moveEntry,
  moveInFolder,
  moveToFolder,
  randomFrame,
  removeFolder,
  removeItem,
  renameFolder,
  renameItem,
  shelfKeys,
} from './shelf'

function video(channel: string, id: string, title = `${id} title`): ShelfItem {
  return { channel, id, library_title: title, duration: 600, missing: false, cover: '' }
}

const ted1 = video('farmtv', 't1', 'Combine Time')
const ted2 = video('farmtv', 't2', 'Tumbling Time')
const telly = video('sesame', 's1', 'Telly and the Fire Bell (1984)')

function shelf(): ShelfEntry[] {
  return [
    { folder: { id: 'f1', name: 'Tractor Ted', cover: '', items: [ted1, ted2] } },
    { item: telly },
  ]
}

describe('shelf edits', () => {
  it('knows what is on the shelf', () => {
    expect([...shelfKeys(shelf())]).toEqual(['farmtv/t1', 'farmtv/t2', 'sesame/s1'])
  })

  it('adds a video to the end of the shelf or of a folder, once', () => {
    const elmo = video('sesame', 's2')
    expect(addItem(shelf(), elmo, null).at(-1)).toEqual({ item: elmo })
    const inFolder = addItem(shelf(), elmo, 'f1')
    expect(inFolder[0]?.folder?.items.map((it) => it.id)).toEqual(['t1', 't2', 's2'])
    expect(addItem(shelf(), telly, 'f1')).toEqual(shelf())
    // A folder that has gone puts it at the end of the shelf instead.
    expect(addItem(shelf(), elmo, 'gone').at(-1)).toEqual({ item: elmo })
  })

  it('removes a video wherever it is', () => {
    expect(shelfKeys(removeItem(shelf(), ted2)).has('farmtv/t2')).toBe(false)
    expect(removeItem(shelf(), telly)).toHaveLength(1)
  })

  it('removes a folder with its videos', () => {
    expect(removeFolder(shelf(), 'f1')).toEqual([{ item: telly }])
  })

  it('adds and renames folders', () => {
    const added = addFolder(shelf(), '  Elmo ')
    expect(added[0]?.folder).toEqual({ id: '', name: 'Elmo', cover: '', items: [] })
    expect(renameFolder(shelf(), 'f1', 'Ted')[0]?.folder?.name).toBe('Ted')
  })

  it('gives a video its own title and takes it away again', () => {
    const renamed = renameItem(shelf(), telly, ' Telly ')
    expect(renamed[1]?.item?.title).toBe('Telly')
    expect(renameItem(renamed, telly, '')[1]?.item).toEqual(telly)
    expect(renameItem(renamed, telly, telly.library_title)[1]?.item?.title).toBeUndefined()
  })

  it('moves entries and videos up and down, and not past the ends', () => {
    expect(moveEntry(shelf(), 1, -1)[0]).toEqual({ item: telly })
    expect(moveEntry(shelf(), 1, 1)).toEqual(shelf())
    const moved = moveInFolder(shelf(), 'f1', ted2, -1)
    expect(moved[0]?.folder?.items.map((it) => it.id)).toEqual(['t2', 't1'])
  })

  it('moves a video into and out of a folder', () => {
    const inside = moveToFolder(shelf(), telly, 'f1')
    expect(inside).toHaveLength(1)
    expect(folderOf(inside, telly)).toBe('f1')
    const out = moveToFolder(inside, ted1, null)
    expect(out.at(-1)).toEqual({ item: ted1 })
    expect(folderOf(out, ted1)).toBeNull()
  })

  it('saves references and names only', () => {
    const renamed = renameItem(shelf(), telly, 'Telly')
    expect(entriesToSave(renamed)).toEqual([
      { folder: { id: 'f1', name: 'Tractor Ted', items: [{ channel: 'farmtv', id: 't1' }, { channel: 'farmtv', id: 't2' }] } },
      { item: { channel: 'sesame', id: 's1', title: 'Telly' } },
    ])
  })

  it('picks another frame inside the video', () => {
    expect(randomFrame(600, () => 0)).toBe(60)
    expect(randomFrame(600, () => 1)).toBe(540)
  })
})
