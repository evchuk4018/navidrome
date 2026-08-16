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

    expect(setActionHandler).toHaveBeenCalledWith('nexttrack', null)
    expect(setActionHandler).toHaveBeenCalledWith('previoustrack', null)
  })

  it('wires the play action to the native audio element', () => {
    const setActionHandler = vi.fn()
    const play = vi.fn(() => Promise.resolve())

    configureMediaSessionTrackNavigation(
      { paused: true, play },
      { setActionHandler },
    )

    const playHandler = setActionHandler.mock.calls.find(
      ([action]) => action === 'play',
    )[1]
    playHandler()

    expect(play).toHaveBeenCalledOnce()
  })

  it('wires the pause action to the native audio element', () => {
    const setActionHandler = vi.fn()
    const pause = vi.fn()

    configureMediaSessionTrackNavigation(
      { paused: false, pause },
      { setActionHandler },
    )

    const pauseHandler = setActionHandler.mock.calls.find(
      ([action]) => action === 'pause',
    )[1]
    pauseHandler()

    expect(pause).toHaveBeenCalledOnce()
  })

  it('clears the play and pause handlers on cleanup', () => {
    const setActionHandler = vi.fn()
    const mediaSession = { setActionHandler }
    const audioInstance = { play: vi.fn(), pause: vi.fn() }

    const cleanup = configureMediaSessionTrackNavigation(
      audioInstance,
      mediaSession,
    )
    cleanup()

    expect(setActionHandler).toHaveBeenCalledWith('play', null)
    expect(setActionHandler).toHaveBeenCalledWith('pause', null)
  })

  it('does nothing when media session is unavailable', () => {
    expect(() =>
      configureMediaSessionTrackNavigation({ playPrev: vi.fn() }, null),
    ).not.toThrow()
  })
})