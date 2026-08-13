import { describe, expect, it } from 'vitest'
import { radioSongs } from './provider'

describe('radioSongs', () => {
  it('preserves server positions and hides held or downloading tracks', () => {
    const result = radioSongs({
      session: { id: 'session-1' },
      items: [
        {
          id: 'fresh-1',
          position: 2,
          type: 'discovery',
          status: 'downloading',
        },
        {
          id: 'library-2',
          position: 3,
          type: 'library',
          status: 'held',
          song: { id: 'song-2' },
        },
        {
          id: 'fresh-2',
          position: 4,
          type: 'discovery',
          status: 'ready',
          song: { id: 'song-3' },
        },
        {
          id: 'library-3',
          position: 5,
          type: 'library',
          status: 'ready',
          song: { id: 'song-4' },
        },
        {
          id: 'library-1',
          position: 1,
          type: 'library',
          status: 'ready',
          song: { id: 'song-1' },
        },
      ],
    })

    expect(result.ids).toEqual([
      'radio-library-1',
      'radio-fresh-2',
      'radio-library-3',
    ])
    expect(result.ids.map((id) => result.data[id].radioItemId)).toEqual([
      'library-1',
      'fresh-2',
      'library-3',
    ])
  })
})
