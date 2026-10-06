import React from 'react'
import { act, cleanup, render } from '@testing-library/react'
import { Provider } from 'react-redux'
import { createStore } from 'redux'
import { playerReducer } from '../reducers/playerReducer'
import { playTracks, setPlayMode } from '../actions'
import {
  PlaybackQueueProvider,
  usePlaybackQueue,
  navigationIndex,
} from './PlaybackQueueContext'
import {
  MediaSessionCoordinator,
  useMediaSessionCoordinator,
} from './MediaSessionCoordinator'
import { SleepTimerProvider, useSleepTimer } from './SleepTimerContext'

vi.mock('../subsonic', () => ({
  default: { streamUrl: (id) => `/stream/${id}`, getCoverArtUrl: () => '' },
}))
vi.mock('../transcode', () => ({ decisionService: { getProfile: () => null } }))
let queue, timer, coordinator
const Probe = () => {
  queue = usePlaybackQueue()
  timer = useSleepTimer()
  coordinator = useMediaSessionCoordinator()
  return null
}
const setup = () => {
  const store = createStore((state, action) => ({
    player: playerReducer(state?.player, action),
  }))
  render(
    <Provider store={store}>
      <MediaSessionCoordinator>
        <SleepTimerProvider>
          <PlaybackQueueProvider>
            <Probe />
          </PlaybackQueueProvider>
        </SleepTimerProvider>
      </MediaSessionCoordinator>
    </Provider>,
  )
  act(() => {
    store.dispatch(
      playTracks({
        1: { id: 'song' },
        2: {
          source: 'hometube',
          videoId: 'video',
          video: { id: 'video', title: 'Video' },
        },
        3: { id: 'last' },
      }),
    )
  })
  return store
}
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

it('uses music Previous semantics, transitions across sources, and ignores a stale end', () => {
  setup()
  const music = {
    element: { currentTime: 12 },
    restart: vi.fn(),
    stop: vi.fn(),
  }
  const video = {
    element: { currentTime: 0 },
    select: vi.fn(),
    restart: vi.fn(),
    stop: vi.fn(),
  }
  queue.register('music', music)
  queue.register('hometube', video)
  act(() => queue.previous())
  expect(music.element.currentTime).toBe(0)
  act(() => queue.next())
  expect(queue.selected.source).toBe('hometube')
  expect(video.select).toHaveBeenCalledWith(queue.selected, false)
  const old = queue.queue[0].uuid
  act(() => queue.ended(old))
  expect(queue.index).toBe(1)
  act(() => queue.next())
  expect(queue.selected.trackId).toBe('last')
  act(() => queue.previous())
  expect(queue.selected.source).toBe('hometube')
})

it.each(['order', 'orderLoop', 'singleLoop', 'shufflePlay'])(
  'keeps %s navigation and duplicate identity across sources',
  (mode) => {
    const store = setup()
    act(() => {
      store.dispatch(setPlayMode(mode))
    })
    expect(queue.mode).toBe(mode)
    const rows = [{}, {}, {}]
    expect(navigationIndex(rows, 2, mode, true, true, () => 0)).toBe(
      mode === 'order' ? -1 : mode === 'singleLoop' ? 2 : 0,
    )
    expect(navigationIndex(rows, 0, mode, false, false, () => 0)).toBe(
      mode === 'shufflePlay' ? 1 : 2,
    )
  },
)

it('retains selection when reordering/removing other entries, and clears both engines', () => {
  setup()
  const music = { stop: vi.fn() },
    video = { stop: vi.fn(), select: vi.fn() }
  queue.register('music', music)
  queue.register('hometube', video)
  act(() => queue.select(1))
  const selected = queue.selected.uuid
  act(() => queue.reorder(1, 0))
  expect(queue.selected.uuid).toBe(selected)
  act(() => queue.remove(queue.queue[2].uuid))
  expect(queue.selected.uuid).toBe(selected)
  act(() => queue.clear())
  expect(queue.queue).toHaveLength(0)
  expect(music.stop).toHaveBeenCalledOnce()
  expect(video.stop).toHaveBeenCalledOnce()
})

it('preserves one deadline across source changes and blocks end/repeat until explicit selection', () => {
  vi.useFakeTimers()
  const store = setup()
  const video = { select: vi.fn(), restart: vi.fn() }
  queue.register('hometube', video)
  act(() => timer.start(15))
  act(() => {
    vi.advanceTimersByTime(60000)
    queue.select(1)
  })
  expect(timer.remainingSeconds).toBe(840)
  act(() => {
    store.dispatch(setPlayMode('singleLoop'))
  })
  act(() => {
    vi.advanceTimersByTime(840000)
  })
  expect(timer.isActive).toBe(false)
  expect(coordinator.isPlaybackBlocked()).toBe(true)
  act(() => queue.ended(queue.selected.uuid))
  expect(video.restart).not.toHaveBeenCalled()
  act(() => queue.select(1))
  expect(coordinator.isPlaybackBlocked()).toBe(false)
  expect(video.select).toHaveBeenCalledTimes(2)
})
