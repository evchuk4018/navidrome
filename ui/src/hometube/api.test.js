import { beforeEach, describe, expect, it, vi } from 'vitest'
import config from '../config'
import {
  getHomeTubeFeed,
  homeTubeApiPath,
  homeTubeRequest,
  refreshHomeTubeFeed,
} from './api'

describe('HomeTube API client', () => {
  beforeEach(() => {
    config.homeTubeBaseURL = '/hometube'
    vi.restoreAllMocks()
  })

  it('normalizes the mount, API segment, and required slash before query strings', () => {
    expect(homeTubeApiPath('/feed?limit=40')).toBe(
      '/hometube/api/feed/?limit=40',
    )
    expect(homeTubeApiPath('channels/')).toBe('/hometube/api/channels/')
    config.homeTubeBaseURL = '/hometube/'
    expect(homeTubeApiPath('/api/feed?limit=1')).toBe(
      '/hometube/api/feed/?limit=1',
    )
    config.homeTubeBaseURL = '/hometube/api'
    expect(homeTubeApiPath('/feed')).toBe('/hometube/api/feed/')
  })

  it('returns parsed JSON without adding a Navidrome authorization header', async () => {
    const fetchMock = vi.spyOn(window, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ videos: [] }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    )

    await expect(getHomeTubeFeed()).resolves.toEqual({ videos: [] })
    expect(fetchMock).toHaveBeenCalledWith('/hometube/api/feed/?limit=40', {
      cache: 'no-store',
    })
    expect(fetchMock.mock.calls[0][1].headers).toBeUndefined()
  })

  it('reports HTTP and network failures with an actionable message', async () => {
    vi.spyOn(window, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: 'database warming up' }), {
        status: 503,
        statusText: 'Service Unavailable',
        headers: { 'content-type': 'application/json' },
      }),
    )
    await expect(homeTubeRequest('/feed')).rejects.toThrow(
      'HomeTube request failed (503): database warming up',
    )

    vi.restoreAllMocks()
    vi.spyOn(window, 'fetch').mockRejectedValue(new Error('offline'))
    await expect(refreshHomeTubeFeed([], 40)).rejects.toThrow(
      'Unable to reach HomeTube: offline',
    )
  })
})
