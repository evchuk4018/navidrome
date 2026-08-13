import {
  getDefaultViewChoices,
  getStoredDefaultView,
  isResourceDefaultView,
  resourceDefaultViews,
} from './defaultViews'
import albumLists from '../album/albumLists'

describe('defaultViews', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('includes album lists and top-level resource lists as choices', () => {
    const choices = getDefaultViewChoices((key, options) =>
      options?.smart_count ? `${key}:${options.smart_count}` : key,
    )

    expect(choices.map((choice) => choice.id)).toEqual([
      ...Object.keys(albumLists),
      ...resourceDefaultViews,
    ])
    expect(choices).toEqual(
      expect.arrayContaining([
        { id: 'artist', name: 'resources.artist.name:2' },
        { id: 'song', name: 'resources.song.name:2' },
        { id: 'playlist', name: 'resources.playlist.name:2' },
        { id: 'radio', name: 'resources.radio.name:2' },
      ]),
    )
  })

  it('identifies resource-backed default views', () => {
    expect(isResourceDefaultView('quick-pick')).toBe(true)
    expect(isResourceDefaultView('artist')).toBe(true)
    expect(isResourceDefaultView('song')).toBe(true)
    expect(isResourceDefaultView('playlist')).toBe(true)
    expect(isResourceDefaultView('radio')).toBe(true)
    expect(isResourceDefaultView('recentlyAdded')).toBe(false)
  })

  it('makes Quick Pick the one-time default when no view is stored', () => {
    expect(getStoredDefaultView()).toBe('quick-pick')
    expect(localStorage.getItem('quickPickDefaultV1')).toBe('1')
  })

  it('returns the stored default view', () => {
    localStorage.setItem('defaultView', 'playlist')
    localStorage.setItem('quickPickDefaultV1', '1')

    expect(getStoredDefaultView()).toBe('playlist')
  })
})
