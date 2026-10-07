import React from 'react'
import { act, cleanup, render } from '@testing-library/react'
import { Provider } from 'react-redux'
import { createStore } from 'redux'
import { playerReducer } from '../reducers/playerReducer'
import { currentPlaying, playTracks, setPlayMode } from '../actions'
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
const setup = (
  tracks = {
    1: { id: 'song' },
    2: {
      source: 'hometube',
      videoId: 'video',
      video: { id: 'video', title: 'Video' },
    },
    3: { id: 'last' },
  },
) => {
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
    store.dispatch(playTracks(tracks))
  })
  return store
}
const musicTracks = Object.fromEntries(
  Array.from({ length: 5 }, (_, index) => {
    const id = `song-${index + 1}`
    return [id, { id }]
  }),
)
const reportCurrent = (store, item, ended = false) => {
  act(() => {
    store.dispatch(currentPlaying({ ...item, paused: ended, ended }))
  })
}
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

it('autoplays five songs when each ended pause is flushed before the end callback', () => {
  const store = setup(musicTracks)
  const rows = [...queue.queue]

  for (let index = 0; index < rows.length; index++) {
    const item = rows[index]
    reportCurrent(store, item)
    expect(store.getState().player.playIndex).toBeUndefined()

    // Natural completion emits pause with ended=true before ended. Flush the
    // callbacks separately so the queue observes the reducer's saved position.
    reportCurrent(store, item, true)
    expect(queue.index).toBe(index)
    expect(queue.selected.uuid).toBe(item.uuid)
    expect(store.getState().player.current).toEqual({})
    const intent = store.getState().player.musicIntent

    act(() => queue.ended(item.uuid))
    const nextIndex = Math.min(index + 1, rows.length - 1)
    expect(queue.index).toBe(nextIndex)
    expect(queue.selected.uuid).toBe(rows[nextIndex].uuid)
    expect(queue.queue).toEqual(rows)
    expect(store.getState().player.musicIntent).toBe(
      intent + (index < rows.length - 1 ? 1 : 0),
    )

    const afterEnd = store.getState().player
    act(() => queue.ended(item.uuid))
    expect(store.getState().player).toBe(afterEnd)
  }
})

it('keeps the next song selected when the outgoing pause arrives after advancement', () => {
  const store = setup(musicTracks)
  const [first, second] = queue.queue
  reportCurrent(store, first)
  act(() => queue.ended(first.uuid))
  reportCurrent(store, first, true)

  expect(queue.selected.uuid).toBe(second.uuid)
  expect(store.getState().player.playIndex).toBe(1)
  const pending = store.getState().player
  act(() => queue.ended(first.uuid))
  expect(store.getState().player).toBe(pending)

  reportCurrent(store, second)
  expect(queue.selected.uuid).toBe(second.uuid)
  expect(store.getState().player.playIndex).toBeUndefined()
  expect(store.getState().player.savedPlayIndex).toBe(1)
})

it('advances past a pending radio download to its playable buffer song', () => {
  const store = setup({
    seed: { id: 'seed' },
    pending: {
      id: 'pending',
      radioItemId: 'pending-item',
      radioPending: true,
    },
    buffer: { id: 'buffer' },
    ready: { id: 'ready' },
  })
  const [seed, , buffer, ready] = queue.queue

  reportCurrent(store, seed)
  reportCurrent(store, seed, true)
  act(() => queue.ended(seed.uuid))
  expect(queue.selected.uuid).toBe(buffer.uuid)
  expect(queue.index).toBe(2)

  reportCurrent(store, buffer)
  reportCurrent(store, buffer, true)
  act(() => queue.ended(buffer.uuid))
  expect(queue.selected.uuid).toBe(ready.uuid)
  expect(queue.index).toBe(3)
})

it.each(['singleLoop', 'orderLoop'])(
  'honors %s after the last song reports an ended pause',
  (mode) => {
    const store = setup(musicTracks)
    const music = { restart: vi.fn() }
    queue.register('music', music)
    act(() => {
      store.dispatch(setPlayMode(mode))
      queue.select(4)
    })
    const last = queue.selected
    reportCurrent(store, last)
    reportCurrent(store, last, true)
    const intent = store.getState().player.musicIntent
    act(() => queue.ended(last.uuid))

    expect(queue.index).toBe(mode === 'singleLoop' ? 4 : 0)
    expect(store.getState().player.musicIntent).toBe(
      intent + (mode === 'orderLoop' ? 1 : 0),
    )
    if (mode === 'singleLoop') {
      expect(music.restart).toHaveBeenCalledOnce()
      expect(music.restart).toHaveBeenCalledWith(true)
    } else expect(music.restart).not.toHaveBeenCalled()
  },
)

it.each(['order', 'singleLoop'])(
  'blocks %s completion after an ended pause when the sleep timer expires',
  (mode) => {
    vi.useFakeTimers()
    const store = setup(musicTracks)
    const music = { restart: vi.fn() }
    queue.register('music', music)
    act(() => {
      store.dispatch(setPlayMode(mode))
      queue.select(1)
      timer.start(15)
    })
    const second = queue.selected
    reportCurrent(store, second)
    act(() => {
      vi.advanceTimersByTime(15 * 60000)
    })
    expect(coordinator.isPlaybackBlocked()).toBe(true)
    reportCurrent(store, second, true)
    const paused = store.getState().player
    act(() => queue.ended(second.uuid))

    expect(queue.selected.uuid).toBe(second.uuid)
    expect(store.getState().player).toBe(paused)
    expect(music.restart).not.toHaveBeenCalled()
  },
)

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
