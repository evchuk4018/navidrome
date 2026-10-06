import React, { useEffect } from 'react'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import config from '../config'
import {
  HomeTubePlaybackProvider,
  shouldResume,
  useHomeTubePlayback,
} from './HomeTubePlaybackContext'

const api = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('./api', () => ({
  homeTubeRequest: api.request,
  homeTubeApiPath: (path) => `https://hometube.test${path}`,
}))

const engine = vi.hoisted(() => ({
  element: {
    paused: true,
    ended: false,
    currentTime: 0,
    duration: 60,
    pause: vi.fn(),
    play: vi.fn(() => Promise.resolve()),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  },
}))

vi.mock('./HomeTubePlayer', () => ({
  default: function MockHomeTubePlayer({
    video,
    autoplayNonce,
    onElementChange,
    onPlay,
    onProgress,
    onEnded,
    onClose,
    onExpand,
    onSleepExpire,
    onUserPlayback,
  }) {
    useEffect(() => {
      onElementChange(engine.element)
    }, [onElementChange])
    if (!video) return null
    return (
      <div data-testid="mock-hometube-player">
        <button type="button" onClick={onSleepExpire}>
          expire sleep timer
        </button>
        <button type="button" onClick={onUserPlayback}>
          resume manually
        </button>
        <span data-testid="autoplay-nonce">{autoplayNonce}</span>
        <button type="button" onClick={onPlay}>
          engine play
        </button>
        <button
          type="button"
          onClick={() =>
            onProgress({ position: 12, duration: 60, event: 'pause' })
          }
        >
          engine pause
        </button>
        <button type="button" onClick={onEnded}>
          engine ended
        </button>
        <button type="button" onClick={onClose}>
          minimize player
        </button>
        <button type="button" onClick={onExpand}>
          expand player
        </button>
      </div>
    )
  },
}))

const video = (id, mediaStatus = 'ready') => ({
  id,
  channelId: '00000000-0000-0000-0000-000000000001',
  channelName: 'Channel',
  title: `Video ${id}`,
  durationSeconds: 60,
  thumbnailUrl: null,
  mediaStatus,
  mediaError: null,
  hasBackgroundAudio: false,
  playbackPositionSeconds: 0,
  watchState: 'unwatched',
})

const Harness = ({ firstVideo = video('one'), secondVideo = video('two') }) => {
  const playback = useHomeTubePlayback()
  return (
    <>
      <button type="button" onClick={() => playback.playVideo(firstVideo)}>
        select video
      </button>
      <button type="button" onClick={() => playback.playVideo(secondVideo)}>
        select second video
      </button>
      <button type="button" onClick={playback.claimMusic}>
        select music
      </button>
      <span data-testid="source">{playback.activeSource || 'none'}</span>
      <span data-testid="current">{playback.currentVideo?.id || 'none'}</span>
      <span data-testid="status">{playback.status}</span>
      <span data-testid="queue">
        {playback.queue.map((entry) => entry.video.id).join(',')}
      </span>
    </>
  )
}

describe('HomeTubePlaybackProvider', () => {
  beforeEach(() => {
    config.homeTubeBaseURL = '/hometube'
    engine.element.pause.mockClear()
    engine.element.play.mockClear()
    api.request.mockImplementation(async (path, options = {}) => {
      if (path === '/api/queue') {
        const body = JSON.parse(options.body)
        const current = video(body.currentVideoId)
        return {
          entries: [
            { video: current, job: null },
            { video: video(`${body.currentVideoId}-next`), job: null },
          ],
        }
      }
      return {}
    })
  })

  afterEach(() => {
    config.homeTubeBaseURL = ''
    vi.clearAllMocks()
  })

  it('keeps the integration inert when the service is not configured', () => {
    config.homeTubeBaseURL = ''
    render(
      <HomeTubePlaybackProvider>
        <Harness />
      </HomeTubePlaybackProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'select video' }))
    expect(screen.getByTestId('source')).toHaveTextContent('none')
    expect(api.request).not.toHaveBeenCalled()
  })

  it('claims HomeTube, opens the video, builds a queue, and releases ownership to music', async () => {
    render(
      <HomeTubePlaybackProvider>
        <Harness />
      </HomeTubePlaybackProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'select video' }))
    await waitFor(() =>
      expect(screen.getByTestId('source')).toHaveTextContent('hometube'),
    )
    expect(screen.getByTestId('current')).toHaveTextContent('one')
    expect(api.request).toHaveBeenCalledWith('/api/videos/one/open', {
      method: 'POST',
    })
    expect(api.request).toHaveBeenCalledWith(
      '/api/queue',
      expect.objectContaining({ method: 'PUT' }),
    )

    fireEvent.click(screen.getByRole('button', { name: 'select music' }))
    expect(screen.getByTestId('source')).toHaveTextContent('music')
    expect(screen.getByTestId('status')).toHaveTextContent('ready')
  })

  it('does not rearm ready playback when delayed queue hydration completes', async () => {
    let resolveQueue
    api.request.mockImplementation((path) => {
      if (path === '/api/queue') {
        return new Promise((resolve) => {
          resolveQueue = resolve
        })
      }
      return Promise.resolve({})
    })

    render(
      <HomeTubePlaybackProvider>
        <Harness />
      </HomeTubePlaybackProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'select video' }))
    expect(screen.getByTestId('autoplay-nonce')).toHaveTextContent('1')

    await act(async () => {
      resolveQueue({
        entries: [
          { video: video('one'), job: null },
          { video: video('one-next'), job: null },
        ],
      })
      await Promise.resolve()
    })
    expect(screen.getByTestId('autoplay-nonce')).toHaveTextContent('1')
  })

  it('ignores delayed queue hydration after music takes ownership', async () => {
    let resolveQueue
    api.request.mockImplementation((path) => {
      if (path === '/api/queue') {
        return new Promise((resolve) => {
          resolveQueue = resolve
        })
      }
      return Promise.resolve({})
    })

    render(
      <HomeTubePlaybackProvider>
        <Harness firstVideo={video('one', 'downloading')} />
      </HomeTubePlaybackProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'select video' }))
    await waitFor(() => expect(resolveQueue).toEqual(expect.any(Function)))
    expect(screen.getByTestId('source')).toHaveTextContent('hometube')
    expect(screen.getByTestId('status')).toHaveTextContent('preparing')

    fireEvent.click(screen.getByRole('button', { name: 'select music' }))
    expect(screen.getByTestId('source')).toHaveTextContent('music')

    await act(async () => {
      resolveQueue({
        entries: [
          { video: video('one'), job: null },
          { video: video('one-next'), job: null },
        ],
      })
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByTestId('source')).toHaveTextContent('music')
    expect(screen.getByTestId('status')).toHaveTextContent('preparing')
    expect(screen.getByTestId('autoplay-nonce')).toHaveTextContent('0')
    expect(engine.element.play).not.toHaveBeenCalled()
  })

  it('invalidates pending downloads when sleep expires before readiness arrives', async () => {
    let resolveQueue
    api.request.mockImplementation((path) =>
      path === '/api/queue'
        ? new Promise((resolve) => {
            resolveQueue = resolve
          })
        : Promise.resolve({}),
    )
    render(
      <HomeTubePlaybackProvider>
        <Harness firstVideo={video('one', 'downloading')} />
      </HomeTubePlaybackProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'select video' }))
    fireEvent.click(screen.getByRole('button', { name: 'expire sleep timer' }))
    await act(async () => {
      resolveQueue({ entries: [{ video: video('one'), job: null }] })
      await Promise.resolve()
    })
    expect(screen.getByTestId('status')).toHaveTextContent('paused')
    expect(screen.getByTestId('autoplay-nonce')).toHaveTextContent('0')
    expect(engine.element.play).not.toHaveBeenCalled()
  })

  it('ignores the first selection response after a second video is selected', async () => {
    const pendingQueues = new Map()
    api.request.mockImplementation((path, options = {}) => {
      if (path === '/api/queue') {
        const currentVideoId = JSON.parse(options.body).currentVideoId
        return new Promise((resolve) => {
          pendingQueues.set(currentVideoId, resolve)
        })
      }
      return Promise.resolve({})
    })

    render(
      <HomeTubePlaybackProvider>
        <Harness />
      </HomeTubePlaybackProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'select video' }))
    await waitFor(() =>
      expect(pendingQueues.get('one')).toEqual(expect.any(Function)),
    )

    fireEvent.click(screen.getByRole('button', { name: 'select second video' }))
    await waitFor(() =>
      expect(pendingQueues.get('two')).toEqual(expect.any(Function)),
    )
    expect(screen.getByTestId('current')).toHaveTextContent('two')
    expect(screen.getByTestId('autoplay-nonce')).toHaveTextContent('2')

    await act(async () => {
      pendingQueues.get('one')({
        entries: [
          { video: video('one'), job: null },
          { video: video('one-next'), job: null },
        ],
      })
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByTestId('current')).toHaveTextContent('two')
    expect(screen.getByTestId('queue')).toHaveTextContent('')
    expect(screen.getByTestId('autoplay-nonce')).toHaveTextContent('2')

    await act(async () => {
      pendingQueues.get('two')({
        entries: [
          { video: video('two'), job: null },
          { video: video('two-next'), job: null },
        ],
      })
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByTestId('current')).toHaveTextContent('two')
    expect(screen.getByTestId('queue')).toHaveTextContent('two,two-next')
  })

  it('blocks queue advancement after sleep expiry until explicit playback', async () => {
    render(
      <HomeTubePlaybackProvider>
        <Harness />
      </HomeTubePlaybackProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'select video' }))
    await waitFor(() =>
      expect(screen.getByTestId('source')).toHaveTextContent('hometube'),
    )
    fireEvent.click(screen.getByRole('button', { name: 'minimize player' }))
    fireEvent.click(screen.getByRole('button', { name: 'expire sleep timer' }))
    expect(engine.element.pause).toHaveBeenCalled()
    expect(screen.getByTestId('autoplay-nonce')).toHaveTextContent('0')
    const requestCount = api.request.mock.calls.length
    fireEvent.click(screen.getByRole('button', { name: 'engine ended' }))
    expect(api.request).toHaveBeenCalledTimes(requestCount)
    fireEvent.click(screen.getByRole('button', { name: 'resume manually' }))
    fireEvent.click(screen.getByRole('button', { name: 'engine ended' }))
    await waitFor(() =>
      expect(screen.getByTestId('current')).toHaveTextContent('one-next'),
    )
  })

  it('resumes only before the end tolerance and never resumes watched videos', () => {
    expect(
      shouldResume(
        { playbackPositionSeconds: 20, watchState: 'in_progress' },
        60,
      ),
    ).toBe(20)
    expect(
      shouldResume(
        { playbackPositionSeconds: 57, watchState: 'in_progress' },
        60,
      ),
    ).toBe(0)
    expect(
      shouldResume({ playbackPositionSeconds: 20, watchState: 'watched' }, 60),
    ).toBe(0)
  })
})
