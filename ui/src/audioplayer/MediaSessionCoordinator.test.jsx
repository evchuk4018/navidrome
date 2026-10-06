import React, { useEffect } from 'react'
import { act, cleanup, render, waitFor } from '@testing-library/react'
import {
  MediaSessionCoordinator,
  useMediaSessionCoordinator,
  useMediaSessionSource,
} from './MediaSessionCoordinator'

const createMediaSession = () => {
  const handlers = {}
  return {
    handlers,
    metadata: null,
    playbackState: 'none',
    setActionHandler: vi.fn((action, handler) => {
      handlers[action] = handler
    }),
    setPositionState: vi.fn(),
  }
}

const createMediaElement = ({ paused = true, duration = 120 } = {}) => {
  const listeners = new Map()
  const element = {
    paused,
    ended: false,
    duration,
    currentTime: 30,
    playbackRate: 1,
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
    act(() => {
      mediaSession.handlers.previoustrack()
    })
    expect(music.currentTime).toBe(20)
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
    mediaSession.handlers.nexttrack()

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

    mediaSession.handlers.play()
    expect(homeTube.play).toHaveBeenCalledOnce()
    expect(music.play).not.toHaveBeenCalled()
  })
})
