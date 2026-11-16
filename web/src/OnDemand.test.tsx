import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
      id: 'grownup',
      number: 90,
      name: 'Grown-ups',
      hidden: true,
      items: [{ id: 'g1', title: 'How Bridges Work', duration: 900, on_shelf: false }],
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

function fakeControls(shelf: ShelfResponse = shelfResponse, name: ShelfControls['name'] = 'lane') {
  const save = vi.fn<(edit: (entries: ShelfEntry[]) => ShelfEntry[]) => Promise<void>>().mockResolvedValue()
  const controls: ShelfControls = {
    name,
    shelf,
    absent: false,
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

/** The station as the Grown-ups tab sees it: the library, the downloads, and every change it is sent. */
function stubGrownupStation() {
  const calls: { method: string; url: string; body: string }[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>().mockImplementation((input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const method = init?.method ?? 'GET'
      calls.push({ method, url, body: typeof init?.body === 'string' ? init.body : '' })
      let body: unknown = library
      if (url.startsWith('/api/grownup/downloads')) {
        body =
          method === 'POST'
            ? { key: 1, url: 'https://youtu.be/x', state: 'queued' }
            : { downloads: [{ key: 7, url: 'https://youtu.be/bad', state: 'failed', error: 'that is a playlist' }] }
      } else if (url.startsWith('/api/grownup/videos')) {
        body = { deleted: 'g1' }
      }
      return Promise.resolve(new Response(JSON.stringify(body), { status: method === 'POST' ? 202 : 200 }))
    }),
  )
  return calls
}

const grownupShelf: ShelfResponse = {
  revision: 1,
  editable: true,
  entries: [
    { item: { channel: 'grownup', id: 'g1', library_title: 'How Bridges Work', duration: 900, missing: false, cover: '' } },
    { item: { channel: 'farmtv', id: 't2', library_title: 'Tumbling Time', duration: 1400, missing: false, cover: '' } },
  ],
}

/** Controls whose run hands the change the grown-up code at once. */
function grownupControls() {
  const { controls, save } = fakeControls(grownupShelf, 'grownup')
  controls.run = vi.fn(async (change: (pin: string) => Promise<void>) => {
    await change('4217')
  })
  return { controls, save }
}

describe('Grown-ups', () => {
  it('keeps grown-up downloads off Lane\'s library', async () => {
    stubLibrary()
    const { controls } = fakeControls()
    render(<OnDemand controls={controls} />)
    await screen.findByText(/4 Farm TV/)
    expect(screen.queryByText(/Grown-ups/)).not.toBeInTheDocument()
  })

  it('pastes a link with the grown-up code, and shows why a download failed', async () => {
    const calls = stubGrownupStation()
    const { controls } = grownupControls()
    render(<OnDemand controls={controls} />)

    expect(await screen.findByText('Failed: that is a playlist')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('YouTube link'), {
      target: { value: '  https://www.youtube.com/watch?v=x  ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'POST')).toBe(true)
    })
    const post = calls.find((c) => c.method === 'POST')
    expect(post?.url).toBe('/api/grownup/downloads')
    expect(JSON.parse(post?.body ?? '{}')).toEqual({ url: 'https://www.youtube.com/watch?v=x' })
    await waitFor(() => {
      expect(screen.getByLabelText('YouTube link')).toHaveValue('')
    })
  })

  it('deletes a downloaded video, and only takes a library video off the shelf', async () => {
    const calls = stubGrownupStation()
    vi.stubGlobal('confirm', vi.fn(() => true))
    const { controls, save } = grownupControls()
    render(<OnDemand controls={controls} />)

    fireEvent.click(screen.getByRole('button', { name: 'Edit How Bridges Work' }))
    fireEvent.click(within(screen.getByTestId('item-grownup/g1')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/grownup/videos?id=g1')).toBe(true)
    })
    expect(save).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Edit Tumbling Time' }))
    fireEvent.click(within(screen.getByTestId('item-farmtv/t2')).getByRole('button', { name: 'Remove' }))
    expect(save).toHaveBeenCalled()
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
