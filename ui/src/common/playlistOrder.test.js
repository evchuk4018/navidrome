import { describe, expect, it } from 'vitest'
import {
  LIKED_MUSIC_PLAYLIST_NAME,
  PLAYLIST_DEFAULT_SORT,
  PLAYLIST_DEFAULT_SORT_FIELD,
  isLikedMusicPlaylist,
  sortPlaylists,
} from './playlistOrder'

describe('playlist ordering', () => {
  it('exposes the opt-in default sort token', () => {
    expect(PLAYLIST_DEFAULT_SORT_FIELD).toBe('liked_songs_first')
    expect(PLAYLIST_DEFAULT_SORT).toEqual({
      field: 'liked_songs_first',
      order: 'ASC',
    })
    expect(LIKED_MUSIC_PLAYLIST_NAME).toBe('liked music')
  })

  it('pins the current user’s normalized ordinary liked playlist', () => {
    const liked = {
      id: 'liked',
      name: '  LiKeD MuSiC  ',
      ownerId: 'user-1',
    }
    const records = [
      { id: 'z', name: 'Zulu', ownerId: 'user-1' },
      liked,
      { id: 'foreign', name: 'liked music', ownerId: 'user-2' },
      { id: 'smart', name: 'liked music', ownerId: 'user-1', rules: {} },
      { id: 'a', name: 'Alpha', ownerId: 'user-1' },
    ]

    expect(isLikedMusicPlaylist(liked, 'user-1')).toBe(true)
    expect(isLikedMusicPlaylist(liked, 'user-2')).toBe(false)
    expect(isLikedMusicPlaylist(liked, '')).toBe(false)
    expect(isLikedMusicPlaylist(records[2], 'user-1')).toBe(false)
    expect(isLikedMusicPlaylist(records[3], 'user-1')).toBe(false)
    expect(sortPlaylists(records, 'en', 'user-1').map(({ id }) => id)).toEqual([
      'liked',
      'a',
      'foreign',
      'smart',
      'z',
    ])
  })

  it('keeps ordinary locale and ID ordering when the liked playlist is absent', () => {
    const records = [
      { id: 'b', name: 'Alpha', ownerId: 'user-1' },
      { id: 'a', name: 'alpha', ownerId: 'user-1' },
      { id: 'z', name: 'Zulu', ownerId: 'user-1' },
    ]
    const before = [...records]

    expect(sortPlaylists(records, 'en', 'user-1').map(({ id }) => id)).toEqual([
      'a',
      'b',
      'z',
    ])
    expect(records).toEqual(before)
  })
})
