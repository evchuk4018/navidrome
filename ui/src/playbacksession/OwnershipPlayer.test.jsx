import React from 'react'
import { act, cleanup, render } from '@testing-library/react'
import { createStore } from 'redux'
import {
  MediaSessionCoordinator,
  useMediaSessionSource,
} from '../audioplayer/MediaSessionCoordinator'
import { playbackSession } from './controller'

const requests = vi.hoisted(() => ({ send: vi.fn() }))
vi.mock('../dataProvider/httpClient', () => ({
  clientUniqueId: 'tab',
  default: (...args) => requests.send(...args),
}))
let store, remote, element, media, writes, lost
const Source = () => {
  useMediaSessionSource({
    source: 'music',
    element,
    active: true,
    entryId: 'entry',
    metadata: { title: 'Song' },
    onOwnershipLost: lost,
  })
  return null
}
beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('userId', 'u')
  remote = null
  writes = []
  lost = vi.fn()
  const selected = {
    uuid: 'entry',
    source: 'music',
    trackId: 'song',
    name: 'Song',
    resumePositionMs: 22000,
    resumeToken: 1,
  }
  store = createStore(
    (state = { player: { queue: [selected], savedPlayIndex: 0 } }, action) =>
      action.type === 'playbackSessionChanged'
        ? { ...state, playbackSessionEvent: action.data }
        : state,
  )
  requests.send.mockImplementation(async (_url, options = {}) => {
    if (options.method === 'POST') {
      const body = JSON.parse(options.body)
      remote = {
        ...body.snapshot,
        epoch: 1,
        revision: 1,
        sequence: 0,
        ownerClientId: 'tab',
        lastClaimRequestId: body.requestId,
      }
    }
    if (options.method === 'PATCH') {
      const body = JSON.parse(options.body)
      remote = {
        ...remote,
        ...(body.snapshot || { positionMs: body.positionMs }),
        revision: remote.revision + 1,
      }
    }
    return { json: { session: remote } }
  })
  element = document.createElement('audio')
  element.ndPlaybackEntryId = 'entry'
  media = { paused: true, position: 0, reject: false }
  Object.defineProperties(element, {
    readyState: { configurable: true, value: 3 },
    networkState: { configurable: true, value: 2 },
    load: { configurable: true, value: vi.fn() },
    duration: { configurable: true, value: 120 },
    paused: { configurable: true, get: () => media.paused },
    currentTime: {
      configurable: true,
      get: () => media.position,
      set: (value) => {
        writes.push(value)
        media.position = value
      },
    },
    play: {
      configurable: true,
      value: vi.fn(() => {
        if (media.reject)
          return Promise.reject(
            Object.assign(new Error('blocked'), { name: 'NotAllowedError' }),
          )
        media.paused = false
        element.dispatchEvent(new Event('play'))
        return Promise.resolve()
      }),
    },
    pause: {
      configurable: true,
      value: vi.fn(() => {
        if (media.paused) return
        media.paused = true
        element.dispatchEvent(new Event('pause'))
      }),
    },
  })
  playbackSession.start(store)
})
afterEach(() => {
  playbackSession.stop()
  cleanup()
})

it('blocks hydration autoplay, restores once after explicit play, and cancels late playback after handoff', async () => {
  render(
    <MediaSessionCoordinator>
      <Source />
    </MediaSessionCoordinator>,
  )
  await act(async () => {
    await element.play()
  })
  expect(media.paused).toBe(true)
  expect(
    requests.send.mock.calls.some(([, options]) => options.method === 'POST'),
  ).toBe(false)
  await act(async () => {
    await playbackSession.explicitPlay()
  })
  expect(media.paused).toBe(false)
  expect(media.position).toBe(22)
  act(() => element.dispatchEvent(new Event('loadedmetadata')))
  expect(writes).toEqual([22])
  remote = { ...remote, ownerClientId: 'other', epoch: 2, revision: 20 }
  await act(async () => {
    store.dispatch({
      type: 'playbackSessionChanged',
      data: { userId: 'u', ownerClientId: 'other', epoch: 2 },
    })
    await Promise.resolve()
  })
  expect(media.paused).toBe(true)
  expect(lost).toHaveBeenCalled()
  await act(async () => {
    await element.play()
    element.dispatchEvent(new Event('canplay'))
  })
  expect(media.paused).toBe(true)
})

it('leaves rejected playback paused and waits for an explicit retry', async () => {
  media.reject = true
  render(
    <MediaSessionCoordinator>
      <Source />
    </MediaSessionCoordinator>,
  )
  await act(async () => {
    await playbackSession.explicitPlay()
    await Promise.resolve()
  })
  expect(media.paused).toBe(true)
  expect(playbackSession.wantsPlayback()).toBe(false)
  const attempts = element.play.mock.calls.length
  act(() => element.dispatchEvent(new Event('canplay')))
  expect(element.play.mock.calls.length).toBe(attempts)
  media.reject = false
  await act(async () => {
    await playbackSession.explicitPlay()
  })
  await act(async () => {
    await element.play()
  })
  expect(media.paused).toBe(false)
})
