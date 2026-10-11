import React, { useEffect } from 'react'
import { act, cleanup, render, waitFor } from '@testing-library/react'
import {
  MediaSessionCoordinator,
  useMediaSessionCoordinator,
  useMediaSessionSource,
} from './MediaSessionCoordinator'

const createMediaSession = () => {
  const handlers = {}
  let metadata = null
  let playbackState = 'none'
  const metadataHistory = []
  const stateHistory = []
  return {
    handlers,
    metadataHistory,
    stateHistory,
    get metadata() {
      return metadata
    },
    set metadata(value) {
      metadataHistory.push(value)
      metadata = value
    },
    get playbackState() {
      return playbackState
    },
    set playbackState(value) {
      stateHistory.push(value)
      playbackState = value
    },
    setActionHandler: vi.fn((action, handler) => {
      handlers[action] = handler
    }),
    setPositionState: vi.fn(),
  }
}

const createMediaElement = ({
  paused = true,
  duration = 120,
  evicted = false,
} = {}) => {
  const listeners = new Map()
  const element = {
    paused,
    src: '/song.mp3',
    readyState: evicted ? 0 : 4,
    networkState: evicted ? 0 : 1,
    ended: false,
    duration,
    currentTime: 30,
    playbackRate: 1,
    emit: (event) => {
      listeners.get(event)?.forEach((listener) => listener())
    },
    load: vi.fn(() => {
      element.currentTime = 0
      element.readyState = 0
      element.networkState = 2
      element.emit('loadstart')
    }),
    play: vi.fn(() => {
      element.paused = false
      listeners.get('play')?.forEach((listener) => listener())
      return Promise.resolve()
    }),
    pause: vi.fn(() => {
      element.paused = true
      listeners.get('pause')?.forEach((listener) => listener())
    }),
    addEventListener: vi.fn((event, listener) => {
      if (!listeners.has(event)) listeners.set(event, new Set())
      listeners.get(event).add(listener)
    }),
    removeEventListener: vi.fn((event, listener) => {
      listeners.get(event)?.delete(listener)
    }),
  }
  return element
}

const MediaSource = ({
  source,
  element,
  metadata,
  active,
  audioContext,
  onPrevious,
  onNext,
  onSeekBackward,
  onSeekForward,
  onSleepExpire,
  onUserPlayback,
}) => {
  useMediaSessionSource({
    source,
    element,
    metadata,
    active,
    audioContext,
    onPrevious,
    onNext,
    onSeekBackward,
    onSeekForward,
    onSleepExpire,
    onUserPlayback,
  })
  return null
}

const CoordinatorProbe = ({ onReady }) => {
  const coordinator = useMediaSessionCoordinator()

  useEffect(() => {
    onReady(coordinator)
  }, [coordinator, onReady])

  return null
}

const TestTree = ({ music, homeTube, onCoordinator }) => (
  <MediaSessionCoordinator>
    <CoordinatorProbe onReady={onCoordinator} />
    <MediaSource source="music" {...music} />
    <MediaSource source="hometube" {...homeTube} />
  </MediaSessionCoordinator>
)

const renderTree = (props) => render(<TestTree {...props} />)

describe('MediaSessionCoordinator', () => {
  let mediaSession

  beforeEach(() => {
    mediaSession = createMediaSession()
    vi.stubGlobal('navigator', { ...navigator, mediaSession })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('allows OS playback to explicitly resume a source stopped by the sleep timer', async () => {
    const music = createMediaElement({ paused: false })
    const onCoordinator = vi.fn()
    const onSleepExpire = vi.fn()
    const onUserPlayback = vi.fn()
    renderTree({
      onCoordinator,
      music: {
        element: music,
        active: true,
        metadata: { title: 'Radio' },
        onSleepExpire,
        onUserPlayback,
      },
      homeTube: {},
    })
    const coordinator = onCoordinator.mock.lastCall[0]
    act(() => {
      coordinator.expireSleep()
    })
    expect(onSleepExpire).toHaveBeenCalledOnce()
    expect(music.paused).toBe(true)
    expect(coordinator.isPlaybackBlocked()).toBe(true)
    expect(mediaSession.handlers.previoustrack).toBeNull()
    expect(mediaSession.handlers.seekbackward).toBeNull()
    expect(music.currentTime).toBe(30)
    expect(music.paused).toBe(true)
    expect(onUserPlayback).not.toHaveBeenCalled()
    act(() => {
      mediaSession.handlers.play()
    })
    expect(onUserPlayback).toHaveBeenCalledOnce()
    expect(music.paused).toBe(false)
    expect(coordinator.isPlaybackBlocked()).toBe(false)
  })

  it('keeps active ownership while descriptors and callbacks rerender', async () => {
    const music = createMediaElement()
    const homeTube = createMediaElement({ paused: false })
    const firstNext = vi.fn()
    const updatedNext = vi.fn()
    const { rerender } = renderTree({
      onCoordinator: vi.fn(),
      music: {
        element: music,
        active: false,
        metadata: { title: 'Music' },
        onNext: vi.fn(),
      },
      homeTube: {
        element: homeTube,
        active: true,
        metadata: { title: 'HomeTube v1' },
        onNext: firstNext,
      },
    })

    await waitFor(() => {
      expect(mediaSession.metadata).toEqual({ title: 'HomeTube v1' })
      expect(mediaSession.handlers.nexttrack).toEqual(expect.any(Function))
    })
    const nextHandler = mediaSession.handlers.nexttrack
    mediaSession.setActionHandler.mockClear()
    mediaSession.metadataHistory.length = 0
    mediaSession.stateHistory.length = 0

    rerender(
      <TestTree
        onCoordinator={vi.fn()}
        music={{
          element: music,
          active: false,
          metadata: { title: 'Music updated' },
          onNext: vi.fn(),
        }}
        homeTube={{
          element: homeTube,
          active: true,
          metadata: { title: 'HomeTube v2' },
          onNext: updatedNext,
        }}
      />,
    )

    await waitFor(() =>
      expect(mediaSession.metadata).toEqual({ title: 'HomeTube v2' }),
    )
    expect(mediaSession.handlers.nexttrack).toBe(nextHandler)
    expect(mediaSession.setActionHandler).not.toHaveBeenCalled()
    expect(mediaSession.metadataHistory).not.toContain(null)
    expect(mediaSession.stateHistory).not.toContain('none')
    nextHandler()

    expect(updatedNext).toHaveBeenCalledOnce()
    expect(firstNext).not.toHaveBeenCalled()
    expect(mediaSession.metadata).toEqual({ title: 'HomeTube v2' })
  })

  it('pauses the selected source through the coordinator before switching sources', async () => {
    const music = createMediaElement({ paused: false })
    const homeTube = createMediaElement({ paused: false })
    const onCoordinator = vi.fn()
    const { rerender } = renderTree({
      onCoordinator,
      music: {
        element: music,
        active: false,
        metadata: { title: 'Music' },
      },
      homeTube: {
        element: homeTube,
        active: true,
        metadata: { title: 'HomeTube' },
      },
    })

    await waitFor(() => expect(onCoordinator).toHaveBeenCalled())
    const coordinator = onCoordinator.mock.lastCall[0]

    act(() => {
      coordinator.pauseActive()
    })
    expect(homeTube.pause).toHaveBeenCalledOnce()
    expect(music.pause).not.toHaveBeenCalled()

    rerender(
      <TestTree
        onCoordinator={onCoordinator}
        music={{
          element: music,
          active: true,
          metadata: { title: 'Music' },
        }}
        homeTube={{
          element: homeTube,
          active: false,
          metadata: { title: 'HomeTube' },
        }}
      />,
    )

    await waitFor(() =>
      expect(mediaSession.metadata).toEqual({ title: 'Music' }),
    )
    expect(homeTube.pause).toHaveBeenCalledOnce()
  })

  it('routes media session actions to the selected source', async () => {
    const music = createMediaElement()
    const homeTube = createMediaElement()
    const musicNext = vi.fn()
    const homeTubeNext = vi.fn()
    renderTree({
      onCoordinator: vi.fn(),
      music: {
        element: music,
        active: false,
        metadata: { title: 'Music' },
        onNext: musicNext,
      },
      homeTube: {
        element: homeTube,
        active: true,
        metadata: { title: 'HomeTube' },
        onNext: homeTubeNext,
      },
    })

    await waitFor(() =>
      expect(mediaSession.handlers.play).toEqual(expect.any(Function)),
    )
    mediaSession.handlers.play()
    mediaSession.handlers.pause()
    mediaSession.handlers.seekto({ seekTime: 77 })
    mediaSession.handlers.nexttrack()

    expect(homeTube.play).toHaveBeenCalledOnce()
    expect(homeTube.pause).toHaveBeenCalledOnce()
    expect(homeTube.currentTime).toBe(77)
    expect(homeTubeNext).toHaveBeenCalledOnce()
    expect(music.play).not.toHaveBeenCalled()
    expect(music.pause).not.toHaveBeenCalled()
    expect(musicNext).not.toHaveBeenCalled()
  })

  it('retains metadata and selected controls when the active source pauses', async () => {
    const music = createMediaElement()
    const homeTube = createMediaElement({ paused: false })
    const onCoordinator = vi.fn()
    const { rerender } = renderTree({
      onCoordinator,
      music: {
        element: music,
        active: false,
        metadata: { title: 'Music' },
      },
      homeTube: {
        element: homeTube,
        active: true,
        metadata: { title: 'HomeTube paused selection' },
      },
    })

    await waitFor(() =>
      expect(mediaSession.metadata).toEqual({
        title: 'HomeTube paused selection',
      }),
    )
    const coordinator = onCoordinator.mock.lastCall[0]
    const playHandler = mediaSession.handlers.play
    mediaSession.metadataHistory.length = 0
    mediaSession.stateHistory.length = 0
    act(() => {
      coordinator.pauseActive()
    })

    rerender(
      <TestTree
        onCoordinator={onCoordinator}
        music={{
          element: music,
          active: false,
          metadata: { title: 'Music' },
        }}
        homeTube={{
          element: homeTube,
          active: false,
          metadata: { title: 'HomeTube paused selection' },
        }}
      />,
    )

    await waitFor(() => {
      expect(mediaSession.metadata).toEqual({
        title: 'HomeTube paused selection',
      })
      expect(mediaSession.playbackState).toBe('paused')
      expect(mediaSession.handlers.play).toEqual(expect.any(Function))
    })
    expect(mediaSession.handlers.play).toBe(playHandler)
    expect(mediaSession.metadataHistory).not.toContain(null)
    expect(mediaSession.stateHistory).not.toContain('none')

    mediaSession.handlers.play()
    expect(homeTube.play).toHaveBeenCalledOnce()
    expect(music.play).not.toHaveBeenCalled()
  })

  it('advertises track navigation without time jumps and keeps timeline seeking', () => {
    const music = createMediaElement({ paused: false })
    const previous = vi.fn()
    const next = vi.fn()
    renderTree({
      onCoordinator: vi.fn(),
      music: {
        element: music,
        active: true,
        metadata: { title: 'Song' },
        onPrevious: previous,
        onNext: next,
      },
      homeTube: {},
    })

    expect(mediaSession.handlers.seekbackward).toBeNull()
    expect(mediaSession.handlers.seekforward).toBeNull()
    mediaSession.handlers.previoustrack()
    mediaSession.handlers.nexttrack()
    mediaSession.handlers.seekto({ seekTime: 65 })
    expect(previous).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledOnce()
    expect(music.currentTime).toBe(65)
  })

  it('uses stable controls for a new source and ignores events from the old element', () => {
    const music = createMediaElement({ paused: false })
    const homeTube = createMediaElement()
    const next = vi.fn()
    const props = {
      onCoordinator: vi.fn(),
      music: { element: music, active: true, metadata: { title: 'Music' } },
      homeTube: {
        element: homeTube,
        active: false,
        metadata: { title: 'HomeTube' },
        onNext: next,
      },
    }
    const { rerender } = renderTree(props)
    const play = mediaSession.handlers.play
    const pause = mediaSession.handlers.pause
    mediaSession.metadataHistory.length = 0
    mediaSession.stateHistory.length = 0
    rerender(
      <TestTree
        {...props}
        music={{ ...props.music, active: false }}
        homeTube={{ ...props.homeTube, active: true }}
      />,
    )

    act(() => play())
    expect(homeTube.play).toHaveBeenCalledOnce()
    expect(music.play).not.toHaveBeenCalled()
    expect(mediaSession.playbackState).toBe('playing')
    music.emit('pause')
    expect(mediaSession.playbackState).toBe('playing')
    act(() => pause())
    expect(homeTube.paused).toBe(true)
    expect(mediaSession.handlers.play).toBe(play)
    expect(mediaSession.metadata).toEqual({ title: 'HomeTube' })
    expect(mediaSession.metadataHistory).not.toContain(null)
    expect(mediaSession.stateHistory).not.toContain('none')
  })

  it('resumes an interrupted context when the selected element still reports playing', () => {
    const music = createMediaElement({ paused: false })
    const audioContext = { state: 'interrupted', resume: vi.fn() }
    renderTree({
      onCoordinator: vi.fn(),
      music: { element: music, active: true, audioContext },
      homeTube: {},
    })

    mediaSession.handlers.play()
    expect(audioContext.resume).toHaveBeenCalledOnce()
    expect(music.play).not.toHaveBeenCalled()
  })

  it.each(['pause', 'sleep', 'next', 'previous', 'seek', 'switch', 'remove'])(
    'cancels an evicted-source recovery after %s',
    (command) => {
      const music = createMediaElement({ evicted: true })
      const homeTube = createMediaElement()
      const onCoordinator = vi.fn()
      const props = {
        onCoordinator,
        music: {
          element: music,
          active: true,
          metadata: { title: 'Music' },
          onNext: vi.fn(),
          onPrevious: vi.fn(),
        },
        homeTube: { element: homeTube, active: false },
      }
      const { rerender } = renderTree(props)
      const coordinator = onCoordinator.mock.lastCall[0]
      act(() => mediaSession.handlers.play())
      expect(music.load).toHaveBeenCalledOnce()
      act(() => {
        if (command === 'pause') mediaSession.handlers.pause()
        if (command === 'sleep') coordinator.expireSleep()
        if (command === 'next') mediaSession.handlers.nexttrack()
        if (command === 'previous') mediaSession.handlers.previoustrack()
        if (command === 'seek') mediaSession.handlers.seekto({ seekTime: 12 })
      })
      if (command === 'switch' || command === 'remove')
        rerender(
          <TestTree
            {...props}
            music={{
              ...props.music,
              active: false,
              element: command === 'remove' ? null : music,
            }}
            homeTube={{ ...props.homeTube, active: command === 'switch' }}
          />,
        )
      music.readyState = 1
      music.emit('loadedmetadata')

      expect(music.currentTime).toBe(command === 'seek' ? 12 : 0)
      expect(music.play).toHaveBeenCalledOnce()
    },
  )

  it('clears system controls only when the selected player is removed or unmounted', () => {
    const music = createMediaElement({ paused: false })
    const { rerender, unmount } = renderTree({
      onCoordinator: vi.fn(),
      music: { element: music, active: true, metadata: { title: 'Song' } },
      homeTube: {},
    })
    rerender(<TestTree onCoordinator={vi.fn()} music={{}} homeTube={{}} />)
    expect(mediaSession.metadata).toBeNull()
    expect(mediaSession.playbackState).toBe('none')
    expect(
      Object.values(mediaSession.handlers).every((handler) => handler === null),
    ).toBe(true)
    expect(mediaSession.setPositionState).toHaveBeenCalledWith()
    unmount()
    expect(mediaSession.handlers.play).toBeNull()
  })

  it('keeps playback usable when a browser rejects unsupported actions', () => {
    const install = mediaSession.setActionHandler
    mediaSession.setActionHandler = vi.fn((action, handler) => {
      if (action === 'seekforward' || action === 'seekbackward')
        throw new DOMException('Unsupported', 'NotSupportedError')
      install(action, handler)
    })
    const music = createMediaElement()
    renderTree({
      onCoordinator: vi.fn(),
      music: { element: music, active: true },
      homeTube: {},
    })
    act(() => mediaSession.handlers.play())
    expect(music.play).toHaveBeenCalledOnce()
  })
})
