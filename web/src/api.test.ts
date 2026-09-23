import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchGuide, fetchNow } from './api'
import guideFixture from './fixtures/guide.json'
import nowFixture from './fixtures/now.json'

function stubFetch(response: Response) {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(response)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchNow', () => {
  it('reads /api/now', async () => {
    const fetchMock = stubFetch(jsonResponse(nowFixture))
    await expect(fetchNow()).resolves.toMatchObject({ tuned: 'clips' })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/now')
  })

  it('throws with the station message on a 4xx', async () => {
    stubFetch(jsonResponse({ error: 'no such channel' }, 404))
    await expect(fetchNow()).rejects.toThrow('/api/now: HTTP 404: no such channel')
  })

  it('throws on a body that is not the error shape', async () => {
    stubFetch(new Response('<html>gateway</html>', { status: 502 }))
    await expect(fetchNow()).rejects.toThrow('/api/now: HTTP 502')
  })
})

describe('fetchGuide', () => {
  it('asks for six hours by default', async () => {
    const fetchMock = stubFetch(jsonResponse(guideFixture))
    await expect(fetchGuide()).resolves.toMatchObject({ from: guideFixture.from })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/guide?hours=6')
  })

  it('passes a different window through', async () => {
    const fetchMock = stubFetch(jsonResponse(guideFixture))
    await fetchGuide(12)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/guide?hours=12')
  })
})
