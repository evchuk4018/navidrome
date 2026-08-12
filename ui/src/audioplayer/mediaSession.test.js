import configureMediaSessionTrackNavigation from './mediaSession'

describe('configureMediaSessionTrackNavigation', () => {
  it('replaces seek actions with previous and next track actions', () => {
    const setActionHandler = vi.fn()
    const mediaSession = { setActionHandler }
    const playPrev = vi.fn()
    const playNext = vi.fn()
    const audioInstance = { playPrev, playNext }

    const cleanup = configureMediaSessionTrackNavigation(
      audioInstance,
      mediaSession,
    )

    expect(setActionHandler).toHaveBeenCalledWith('seekbackward', null)
    expect(setActionHandler).toHaveBeenCalledWith('seekforward', null)

    const previousTrackHandler = setActionHandler.mock.calls.find(
      ([action]) => action === 'previoustrack',
    )[1]
    const nextTrackHandler = setActionHandler.mock.calls.find(
      ([action]) => action === 'nexttrack',
    )[1]

    previousTrackHandler()
    nextTrackHandler()

    expect(playPrev).toHaveBeenCalledOnce()
    expect(playNext).toHaveBeenCalledOnce()

    cleanup()

    expect(setActionHandler).toHaveBeenLastCalledWith('nexttrack', null)
    expect(setActionHandler).toHaveBeenCalledWith('previoustrack', null)
  })

  it('does nothing when media session is unavailable', () => {
    expect(() =>
      configureMediaSessionTrackNavigation({ playPrev: vi.fn() }, null),
    ).not.toThrow()
  })
})
