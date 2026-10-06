import React from 'react'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import HomeTubePlayer from './HomeTubePlayer'

vi.mock('../config', () => ({
  default: { homeTubeBaseURL: '/hometube' },
}))

const makeVideo = (overrides = {}) => ({
  id: 'video-1',
  title: 'A HomeTube video',
  channelId: 'channel-1',
  channelName: 'HomeTube channel',
  thumbnailUrl: '/thumbnail.jpg',
  mediaStatus: 'ready',
  durationSeconds: 120,
  playbackPositionSeconds: 0,
  ...overrides,
})

const installMediaElement = (
  element,
  { paused = true, rejectPlay = false } = {},
) => {
  let currentPaused = paused
  Object.defineProperty(element, 'paused', {
    configurable: true,
    get: () => currentPaused,
  })

  const play = vi.fn(() => {
    if (rejectPlay) return Promise.reject(new Error('autoplay blocked'))
    currentPaused = false
    element.dispatchEvent(new Event('play'))
    return Promise.resolve()
  })
  const pause = vi.fn(() => {
    currentPaused = true
    element.dispatchEvent(new Event('pause'))
  })
  Object.defineProperty(element, 'play', {
    configurable: true,
    value: play,
  })
  Object.defineProperty(element, 'pause', {
    configurable: true,
    value: pause,
  })

  return {
    play,
    pause,
    isPaused: () => currentPaused,
  }
}

const renderPlayer = (overrides = {}) => {
  const video = overrides.video || makeVideo()
  const props = {
    video,
    queue: [{ video }],
    active: true,
    expanded: true,
    autoplayNonce: 0,
    onElementChange: vi.fn(),
    onProgress: vi.fn(),
    onEnded: vi.fn(),
    onAutoplayRejected: vi.fn(),
    onClose: vi.fn(),
    onPlay: vi.fn(),
    onRetry: vi.fn(),
    onNext: vi.fn(),
    onDismiss: vi.fn(),
    ...overrides,
  }
  return { ...render(<HomeTubePlayer {...props} />), props }
}

describe('HomeTubePlayer', () => {
  beforeEach(() => {
    // jsdom's native media methods throw. A resolving default keeps tests that
    // do not explicitly exercise autoplay focused on DOM and state behavior.
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(
      function () {
        Object.defineProperty(this, 'paused', {
          configurable: true,
          value: false,
          writable: true,
        })
        this.dispatchEvent(new Event('play'))
        return Promise.resolve()
      },
    )
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(
      function () {
        Object.defineProperty(this, 'paused', {
          configurable: true,
          value: true,
          writable: true,
        })
        this.dispatchEvent(new Event('pause'))
      },
    )
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('keeps the same audio and visual elements across expanded state changes', () => {
    const video = makeVideo({ hasBackgroundAudio: true })
    const result = renderPlayer({ video, expanded: true })
    const visual = result.container.querySelector('video')
    const audio = result.container.querySelector('audio')
    const videoSrc = visual.getAttribute('src')
    const audioSrc = audio.getAttribute('src')

    expect(visual).toBeInTheDocument()
    expect(audio).toBeInTheDocument()
    expect(visual.controls).toBe(false)
    expect(audio.controls).toBe(false)

    act(() => {
      result.rerender(<HomeTubePlayer {...result.props} expanded={false} />)
    })
    expect(result.container.querySelector('video')).toBe(visual)
    expect(result.container.querySelector('audio')).toBe(audio)
    expect(visual.getAttribute('src')).toBe(videoSrc)
    expect(audio.getAttribute('src')).toBe(audioSrc)
    expect(audio.controls).toBe(false)

    act(() => {
      result.rerender(<HomeTubePlayer {...result.props} expanded />)
    })
    expect(result.container.querySelector('video')).toBe(visual)
    expect(result.container.querySelector('audio')).toBe(audio)
    expect(audio.controls).toBe(false)
  })

  it('keeps background audio playing while minimizing and pauses only the visual', () => {
    const video = makeVideo({ hasBackgroundAudio: true })
    const result = renderPlayer({ video, expanded: true })
    const visual = result.container.querySelector('video')
    const audio = result.container.querySelector('audio')
    const visualMedia = installMediaElement(visual, { paused: false })
    const audioMedia = installMediaElement(audio, { paused: false })
    const audioSrc = audio.getAttribute('src')

    act(() => {
      result.rerender(<HomeTubePlayer {...result.props} expanded={false} />)
    })

    expect(visualMedia.pause).toHaveBeenCalledOnce()
    expect(visualMedia.isPaused()).toBe(true)
    expect(audioMedia.isPaused()).toBe(false)
    expect(audio.getAttribute('src')).toBe(audioSrc)
    expect(result.container.querySelector('audio')).toBe(audio)
  })

  it('does not restart paused playback when the player is minimized or expanded again', async () => {
    const video = makeVideo()
    const result = renderPlayer({ video, expanded: true, autoplayNonce: 0 })
    const visual = result.container.querySelector('video')
    const media = installMediaElement(visual, { paused: true })

    act(() => {
      result.rerender(<HomeTubePlayer {...result.props} autoplayNonce={1} />)
    })
    await waitFor(() => expect(media.play).toHaveBeenCalledOnce())

    act(() => {
      media.pause()
      result.rerender(
        <HomeTubePlayer {...result.props} expanded={false} autoplayNonce={1} />,
      )
      result.rerender(
        <HomeTubePlayer {...result.props} expanded autoplayNonce={1} />,
      )
    })

    expect(media.play).toHaveBeenCalledOnce()
    expect(media.isPaused()).toBe(true)
  })

  it('does not play or claim an inactive video when a late canplay event arrives', () => {
    const video = makeVideo({ mediaStatus: 'downloading' })
    const onPlay = vi.fn()
    const result = renderPlayer({
      video,
      active: false,
      expanded: false,
      autoplayNonce: 1,
      onPlay,
    })
    const visual = result.container.querySelector('video')
    const media = installMediaElement(visual, { paused: true })

    act(() => {
      result.rerender(
        <HomeTubePlayer
          {...result.props}
          video={makeVideo({ mediaStatus: 'ready' })}
          expanded={false}
          autoplayNonce={1}
        />,
      )
      visual.dispatchEvent(new Event('canplay'))
      visual.dispatchEvent(new Event('loadedmetadata'))
    })

    expect(media.play).not.toHaveBeenCalled()
    expect(onPlay).not.toHaveBeenCalled()
  })

  it('keeps the Play control usable after autoplay is rejected', async () => {
    const video = makeVideo()
    const onAutoplayRejected = vi.fn()
    const result = renderPlayer({ video, autoplayNonce: 0, onAutoplayRejected })
    const visual = result.container.querySelector('video')
    const media = installMediaElement(visual, {
      paused: true,
      rejectPlay: true,
    })

    act(() => {
      result.rerender(<HomeTubePlayer {...result.props} autoplayNonce={1} />)
    })
    await waitFor(() => expect(onAutoplayRejected).toHaveBeenCalledOnce())

    const playButton = screen.getByRole('button', { name: 'Play' })
    expect(playButton).not.toBeDisabled()
    act(() => {
      fireEvent.click(playButton)
    })
    await waitFor(() => expect(media.play).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: 'Play' })).not.toBeDisabled()
  })

  it('uses one custom playback row, clamps rewind, and seeks the background audio', () => {
    const result = renderPlayer({
      video: makeVideo({ hasBackgroundAudio: true }),
    })
    const visual = result.container.querySelector('video')
    const audio = result.container.querySelector('audio')
    expect(visual.controls).toBe(false)
    expect(audio.controls).toBe(false)
    expect(screen.getAllByRole('button', { name: 'Play' })).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: 'Next' })).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()
    act(() => {
      audio.currentTime = 8
      audio.dispatchEvent(new Event('timeupdate'))
    })
    fireEvent.click(screen.getByRole('button', { name: 'Rewind 10 seconds' }))
    expect(audio.currentTime).toBe(0)
    const slider = screen.getByRole('slider', { name: 'Video position' })
    fireEvent.keyDown(slider, { key: 'ArrowRight', code: 'ArrowRight' })
    expect(audio.currentTime).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: 'Sleep timer' })).toBeEnabled()
  })

  it('opens the upcoming queue separately and preserves selection and dismissal', () => {
    const current = makeVideo()
    const next = makeVideo({ id: 'video-2', title: 'Upcoming video' })
    const { props } = renderPlayer({
      queue: [{ video: current }, { video: next }],
    })
    expect(
      screen.queryByRole('button', { name: 'Upcoming video' }),
    ).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Open autoplay queue' }))
    fireEvent.click(
      screen.getByRole('button', { name: 'Dismiss Upcoming video' }),
    )
    expect(props.onDismiss).toHaveBeenCalledWith('video-2')
    fireEvent.click(screen.getByRole('button', { name: 'Upcoming video' }))
    expect(props.onNext).toHaveBeenCalledWith(next)
  })
})
