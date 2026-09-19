import { describe, expect, it } from 'vitest'
import { radioSongs } from './provider'

describe('radioSongs ready queue projection', () => {
  it('only creates playable rows from ready upNext items', () => {
    const result = radioSongs({
      session: { id: 'session-1' },
      items: [
        {
          id: 'seed-1',
          position: 0,
          type: 'seed',
          status: 'ready',
          song: { id: 'song-seed', title: 'Seed' },
        },
      ],
      upNext: [
        {
          id: 'ready-1',
          position: 1,
          type: 'library',
          status: 'ready',
          mediaFileId: 'song-1',
          song: { title: 'Ready' },
        },
        {
          id: 'pending-1',
          position: 2,
          type: 'discovery',
          status: 'downloading',
          song: { id: 'song-pending', title: 'Pending' },
        },
      ],
      pendingItems: [{ id: 'pending-1', status: 'downloading' }],
    })

    expect(result.ids).toEqual(['radio-seed-1', 'radio-ready-1'])
    expect(result.ids.some((id) => id.includes('pending'))).toBe(false)
    expect(result.data['radio-ready-1']).toEqual(
      expect.objectContaining({
        id: 'song-1',
        isRadio: true,
        radioPending: false,
      }),
    )
  })
})
