import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  clearQuickPickCache,
  ensureQuickPickImpressions,
  getQuickPickCacheEntry,
  loadQuickPickSection,
} from './cache'

describe('Quick Pick page cache', () => {
  beforeEach(() => clearQuickPickCache())

  it('caches successful empty values and detaches nested response records', async () => {
    const response = { items: [], metadata: { source: 'server' } }
    const loader = vi.fn().mockResolvedValue(response)
    await loadQuickPickSection('user-1', 'discovery', loader)

    response.metadata.source = 'resource-store'
    const cached = await loadQuickPickSection('user-1', 'discovery', loader)
    expect(cached).toEqual({ items: [], metadata: { source: 'server' } })
    expect(loader).toHaveBeenCalledOnce()
  })

  it('shares one pending request for a user and section', async () => {
    let resolve
    const loader = vi.fn().mockReturnValue(
      new Promise((result) => {
        resolve = result
      }),
    )
    const first = loadQuickPickSection('user-1', 'playlists', loader)
    const second = loadQuickPickSection('user-1', 'playlists', loader)
    expect(second).toBe(first)
    await Promise.resolve()
    expect(loader).toHaveBeenCalledOnce()
    resolve([])
    await expect(first).resolves.toEqual([])
  })

  it('prevents a cleared session response from populating a later cache', async () => {
    let resolveOld
    const old = loadQuickPickSection(
      'user-1',
      'discovery',
      () => new Promise((resolve) => (resolveOld = resolve)),
    )
    clearQuickPickCache()
    await loadQuickPickSection('user-1', 'discovery', () => ({ viewId: 'new' }))
    resolveOld({ viewId: 'old' })
    await expect(old).resolves.toEqual({ viewId: 'old' })
    expect(getQuickPickCacheEntry('user-1', 'discovery').value).toEqual({
      viewId: 'new',
    })
  })

  it('retries only a failed section while retaining another section snapshot', async () => {
    const discoveryLoader = vi.fn().mockResolvedValue({ viewId: 'view-1' })
    const playlistLoader = vi
      .fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce([{ id: 'playlist-1' }])
    await loadQuickPickSection('user-1', 'discovery', discoveryLoader)
    await expect(
      loadQuickPickSection('user-1', 'playlists', playlistLoader),
    ).rejects.toThrow('offline')
    await expect(
      loadQuickPickSection('user-1', 'playlists', playlistLoader, {
        retry: true,
      }),
    ).resolves.toEqual([{ id: 'playlist-1' }])
    expect(getQuickPickCacheEntry('user-1', 'discovery').value).toEqual({
      viewId: 'view-1',
    })
    expect(discoveryLoader).toHaveBeenCalledOnce()
    expect(playlistLoader).toHaveBeenCalledTimes(2)
  })

  it('starts both sections over after a full page cache reset', async () => {
    const discoveryLoader = vi.fn().mockResolvedValue({ viewId: 'view-1' })
    const playlistLoader = vi.fn().mockResolvedValue([])
    await loadQuickPickSection('user-1', 'discovery', discoveryLoader)
    await loadQuickPickSection('user-1', 'playlists', playlistLoader)
    clearQuickPickCache()
    await loadQuickPickSection('user-1', 'discovery', discoveryLoader)
    await loadQuickPickSection('user-1', 'playlists', playlistLoader)
    expect(discoveryLoader).toHaveBeenCalledTimes(2)
    expect(playlistLoader).toHaveBeenCalledTimes(2)
  })

  it('claims one impression request for a cached discovery view', async () => {
    await loadQuickPickSection('user-1', 'discovery', () => ({
      viewId: 'view-1',
    }))
    const firstRequest = Promise.resolve()
    const createFirst = vi.fn(() => firstRequest)
    const createSecond = vi.fn(() => Promise.resolve())
    expect(
      ensureQuickPickImpressions('user-1', 'view-1', ['track:a'], createFirst),
    ).toBe(firstRequest)
    expect(
      ensureQuickPickImpressions('user-1', 'view-1', ['track:a'], createSecond),
    ).toBe(firstRequest)
    expect(createFirst).toHaveBeenCalledOnce()
    expect(createSecond).not.toHaveBeenCalled()
  })
})
