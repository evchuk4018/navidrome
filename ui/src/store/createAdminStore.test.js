import { createMemoryHistory } from 'history'
import createAdminStore from './createAdminStore'
import { playerReducer } from '../reducers/playerReducer'
import { playbackCacheKey } from '../playbacksession/codec'

vi.mock('react-admin', () => ({
  adminReducer: (state = {}) => state,
  adminSaga: function* () {},
  USER_LOGOUT: 'USER_LOGOUT',
}))
vi.mock('../playbacksession/controller', () => ({
  playbackSession: { start: vi.fn() },
  playbackSessionMiddleware: () => (next) => (action) => next(action),
}))
vi.mock('../subsonic', () => ({
  default: {
    streamUrl: (id) => `fresh-stream-${id}`,
    getCoverArtUrl: () => 'fresh-art',
  },
}))
vi.mock('../transcode', () => ({ decisionService: { getProfile: () => null } }))
const makeStore = () =>
  createAdminStore({
    authProvider: {},
    dataProvider: {},
    history: createMemoryHistory(),
    customReducers: { player: playerReducer },
  })
beforeEach(() => localStorage.clear())

it('opens a complete paused player when only local volume was persisted', () => {
  localStorage.setItem('state', JSON.stringify({ player: { volume: 0.4 } }))
  const player = makeStore().getState().player
  expect(player.queue).toEqual([])
  expect(player.current).toEqual({})
  expect(player.volume).toBe(0.4)
  expect(player.autoPlay).toBe(false)
})

it('restores only the current users cache with fresh URLs and duplicate occurrences', () => {
  localStorage.setItem('userId', 'u')
  localStorage.setItem('state', JSON.stringify({ player: { volume: 0.6 } }))
  const saved = {
    entries: ['one', 'two'].map((entryId) => ({
      entryId,
      source: 'music',
      id: 'song',
      title: 'Song',
      artist: 'Artist',
    })),
    selectedEntryId: 'two',
    positionMs: 34000,
    mode: 'orderLoop',
  }
  localStorage.setItem(playbackCacheKey('u'), JSON.stringify(saved))
  localStorage.setItem(
    playbackCacheKey('other'),
    JSON.stringify({
      ...saved,
      entries: [{ entryId: 'private', source: 'music', id: 'private' }],
    }),
  )
  const player = makeStore().getState().player
  expect(player.queue.map((item) => item.uuid)).toEqual(['one', 'two'])
  expect(player.queue[1].musicSrc).toBe('fresh-stream-song')
  expect(player.savedPlayIndex).toBe(1)
  expect(player.queue[1].resumePositionMs).toBe(34000)
  expect(player.autoPlay).toBe(false)
  expect(player.volume).toBe(0.6)
})

it('discards corrupt caches and unscoped legacy queues while logged out', () => {
  localStorage.setItem(playbackCacheKey('u'), '{"entries":"invalid"}')
  localStorage.setItem(
    'state',
    JSON.stringify({
      player: {
        volume: 0.5,
        queue: [{ uuid: 'private', name: 'Old account song' }],
      },
    }),
  )
  const player = makeStore().getState().player
  expect(player.queue).toEqual([])
  expect(player.volume).toBe(0.5)
})
