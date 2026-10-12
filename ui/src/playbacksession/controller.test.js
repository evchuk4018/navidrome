import { createStore } from 'redux'
import { createPlaybackSessionController } from './controller'
import { playbackCacheKey } from './codec'

const queue = [
  {
    uuid: 'first',
    source: 'music',
    trackId: 'song',
    name: 'Song',
    singer: 'Artist',
  },
  {
    uuid: 'video',
    source: 'hometube',
    videoId: 'video',
    name: 'Video',
    video: {
      id: 'video',
      title: 'Video',
      channelName: 'Channel',
      mediaUrl: 'secret',
    },
  },
  {
    uuid: 'duplicate',
    source: 'music',
    trackId: 'song',
    name: 'Song',
    singer: 'Artist',
  },
]
const local = { queue, savedPlayIndex: 2, current: {}, mode: 'shufflePlay' }
const saved = () => ({
  entries: [
    {
      entryId: 'first',
      source: 'music',
      id: 'song',
      title: 'Song',
      song: { id: 'song' },
    },
    {
      entryId: 'video',
      source: 'hometube',
      id: 'video',
      video: { id: 'video', title: 'Video' },
    },
    {
      entryId: 'duplicate',
      source: 'music',
      id: 'song',
      title: 'Song',
      song: { id: 'song' },
    },
  ],
  selectedEntryId: 'duplicate',
  positionMs: 45678,
  mode: 'shufflePlay',
  state: 'paused',
  ownerClientId: 'other',
  deviceLabel: 'Safari on iPhone',
  epoch: 1,
  revision: 1,
  sequence: 0,
})
const clone = (value) => JSON.parse(JSON.stringify(value))
let controller, store, remote, send, actions, user, pause, resume
const settle = () => vi.advanceTimersByTimeAsync(0)

beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
  user = 'user'
  remote = null
  actions = []
  store = createStore((state = { player: local }, action) => {
    actions.push(action)
    if (action.type === 'replace') return { ...state, player: action.player }
    if (action.type === 'PLAYER_RESET_PLAYBACK_USER')
      return { player: { queue: [] } }
    if (action.type === 'playbackSessionChanged')
      return { ...state, playbackSessionEvent: action.data }
    return state
  })
  send = vi.fn(async (path, options = {}) => {
    if (!options.method) {
      const session = remote && clone(remote)
      if (session && path.includes('summary')) {
        session.currentEntry = session.entries.find(
          (entry) => entry.entryId === session.selectedEntryId,
        )
        delete session.entries
      }
      return { session }
    }
    const body = JSON.parse(options.body)
    if (options.method === 'POST') {
      if (body.expectedEpoch !== (remote?.epoch || 0)) throw { status: 409 }
      remote = {
        ...(body.intent === 'resume' ? remote : body.snapshot),
        ownerClientId: 'this',
        epoch: (remote?.epoch || 0) + 1,
        revision: (remote?.revision || 0) + 1,
        sequence: 0,
        lastClaimRequestId: body.requestId,
      }
    } else {
      if (remote.ownerClientId !== 'this' || body.epoch !== remote.epoch)
        throw { status: 409 }
      if (body.sequence > remote.sequence)
        remote = {
          ...remote,
          ...(body.snapshot || {
            positionMs: body.positionMs,
            state: body.state,
          }),
          sequence: body.sequence,
          revision: remote.revision + 1,
        }
    }
    return { session: clone(remote) }
  })
  controller = createPlaybackSessionController({
    send,
    clientId: 'this',
    getUser: () => user,
    label: () => 'Chrome on Windows',
  })
  pause = vi.fn()
  resume = vi.fn()
  controller.setHooks({ pause, resume })
})
afterEach(() => {
  controller.stop()
  vi.useRealTimers()
})

it('loads a remote summary without playing, claiming, or replacing the local queue', async () => {
  remote = saved()
  controller.start(store)
  await settle()
  expect(controller.view().remote.currentEntry.entryId).toBe('duplicate')
  expect(controller.isAllowed()).toBe(false)
  expect(send.mock.calls.some(([, options]) => options?.method)).toBe(false)
  expect(
    actions.some((action) => action.type === 'PLAYER_RESTORE_SESSION'),
  ).toBe(false)
  expect(resume).not.toHaveBeenCalled()
})

it('claims and restores the latest mixed snapshot, retaining duplicate occurrences and position', async () => {
  remote = saved()
  controller.start(store)
  await settle()
  remote.positionMs = 60000
  expect(await controller.resumeHere()).toBe(true)
  const restored = actions.find(
    (action) => action.type === 'PLAYER_RESTORE_SESSION',
  )
  expect(restored.snapshot.positionMs).toBe(60000)
  expect(restored.snapshot.entries.map((entry) => entry.entryId)).toEqual([
    'first',
    'video',
    'duplicate',
  ])
  expect(restored.snapshot.mode).toBe('shufflePlay')
  expect(controller.epoch()).toBe(2)
})

it('claims only after explicit Play and saves references without generated media URLs', async () => {
  controller.start(store)
  await settle()
  expect(await controller.explicitPlay()).toBe(true)
  await settle()
  const post = send.mock.calls.find(([, options]) => options?.method === 'POST')
  const body = JSON.parse(post[1].body)
  expect(body.snapshot.selectedEntryId).toBe('duplicate')
  expect(body.snapshot.entries[1].video.mediaUrl).toBeUndefined()
  expect(body.snapshot.entries[0].song).toBeUndefined()
  controller.updateProgress('duplicate', 12000, 'playing')
  await settle()
  expect(remote.positionMs).toBe(12000)
  await vi.advanceTimersByTimeAsync(5000)
  controller.updateProgress('duplicate', 17000, 'playing')
  await settle()
  expect(remote.positionMs).toBe(17000)
  controller.updateProgress('duplicate', 31000, 'playing', true)
  await settle()
  expect(remote.positionMs).toBe(31000)
})

it('pauses on a handoff event and discards old saves and late progress', async () => {
  controller.start(store)
  await settle()
  await controller.explicitPlay()
  await settle()
  remote = {
    ...remote,
    ownerClientId: 'other',
    epoch: 2,
    revision: remote.revision + 1,
  }
  store.dispatch({
    type: 'playbackSessionChanged',
    data: { ownerClientId: 'other', epoch: 2 },
  })
  await settle()
  const count = send.mock.calls.filter(
    ([, options]) => options?.method === 'PATCH',
  ).length
  controller.updateProgress('duplicate', 99000, 'playing')
  await controller.checkpoint()
  expect(controller.isAllowed()).toBe(false)
  expect(pause).toHaveBeenCalled()
  expect(
    send.mock.calls.filter(([, options]) => options?.method === 'PATCH'),
  ).toHaveLength(count)
})

it('recovers a lost claim response by reading its receipt without another takeover', async () => {
  const normal = send.getMockImplementation()
  let lost = false
  send.mockImplementation(async (path, options) => {
    const result = await normal(path, options)
    if (options?.method === 'POST' && !lost) {
      lost = true
      throw new Error('lost response')
    }
    return result
  })
  controller.start(store)
  await settle()
  expect(await controller.explicitPlay()).toBe(true)
  expect(
    send.mock.calls.filter(([, options]) => options?.method === 'POST'),
  ).toHaveLength(1)
  expect(controller.epoch()).toBe(1)
})

it('never reacquires ownership when a lost claim was superseded', async () => {
  const normal = send.getMockImplementation()
  send.mockImplementation(async (path, options) => {
    const result = await normal(path, options)
    if (options?.method === 'POST') {
      remote = {
        ...remote,
        ownerClientId: 'other',
        epoch: 2,
        revision: 2,
        lastClaimRequestId: 'new',
      }
      throw new Error('lost response')
    }
    return result
  })
  controller.start(store)
  await settle()
  expect(await controller.explicitPlay()).toBe(false)
  expect(controller.isAllowed()).toBe(false)
  expect(
    send.mock.calls.filter(([, options]) => options?.method === 'POST'),
  ).toHaveLength(1)
})

it('continues locally offline and checks ownership before reconnecting saves', async () => {
  controller.start(store)
  await settle()
  await controller.explicitPlay()
  await settle()
  const normal = send.getMockImplementation()
  send.mockRejectedValue(new Error('offline'))
  controller.updateProgress('duplicate', 25000, 'playing')
  await settle()
  expect(controller.isAllowed()).toBe(true)
  expect(
    JSON.parse(localStorage.getItem(playbackCacheKey('user'))).positionMs,
  ).toBe(25000)
  remote = {
    ...remote,
    ownerClientId: 'other',
    epoch: 2,
    revision: remote.revision + 1,
  }
  send.mockImplementation(normal)
  await controller.refresh()
  await settle()
  expect(controller.isAllowed()).toBe(false)
  expect(remote.positionMs).not.toBe(25000)
})

it('clears ownership and local queue when the authenticated user changes', async () => {
  controller.start(store)
  await settle()
  await controller.explicitPlay()
  await settle()
  user = 'other-user'
  await controller.refresh()
  expect(controller.isAllowed()).toBe(false)
  expect(store.getState().player.queue).toEqual([])
  expect(controller.epoch()).toBe(0)
})

it('ignores progress from a previously selected occurrence', async () => {
  controller.start(store)
  await settle()
  await controller.explicitPlay()
  await settle()
  controller.updateProgress('first', 99999, 'playing')
  await settle()
  expect(remote.positionMs).not.toBe(99999)
})

it('retains the resume checkpoint until the selected media finishes seeking', async () => {
  remote = saved()
  controller.start(store)
  await settle()
  await controller.resumeHere()
  await settle()
  controller.updateProgress('duplicate', 0, 'paused')
  await settle()
  expect(remote.positionMs).toBe(45678)
  expect(controller.wantsPlayback()).toBe(true)
  controller.restoreComplete('duplicate', 45000, 1)
  expect(controller.isRestoring()).toBe(true)
  controller.restoreComplete('duplicate', 45678, 2)
  await settle()
  expect(controller.isRestoring()).toBe(false)
  expect(remote.positionMs).toBe(45678)
})
