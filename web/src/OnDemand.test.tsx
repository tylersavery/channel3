import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { LibraryResponse, ShelfEntry, ShelfResponse } from './api'
import { Guide } from './Guide'
import { OnDemand } from './OnDemand'
import nowFixture from './fixtures/now.json'
import type { NowResponse } from './api'
import type { ShelfControls } from './useShelf'

const library: LibraryResponse = {
  channels: [
    {
      id: 'farmtv',
      number: 4,
      name: 'Farm TV',
      items: [
        { id: 't1', title: 'Combine Time', duration: 1500, on_shelf: true },
        { id: 't2', title: 'Tumbling Time', duration: 1400, on_shelf: false },
      ],
    },
    {
      id: 'radio',
      number: 20,
      name: 'Radio',
      items: [{ id: 'r1', title: 'Help!', artist: 'The Beatles', duration: 140, on_shelf: false }],
    },
  ],
}

const shelfResponse: ShelfResponse = {
  revision: 3,
  editable: true,
  entries: [
    {
      folder: {
        id: 'f1',
        name: 'Tractor Ted',
        cover: '/api/shelf/cover?key=folder%3Af1&v=1',
        items: [
          { channel: 'farmtv', id: 't1', library_title: 'Combine Time', duration: 1500, missing: false, cover: '' },
        ],
      },
    },
    {
      item: {
        channel: 'sesame',
        id: 's1',
        title: 'Telly',
        library_title: 'Telly and the Fire Bell (1984)',
        duration: 3534,
        missing: true,
        cover: '',
      },
    },
  ],
}

function fakeControls(shelf: ShelfResponse = shelfResponse) {
  const save = vi.fn<(edit: (entries: ShelfEntry[]) => ShelfEntry[]) => Promise<void>>().mockResolvedValue()
  const controls: ShelfControls = {
    shelf,
    loadError: null,
    notice: null,
    busy: false,
    askingPin: false,
    save,
    run: vi.fn().mockResolvedValue(undefined),
    reload: vi.fn().mockResolvedValue(undefined),
    submitPin: vi.fn(),
    cancelPin: vi.fn(),
    dismissNotice: vi.fn(),
  }
  return { controls, save }
}

/** What the last save would have made of the shelf. */
function lastEdit(save: ReturnType<typeof fakeControls>['save']): ShelfEntry[] {
  const edit = save.mock.calls.at(-1)?.[0]
  if (edit === undefined) {
    throw new Error('nothing was saved')
  }
  return edit(shelfResponse.entries)
}

function stubLibrary() {
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>().mockImplementation(() =>
      Promise.resolve(new Response(JSON.stringify(library), { status: 200 })),
    ),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('OnDemand', () => {
  it('shows the shelf: folders with their videos, and what has left the library', async () => {
    stubLibrary()
    const { controls } = fakeControls()
    render(<OnDemand controls={controls} />)

    const folder = screen.getByTestId('folder-Tractor Ted')
    expect(within(folder).getByText('Folder · 1 video')).toBeInTheDocument()
    const episode = within(folder).getByTestId('item-farmtv/t1')
    expect(episode.querySelector('.row__title')).toHaveTextContent('Combine Time')
    expect(await within(episode).findByText('Farm TV · 25 min')).toBeInTheDocument()

    const telly = screen.getByTestId('item-sesame/s1')
    expect(telly.querySelector('.row__title')).toHaveTextContent('Telly')
    expect(within(telly).getByText('sesame · not in the library now')).toBeInTheDocument()
  })

  it('puts a library video on the shelf, into the folder chosen', async () => {
    stubLibrary()
    const { controls, save } = fakeControls()
    render(<OnDemand controls={controls} />)

    fireEvent.change(screen.getByLabelText('Add to'), { target: { value: 'f1' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Put on the shelf: Tumbling Time' }))
    const folder = lastEdit(save)[0]?.folder
    expect(folder?.items.map((it) => it.id)).toEqual(['t1', 't2'])

    fireEvent.click(screen.getByRole('button', { name: 'Take off the shelf: Combine Time' }))
    expect(lastEdit(save)[0]?.folder?.items).toEqual([])
  })

  it('leaves songs out of the library', async () => {
    stubLibrary()
    const { controls } = fakeControls()
    render(<OnDemand controls={controls} />)
    await screen.findByText(/4 Farm TV/)
    expect(screen.queryByText(/Radio/)).not.toBeInTheDocument()
  })

  it('moves and removes a video from its edit panel', () => {
    stubLibrary()
    const { controls, save } = fakeControls()
    render(<OnDemand controls={controls} />)

    fireEvent.click(screen.getByRole('button', { name: 'Edit Telly' }))
    const telly = screen.getByTestId('item-sesame/s1')
    fireEvent.click(within(telly).getByRole('button', { name: 'Up' }))
    expect(lastEdit(save)[0]?.item?.id).toBe('s1')

    fireEvent.change(within(telly).getByLabelText('Folder for Telly'), { target: { value: 'f1' } })
    expect(lastEdit(save)).toHaveLength(1)

    fireEvent.click(within(telly).getByRole('button', { name: 'Remove' }))
    expect(lastEdit(save)).toHaveLength(1)
  })

  it('says when the box cannot be changed', () => {
    stubLibrary()
    const { controls } = fakeControls({ ...shelfResponse, editable: false })
    render(<OnDemand controls={controls} />)
    expect(screen.getByText(/no upload PIN/)).toBeInTheDocument()
  })
})

describe('Guide save to On Demand', () => {
  it('offers to save what is on, and says when it is already saved', () => {
    const now = nowFixture as NowResponse
    const add = vi.fn()
    const trains = now.channels.find((channel) => channel.id === 'trains')
    const onShelf = new Set([`trains/${trains?.now?.id ?? ''}`])
    render(<Guide now={now} guide={null} elapsedSeconds={0} shelf={{ keys: onShelf, busy: false, add }} />)

    expect(within(screen.getByTestId('channel-trains')).getByText('On the On Demand shelf')).toBeInTheDocument()
    fireEvent.click(within(screen.getByTestId('channel-clips')).getByRole('button', { name: 'Save to On Demand' }))
    expect(add).toHaveBeenCalledWith('clips', expect.objectContaining({ id: expect.any(String) as unknown }))
    expect(within(screen.getByTestId('channel-standby')).queryByRole('button')).not.toBeInTheDocument()
  })
})
