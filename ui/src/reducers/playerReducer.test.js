import { describe, it, expect } from 'vitest'
import { playerReducer } from './playerReducer'
import {
  PLAYER_SYNC_QUEUE,
  PLAYER_CURRENT,
  PLAYER_REFRESH_QUEUE,
  PLAYER_ADD_TRACKS,
  PLAYER_PLAY_TRACKS,
  PLAYER_SET_RADIO_SESSION,
  PLAYER_SET_RADIO_PLANNING,
  PLAYER_SYNC_RADIO_TRACKS,
  PLAYER_RESOLVE_QUEUE_URLS,
} from '../actions'

describe('playerReducer', () => {
  it('updates radio planning status without changing the queue', () => {
    const state = {
      queue: [{ trackId: 'seed' }],
      radioSession: { id: 'session-1', seedItemId: 'item-1' },
    }
    const result = playerReducer(state, {
      type: PLAYER_SET_RADIO_PLANNING,
      data: 'downloading',
    })

    expect(result.queue).toEqual(state.queue)
    expect(result.radioSession).toEqual({
      ...state.radioSession,
      planningStatus: 'downloading',
    })
  })

  it('appends ready radio tracks without restarting the current iOS playback', () => {
    const current = { uuid: 'seed-uuid', trackId: 'seed', paused: false }
    const queue = [current]
    const state = {
      queue,
      current,
      savedPlayIndex: 0,
      playIndex: undefined,
      clear: false,
      autoPlay: false,
      radioSession: { id: 'session-1' },
    }
    const result = playerReducer(state, {
      type: PLAYER_ADD_TRACKS,
      data: {
        'radio-item-1': {
          id: 'track-1',
          title: 'Fresh Track',
          artist: 'Artist',
          radioItemId: 'item-1',
        },
      },
    })

    expect(result.queue).not.toBe(queue)
    expect(result.queue[0]).toBe(current)
    expect(result.current).toBe(current)
    expect(result.savedPlayIndex).toBe(0)
    expect(result.playIndex).toBeUndefined()
    expect(result.clear).toBe(false)
    expect(result.autoPlay).toBe(false)
    expect(result.queue[1].radioItemId).toBe('item-1')
  })

  it('replaces a pending radio placeholder in place when the download resolves', () => {
    const state = {
      queue: [
        {
          trackId: undefined,
          uuid: 'placeholder-uuid',
          name: 'Pending download…',
          musicSrc: null,
          isRadio: true,
          radioPending: true,
          radioItemId: 'item-1',
        },
      ],
      current: {},
      savedPlayIndex: 0,
      clear: false,
      autoPlay: false,
      radioSession: { id: 'session-1' },
    }
    const result = playerReducer(state, {
      type: PLAYER_SYNC_RADIO_TRACKS,
      data: {
        'radio-item-1': {
          id: 'track-1',
          title: 'Fresh Track',
          artist: 'Artist',
          radioItemId: 'item-1',
          radioPending: false,
        },
      },
    })

    expect(result.queue).toHaveLength(1)
    expect(result.queue[0].radioItemId).toBe('item-1')
    expect(result.queue[0].radioPending).toBe(false)
    expect(result.queue[0].name).toBe('Fresh Track')
    expect(result.queue[0].singer).toBe('Artist')
    expect(result.queue[0].musicSrc).not.toBeNull()
    expect(result.queue[0].uuid).toBe('placeholder-uuid')
    expect(result.clear).toBe(true)
  })

  it('ignores an unchanged radio sync so the player does not append duplicates', () => {
    const state = {
      queue: [
        {
          trackId: 'track-1',
          uuid: 'track-uuid',
          radioItemId: 'item-1',
          radioPending: false,
          musicSrc: 'stream-url',
        },
      ],
      current: {},
      clear: false,
      radioSession: { id: 'session-1' },
    }
    const result = playerReducer(state, {
      type: PLAYER_SYNC_RADIO_TRACKS,
      data: {
        'radio-item-1': {
          id: 'track-1',
          title: 'Fresh Track',
          radioItemId: 'item-1',
          radioPending: false,
        },
      },
    })

    expect(result).toBe(state)
  })

  it('associates a Quick Pick seed with the pending play index', () => {
    const state = {
      queue: [
        { trackId: 'old-1', uuid: 'old-1-uuid' },
        { trackId: 'old-2', uuid: 'old-2-uuid' },
        { trackId: 'old-3', uuid: 'old-3-uuid' },
      ],
      current: { uuid: 'old-3-uuid' },
      savedPlayIndex: 2,
      clear: false,
      radioSession: null,
    }
    const playing = playerReducer(state, {
      type: PLAYER_PLAY_TRACKS,
      id: 'new-seed',
      data: {
        'new-seed': {
          id: 'new-seed',
          title: 'New Seed',
          artist: 'Artist',
        },
      },
    })

    expect(playing.playIndex).toBe(0)
    expect(playing.savedPlayIndex).toBe(2)

    const withSession = playerReducer(playing, {
      type: PLAYER_SET_RADIO_SESSION,
      data: {
        id: 'session-1',
        seedItemId: 'seed-item-1',
      },
    })

    expect(withSession.queue).toHaveLength(1)
    expect(withSession.queue[0].radioItemId).toBe('seed-item-1')
    expect(withSession.queue[0].radioItemType).toBe('seed')

    const synced = playerReducer(withSession, {
      type: PLAYER_SYNC_RADIO_TRACKS,
      data: {
        'radio-seed-item-1': {
          id: 'new-seed',
          title: 'New Seed',
          artist: 'Artist',
          radioSessionId: 'session-1',
          radioItemId: 'seed-item-1',
          radioItemType: 'seed',
          radioPending: false,
        },
      },
    })

    expect(synced).toBe(withSession)
    expect(synced.queue).toHaveLength(1)
  })

  it('shows the pending download with its recommendation metadata', () => {
    const state = {
      queue: [],
      current: {},
      savedPlayIndex: 0,
      clear: false,
      autoPlay: false,
      radioSession: { id: 'session-1' },
    }
    const result = playerReducer(state, {
      type: PLAYER_SYNC_RADIO_TRACKS,
      data: {
        'radio-item-2': {
          radioItemId: 'item-2',
          radioPending: true,
          name: 'Downloading: Fresh Track',
          title: 'Fresh Track',
          artist: 'Fresh Artist',
          id: undefined,
        },
      },
    })

    expect(result.queue[0].radioPending).toBe(true)
    expect(result.queue[0].name).toBe('Downloading: Fresh Track')
    expect(result.queue[0].singer).toBe('Fresh Artist')
    expect(typeof result.queue[0].musicSrc).toBe('function')
  })

  it('keeps pending radio sources unique and stable across queue replacement', () => {
    const state = {
      queue: [],
      current: {},
      clear: false,
      radioSession: { id: 'session-1' },
    }
    const pending = {
      'radio-item-1': {
        radioSessionId: 'session-1',
        radioItemId: 'item-1',
        radioPending: true,
        name: 'Pending One',
      },
      'radio-item-2': {
        radioSessionId: 'session-1',
        radioItemId: 'item-2',
        radioPending: true,
        name: 'Pending Two',
      },
      'radio-item-3': {
        radioSessionId: 'session-1',
        radioItemId: 'item-3',
        radioPending: true,
        name: 'Pending Three',
      },
    }
    const first = playerReducer(state, {
      type: PLAYER_SYNC_RADIO_TRACKS,
      data: pending,
    })
    const second = playerReducer(first, {
      type: PLAYER_SYNC_RADIO_TRACKS,
      data: {
        ...pending,
        'radio-item-1': {
          radioSessionId: 'session-1',
          radioItemId: 'item-1',
          radioPending: false,
          id: 'track-1',
          title: 'Ready One',
        },
      },
    })

    expect(first.queue[0].musicSrc).not.toBe(first.queue[1].musicSrc)
    expect(first.queue[1].musicSrc).not.toBe(first.queue[2].musicSrc)
    expect(second.queue[1].musicSrc).toBe(first.queue[1].musicSrc)
    expect(second.queue[2].musicSrc).toBe(first.queue[2].musicSrc)
    expect(second.queue[1].musicSrc).not.toBe(second.queue[2].musicSrc)
  })

  it('appends pending radio placeholders and keeps playing track untouched', () => {
    const current = {
      uuid: 'seed-uuid',
      trackId: 'seed',
      name: 'Seed Song',
      paused: false,
    }
    const state = {
      queue: [current],
      current,
      savedPlayIndex: 0,
      clear: false,
      radioSession: { id: 'session-1' },
    }
    const result = playerReducer(state, {
      type: PLAYER_SYNC_RADIO_TRACKS,
      data: {
        'radio-item-2': {
          radioItemId: 'item-2',
          radioPending: true,
          name: 'Pending download…',
          id: undefined,
        },
      },
    })

    expect(result.queue[0]).toBe(current)
    expect(result.queue[1].radioItemId).toBe('item-2')
    expect(result.queue[1].radioPending).toBe(true)
    expect(typeof result.queue[1].musicSrc).toBe('function')
  })

  describe('pending track selection survives SYNC_QUEUE and premature CURRENT', () => {
    // Simulates the real sequence when clicking a new song while one is playing:
    // 1. PLAYER_PLAY_TRACKS sets playIndex and clear
    // 2. PLAYER_SYNC_QUEUE fires when music player syncs its internal queue
    // 3. PLAYER_CURRENT fires for the OLD still-playing track
    // 4. PLAYER_CURRENT fires for the NEW track (player switched)
    const stateAfterPlayTracks = {
      queue: [
        { trackId: 's1', uuid: 'aaa', name: 'Song 1' },
        { trackId: 's2', uuid: 'bbb', name: 'Song 2' },
        { trackId: 's3', uuid: 'ccc', name: 'Song 3' },
      ],
      current: { uuid: 'ccc', name: 'Song 3' },
      playIndex: 0, // user clicked Song 1
      savedPlayIndex: 2, // Song 3 was playing
      clear: true,
      volume: 1,
    }

    it('SYNC_QUEUE preserves pending playIndex and clear', () => {
      const newQueue = [
        { trackId: 's1', uuid: 'xxx', name: 'Song 1' },
        { trackId: 's2', uuid: 'yyy', name: 'Song 2' },
        { trackId: 's3', uuid: 'zzz', name: 'Song 3' },
      ]
      const action = {
        type: PLAYER_SYNC_QUEUE,
        data: { audioInfo: {}, audioLists: newQueue },
      }
      const result = playerReducer(stateAfterPlayTracks, action)
      expect(result.playIndex).toBe(0)
      expect(result.clear).toBe(true)
      expect(result.queue).toBe(newQueue)
    })

    it('SYNC_QUEUE clears playIndex when no pending selection', () => {
      const stateNoPending = { ...stateAfterPlayTracks, playIndex: undefined }
      const action = {
        type: PLAYER_SYNC_QUEUE,
        data: { audioInfo: {}, audioLists: stateNoPending.queue },
      }
      const result = playerReducer(stateNoPending, action)
      expect(result.playIndex).toBeUndefined()
      expect(result.clear).toBe(false)
    })

    it('CURRENT for old track preserves pending playIndex', () => {
      // After SYNC_QUEUE, queue has new UUIDs. The old track's UUID (zzz)
      // is at index 2, but playIndex is 0. This is a premature callback.
      const stateAfterSync = {
        ...stateAfterPlayTracks,
        queue: [
          { trackId: 's1', uuid: 'xxx', name: 'Song 1' },
          { trackId: 's2', uuid: 'yyy', name: 'Song 2' },
          { trackId: 's3', uuid: 'zzz', name: 'Song 3' },
        ],
      }
      const action = {
        type: PLAYER_CURRENT,
        data: { uuid: 'zzz', name: 'Song 3', volume: 1 },
      }
      const result = playerReducer(stateAfterSync, action)
      expect(result.playIndex).toBe(0)
      expect(result.clear).toBe(true)
      expect(result.savedPlayIndex).toBe(2) // preserved from before
    })

    it('CURRENT for correct track consumes pending playIndex', () => {
      const stateAfterSync = {
        ...stateAfterPlayTracks,
        queue: [
          { trackId: 's1', uuid: 'xxx', name: 'Song 1' },
          { trackId: 's2', uuid: 'yyy', name: 'Song 2' },
          { trackId: 's3', uuid: 'zzz', name: 'Song 3' },
        ],
      }
      // Player switched to Song 1 (uuid 'xxx', index 0 == playIndex)
      const action = {
        type: PLAYER_CURRENT,
        data: { uuid: 'xxx', name: 'Song 1', volume: 1 },
      }
      const result = playerReducer(stateAfterSync, action)
      expect(result.playIndex).toBeUndefined()
      expect(result.clear).toBe(false)
      expect(result.savedPlayIndex).toBe(0)
      expect(result.current.name).toBe('Song 1')
    })
  })

  describe('play new album after closing player (issue #5440)', () => {
    it('SYNC_QUEUE preserves pending playIndex=0 after clearQueue', () => {
      // Scenario: user plays album A, advances to track 3, closes player,
      // then plays album B. After clearQueue, savedPlayIndex=0.
      // PLAYER_PLAY_TRACKS sets playIndex=0. SYNC_QUEUE must NOT clear it.
      const stateAfterClearThenPlay = {
        queue: [
          { trackId: 'b1', uuid: 'u1', name: 'B Song 1' },
          { trackId: 'b2', uuid: 'u2', name: 'B Song 2' },
          { trackId: 'b3', uuid: 'u3', name: 'B Song 3' },
        ],
        current: {},
        playIndex: 0,
        savedPlayIndex: 0, // reset by clearQueue
        clear: true,
        volume: 1,
      }

      const action = {
        type: PLAYER_SYNC_QUEUE,
        data: {
          audioInfo: {},
          audioLists: stateAfterClearThenPlay.queue,
        },
      }
      const result = playerReducer(stateAfterClearThenPlay, action)
      expect(result.playIndex).toBe(0)
      expect(result.clear).toBe(true)
    })

    it('CURRENT for wrong track preserves pending playIndex=0 after clearQueue', () => {
      // The music player fires onAudioPlay for the old track (at index 3)
      // before switching to the new track at index 0.
      const stateAfterClearThenPlay = {
        queue: [
          { trackId: 'b1', uuid: 'u1', name: 'B Song 1' },
          { trackId: 'b2', uuid: 'u2', name: 'B Song 2' },
          { trackId: 'b3', uuid: 'u3', name: 'B Song 3' },
          { trackId: 'b4', uuid: 'u4', name: 'B Song 4' },
        ],
        current: {},
        playIndex: 0,
        savedPlayIndex: 0,
        clear: true,
        volume: 1,
      }

      // Player reports track at index 3 as current (stale callback)
      const action = {
        type: PLAYER_CURRENT,
        data: { uuid: 'u4', name: 'B Song 4', volume: 1 },
      }
      const result = playerReducer(stateAfterClearThenPlay, action)
      expect(result.playIndex).toBe(0)
      expect(result.clear).toBe(true)
    })

    it('CURRENT for correct track consumes pending playIndex=0', () => {
      const stateAfterClearThenPlay = {
        queue: [
          { trackId: 'b1', uuid: 'u1', name: 'B Song 1' },
          { trackId: 'b2', uuid: 'u2', name: 'B Song 2' },
        ],
        current: {},
        playIndex: 0,
        savedPlayIndex: 0,
        clear: true,
        volume: 1,
      }

      // Player confirms it switched to track at index 0
      const action = {
        type: PLAYER_CURRENT,
        data: { uuid: 'u1', name: 'B Song 1', volume: 1 },
      }
      const result = playerReducer(stateAfterClearThenPlay, action)
      expect(result.playIndex).toBeUndefined()
      expect(result.clear).toBe(false)
      expect(result.savedPlayIndex).toBe(0)
    })
  })

  describe('PLAYER_REFRESH_QUEUE', () => {
    it('clamps negative savedPlayIndex to 0', () => {
      const state = {
        queue: [
          { trackId: 'song-1', musicSrc: 'old-url', uuid: 'a' },
          { trackId: 'song-2', musicSrc: 'old-url', uuid: 'b' },
        ],
        savedPlayIndex: -1,
        current: {},
        clear: false,
        volume: 1,
      }
      const action = { type: PLAYER_REFRESH_QUEUE, data: {} }
      const result = playerReducer(state, action)
      expect(result.playIndex).toBe(0)
    })

    it('preserves valid savedPlayIndex', () => {
      const state = {
        queue: [
          { trackId: 'song-1', musicSrc: 'old-url', uuid: 'a' },
          { trackId: 'song-2', musicSrc: 'old-url', uuid: 'b' },
        ],
        savedPlayIndex: 1,
        current: {},
        clear: false,
        volume: 1,
      }
      const action = { type: PLAYER_REFRESH_QUEUE, data: {} }
      const result = playerReducer(state, action)
      expect(result.playIndex).toBe(1)
    })

    it('uses savedPlayIndex of 0 correctly', () => {
      const state = {
        queue: [{ trackId: 'song-1', musicSrc: 'old-url', uuid: 'a' }],
        savedPlayIndex: 0,
        current: {},
        clear: false,
        volume: 1,
      }
      const action = { type: PLAYER_REFRESH_QUEUE, data: {} }
      const result = playerReducer(state, action)
      expect(result.playIndex).toBe(0)
    })
  })

  describe('PLAYER_RESOLVE_QUEUE_URLS', () => {
    it('materializes upcoming stream URLs without touching the current track', () => {
      const currentMusicSrc = () => Promise.resolve('http://a')
      const state = {
        queue: [
          {
            trackId: 'a',
            uuid: 'u-a',
            name: 'A',
            musicSrc: currentMusicSrc,
          },
          {
            trackId: 'b',
            uuid: 'u-b',
            name: 'B',
            musicSrc: () => Promise.resolve('http://b'),
          },
          {
            trackId: 'c',
            uuid: 'u-c',
            name: 'C',
            musicSrc: () => Promise.resolve('http://c'),
          },
        ],
        current: { uuid: 'u-a' },
        savedPlayIndex: 0,
        playIndex: undefined,
        clear: false,
        volume: 1,
      }
      const action = {
        type: PLAYER_RESOLVE_QUEUE_URLS,
        data: { b: 'http://b-resolved', c: 'http://c-resolved' },
      }
      const result = playerReducer(state, action)

      expect(result.queue).toHaveLength(3)
      expect(result.queue[0]).toBe(state.queue[0])
      expect(result.queue[0].musicSrc).toBe(currentMusicSrc)
      expect(result.queue[1].musicSrc).toBe('http://b-resolved')
      expect(result.queue[1].uuid).toBe('u-b')
      expect(result.queue[2].musicSrc).toBe('http://c-resolved')
      expect(result.savedPlayIndex).toBe(0)
      expect(result.playIndex).toBeUndefined()
      expect(result.current).toBe(state.current)
    })

    it('does not resolve radio or already-materialized items', () => {
      const fnMusicSrc = () => Promise.resolve('http://b')
      const state = {
        queue: [
          { trackId: 'radio', uuid: 'u-r', isRadio: true, musicSrc: 'stream' },
          { trackId: 'b', uuid: 'u-b', musicSrc: fnMusicSrc },
          { trackId: 'c', uuid: 'u-c', musicSrc: 'http://c' },
        ],
        savedPlayIndex: 0,
        clear: false,
        volume: 1,
      }
      const result = playerReducer(state, {
        type: PLAYER_RESOLVE_QUEUE_URLS,
        data: { radio: 'x', b: 'http://b-resolved', c: 'y' },
      })

      expect(result.queue[0].musicSrc).toBe('stream')
      expect(result.queue[1].musicSrc).toBe('http://b-resolved')
      expect(result.queue[2].musicSrc).toBe('http://c')
    })

    it('returns the same state when nothing is resolved', () => {
      const state = {
        queue: [
          { trackId: 'a', uuid: 'u-a', musicSrc: () => Promise.resolve('x') },
        ],
        savedPlayIndex: 0,
        clear: false,
        volume: 1,
      }
      const result = playerReducer(state, {
        type: PLAYER_RESOLVE_QUEUE_URLS,
        data: {},
      })

      expect(result).toBe(state)
    })
  })
})
