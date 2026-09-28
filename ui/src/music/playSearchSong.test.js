import { beforeEach, describe, expect, it, vi } from 'vitest'
import { resolveSearchSongForPlayback } from './playSearchSong'

const mocks = vi.hoisted(() => ({
  createDownload: vi.fn(),
  getDownload: vi.fn(),
  search: vi.fn(),
}))

vi.mock('./provider', () => mocks)

const request = {
  sourceId: 'recording-1',
  title: 'Seed Song',
  artist: 'Seed Artist',
}

const options = (overrides = {}) => ({
  dataProvider: {
    getOne: vi.fn().mockResolvedValue({ data: { id: 'local-1' } }),
  },
  isCurrent: () => true,
  onStatus: vi.fn(),
  delay: vi.fn().mockResolvedValue(),
  ...overrides,
})

describe('resolveSearchSongForPlayback', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((mock) => mock.mockReset())
  })

  it('loads an existing local song without downloading', async () => {
    const services = options()
    const song = await resolveSearchSongForPlayback(
      { ...request, localMediaFileId: 'local-1' },
      services,
    )
    expect(song.id).toBe('local-1')
    expect(services.dataProvider.getOne).toHaveBeenCalledWith('song', {
      id: 'local-1',
    })
    expect(mocks.createDownload).not.toHaveBeenCalled()
  })

  it('waits for its prioritized job and a resolved library ID', async () => {
    mocks.createDownload.mockResolvedValue({ id: 'job-1', status: 'queued' })
    mocks.getDownload
      .mockResolvedValueOnce({ id: 'job-1', status: 'running' })
      .mockResolvedValueOnce({ id: 'job-1', status: 'succeeded' })
      .mockResolvedValueOnce({
        id: 'job-1',
        status: 'succeeded',
        mediaFileId: 'local-1',
      })
    const services = options()
    const song = await resolveSearchSongForPlayback(request, services)

    expect(mocks.createDownload).toHaveBeenCalledWith('song', 'recording-1', {
      playNow: true,
    })
    expect(mocks.getDownload).toHaveBeenCalledTimes(3)
    expect(services.onStatus.mock.calls.map(([status]) => status)).toEqual([
      'queued',
      'running',
      'succeeded',
      'succeeded',
    ])
    expect(song.id).toBe('local-1')
  })

  it('does not fetch or play after a newer selection', async () => {
    mocks.createDownload.mockResolvedValue({ id: 'job-1', status: 'queued' })
    let current = true
    const services = options({
      isCurrent: () => current,
      delay: async () => {
        current = false
      },
    })
    expect(await resolveSearchSongForPlayback(request, services)).toBeNull()
    expect(mocks.getDownload).not.toHaveBeenCalled()
    expect(services.dataProvider.getOne).not.toHaveBeenCalled()
  })

  it('does not play a failed download', async () => {
    mocks.createDownload.mockResolvedValue({
      id: 'job-1',
      status: 'failed',
      error: 'Source unavailable',
    })
    const services = options()
    await expect(
      resolveSearchSongForPlayback(request, services),
    ).rejects.toThrow('Source unavailable')
    expect(services.dataProvider.getOne).not.toHaveBeenCalled()
  })

  it('resolves a library race after the server returns conflict', async () => {
    mocks.createDownload.mockRejectedValue({ status: 409 })
    mocks.search.mockResolvedValue({
      results: [
        {
          kind: 'song',
          song: { id: 'recording-1', localMediaFileId: 'local-1' },
        },
      ],
    })
    const services = options()
    expect(await resolveSearchSongForPlayback(request, services)).toEqual({
      id: 'local-1',
    })
    expect(mocks.getDownload).not.toHaveBeenCalled()
  })

  it('stops waiting if a completed download stays unindexed for 90 seconds', async () => {
    mocks.createDownload.mockResolvedValue({ id: 'job-1', status: 'succeeded' })
    mocks.getDownload.mockResolvedValue({ id: 'job-1', status: 'succeeded' })
    let time = 0
    const services = options({
      now: () => time,
      delay: async () => {
        time += 90_000
      },
    })
    await expect(
      resolveSearchSongForPlayback(request, services),
    ).rejects.toThrow('Downloaded song did not appear in the library')
  })
})
