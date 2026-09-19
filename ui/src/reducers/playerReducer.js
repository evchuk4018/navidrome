import { v4 as uuidv4 } from 'uuid'
import subsonic from '../subsonic'
import { decisionService } from '../transcode'
import {
  PLAYER_ADD_TRACKS,
  PLAYER_CLEAR_QUEUE,
  PLAYER_CURRENT,
  PLAYER_PLAY_NEXT,
  PLAYER_PLAY_TRACKS,
  PLAYER_SET_TRACK,
  PLAYER_SET_VOLUME,
  PLAYER_SYNC_QUEUE,
  PLAYER_SET_MODE,
  PLAYER_REFRESH_QUEUE,
  PLAYER_SET_RADIO_SESSION,
  PLAYER_SET_RADIO_PLANNING,
  PLAYER_SYNC_RADIO_TRACKS,
  PLAYER_RESOLVE_QUEUE_URLS,
  PLAYER_SET_RADIO_MODE,
  PLAYER_SET_RADIO_AUTOPLAY,
  PLAYER_END_RADIO_SESSION,
  PLAYER_REMOVE_RADIO_ITEM,
} from '../actions'
import config from '../config'

const initialState = {
  queue: [],
  current: {},
  clear: false,
  volume: config.defaultUIVolume / 100,
  savedPlayIndex: 0,
  radioSession: null,
}

// The music-player dependency uses musicSrc as a fallback identity when it
// reconciles a quiet queue update. A null source is shared by every pending
// radio item, so use one stable never-resolving source per item instead.
const pendingRadioMusicSources = new Map()

const pendingRadioMusicSrc = (item) => {
  const key = JSON.stringify([
    item.radioSessionId || '',
    item.radioItemId || item.id || item.name || '',
  ])
  let source = pendingRadioMusicSources.get(key)
  if (!source) {
    source = () => new Promise(() => {})
    pendingRadioMusicSources.set(key, source)
  }
  return source
}

const pad = (value) => {
  const str = value.toString()
  if (str.length === 1) {
    return `0${str}`
  } else {
    return str
  }
}

const makeMusicSrc = (trackId) =>
  decisionService.getProfile()
    ? () =>
        decisionService
          .resolveStreamUrl(trackId)
          .catch(() => subsonic.streamUrl(trackId))
    : subsonic.streamUrl(trackId)

const mapToAudioLists = (item) => {
  // If item comes from a playlist, trackId is mediaFileId
  const trackId = item.mediaFileId || item.id

  if (item.isRadio || item.radioPending) {
    return {
      trackId,
      uuid: uuidv4(),
      name: item.name || item.title,
      song: item,
      musicSrc: item.radioPending
        ? pendingRadioMusicSrc(item)
        : item.streamUrl || (trackId ? makeMusicSrc(trackId) : null),
      singer: item.artist || '',
      cover: item.cover,
      isRadio: true,
      radioPending: item.radioPending || false,
      radioSessionId: item.radioSessionId,
      radioItemId: item.radioItemId,
      radioItemType: item.radioItemType,
      radioTrackKey: item.radioTrackKey,
    }
  }

  const { lyrics } = item
  let lyricText = ''

  if (lyrics) {
    const structured = JSON.parse(lyrics)
    for (const structuredLyric of structured) {
      if (structuredLyric.synced) {
        for (const line of structuredLyric.line) {
          let time = Math.floor(line.start / 10)
          const ms = time % 100
          time = Math.floor(time / 100)
          const sec = time % 60
          time = Math.floor(time / 60)
          const min = time % 60

          ms.toString()
          lyricText += `[${pad(min)}:${pad(sec)}.${pad(ms)}] ${line.value}\n`
        }
      }
    }
  }

  return {
    trackId,
    uuid: uuidv4(),
    song: item,
    name: item.title,
    lyric: lyricText,
    singer: item.artist,
    duration: item.duration,
    musicSrc: makeMusicSrc(trackId),
    cover: subsonic.getCoverArtUrl(
      {
        id: trackId,
        updatedAt: item.updatedAt,
        album: item.album,
      },
      300,
    ),
    radioSessionId: item.radioSessionId,
    radioItemId: item.radioItemId,
    radioItemType: item.radioItemType,
    radioPending: item.radioPending || false,
  }
}

const reduceClearQueue = () => ({ ...initialState, clear: true })

const reducePlayTracks = (state, { data, id }) => {
  let playIndex = 0
  const queue = Object.keys(data).map((key, idx) => {
    if (key === id) {
      playIndex = idx
    }
    return mapToAudioLists(data[key])
  })
  return {
    ...state,
    queue,
    playIndex,
    clear: true,
    radioSession: null,
  }
}

const reduceSetTrack = (state, { data }) => {
  return {
    ...state,
    queue: [mapToAudioLists(data)],
    playIndex: 0,
    clear: true,
    radioSession: null,
  }
}

const reduceAddTracks = (state, { data }) => {
  const appended = Object.keys(data).map((id) => mapToAudioLists(data[id]))
  return { ...state, queue: [...state.queue, ...appended], clear: false }
}

// Replaces or appends radio items by radioItemId. Authoritative responses also
// remove failed/obsolete future rows for this session, while preserving the
// current row and all ordinary playback rows.
const reduceSyncRadioTracks = (state, { data, meta = {} }) => {
  const ids = Object.keys(data)
  const sessionId = meta.sessionId
  const currentSessionId = state.radioSession?.id
  if (sessionId && currentSessionId && sessionId !== currentSessionId) {
    return state
  }
  const revision = Number(meta.revision)
  const currentRevision = Number(state.radioSession?.revision)
  if (
    Number.isFinite(revision) &&
    Number.isFinite(currentRevision) &&
    revision < currentRevision
  ) {
    return state
  }
  if (!ids.length && !meta.authoritative) return state
  const queue = [...state.queue]
  let requiresReplacement = false
  const incomingRadioIds = new Set()
  const byRadioItemId = new Map(
    queue.map((item) => [item.radioItemId, item]).filter(([id]) => id),
  )
  ids.forEach((id) => {
    const next = mapToAudioLists(data[id])
    if (!next.radioItemId) return
    incomingRadioIds.add(next.radioItemId)
    const existing = byRadioItemId.get(next.radioItemId)
    if (existing) {
      const index = queue.findIndex(
        (item) => item.radioItemId === next.radioItemId,
      )
      if (index >= 0) {
        if (
          !!existing.radioPending === !!next.radioPending &&
          existing.trackId === next.trackId
        ) {
          return
        }
        requiresReplacement = true
        queue[index] = { ...next, uuid: existing.uuid }
        return
      }
    }
    queue.push(next)
  })
  if (meta.authoritative && sessionId) {
    const currentUuid = state.current?.uuid
    const filtered = queue.filter((item) => {
      if (item.radioSessionId !== sessionId || !item.radioItemId) return true
      if (item.uuid === currentUuid) return true
      return incomingRadioIds.has(item.radioItemId)
    })
    if (filtered.length !== queue.length) requiresReplacement = true
    queue.splice(0, queue.length, ...filtered)
  }
  // Radio updates use the player's quiet replacement path. This keeps the
  // currently playing seed alive while replacing a pending placeholder in the
  // music player's internal list instead of appending a second copy.
  const unchanged =
    queue.length === state.queue.length &&
    queue.every((item, index) => item === state.queue[index])
  if (unchanged && !Number.isFinite(revision)) return state
  return {
    ...state,
    queue,
    clear: requiresReplacement,
    radioSession: state.radioSession
      ? {
          ...state.radioSession,
          ...(Number.isFinite(revision) ? { revision } : {}),
        }
      : state.radioSession,
  }
}

const reduceSetRadioMode = (state, { data }) =>
  state.radioSession
    ? {
        ...state,
        radioSession: {
          ...state.radioSession,
          mode: data,
        },
      }
    : state

const reduceSetRadioAutoplay = (state, { data }) =>
  state.radioSession
    ? {
        ...state,
        radioSession: {
          ...state.radioSession,
          autoplay: data !== false,
        },
      }
    : state

const reduceEndRadioSession = (state) => {
  const sessionId = state.radioSession?.id
  if (!sessionId) return state
  const currentUuid = state.current?.uuid
  return {
    ...state,
    radioSession: null,
    queue: state.queue.filter(
      (item) => item.radioSessionId !== sessionId || item.uuid === currentUuid,
    ),
    clear: true,
  }
}

const reduceRemoveRadioItem = (state, { data }) => {
  const itemId = typeof data === 'string' ? data : data?.itemId
  const force = typeof data === 'object' && data?.force
  if (!itemId) return state
  const currentUuid = state.current?.uuid
  const queue = state.queue.filter(
    (item) =>
      item.radioItemId !== itemId || (!force && item.uuid === currentUuid),
  )
  return queue.length === state.queue.length
    ? state
    : { ...state, queue, clear: true }
}

const reducePlayNext = (state, { data }) => {
  const newTracks = Object.keys(data).map((id) => mapToAudioLists(data[id]))
  const newQueue = []
  const current = state.current || {}
  let foundPos = false
  state.queue.forEach((item) => {
    newQueue.push(item)
    if (item.uuid === current.uuid) {
      foundPos = true
      newQueue.push(...newTracks)
    }
  })
  if (!foundPos) {
    newQueue.push(...newTracks)
  }

  return {
    ...state,
    queue: newQueue,
    clear: true,
  }
}

const reduceSetVolume = (state, { data: { volume } }) => {
  return {
    ...state,
    volume,
  }
}

const reduceSyncQueue = (state, { data: { audioInfo, audioLists } }) => {
  // Keep clear and playIndex alive when there is a pending track switch.
  // A switch is pending when playIndex is set AND either:
  //   - playIndex differs from savedPlayIndex, OR
  //   - clear is true (a new queue was loaded, e.g. after clearQueue + playTracks)
  // The clear check handles the edge case where both playIndex and
  // savedPlayIndex are 0 (close player then play a new album from track 1).
  const hasPendingSwitch =
    state.playIndex != null &&
    (state.clear || state.playIndex !== state.savedPlayIndex)
  return {
    ...state,
    queue: audioLists,
    clear: hasPendingSwitch ? state.clear : false,
    playIndex: hasPendingSwitch ? state.playIndex : undefined,
  }
}

const reduceCurrent = (state, { data }) => {
  const current = data.ended ? {} : data
  const savedPlayIndex = state.queue.findIndex(
    (item) => item.uuid === current.uuid,
  )
  // When a track selection is pending (playIndex is set), keep it alive
  // until the music player confirms it actually switched to the requested
  // track. Without this, a premature onAudioPlay callback for the
  // still-playing old track would overwrite the pending selection.
  const pending = state.playIndex != null && savedPlayIndex !== state.playIndex
  return {
    ...state,
    current,
    playIndex: pending ? state.playIndex : undefined,
    clear: pending ? state.clear : false,
    savedPlayIndex: pending ? state.savedPlayIndex : savedPlayIndex,
    volume: data.volume,
  }
}

const reduceMode = (state, { data: { mode } }) => {
  return {
    ...state,
    mode,
  }
}

export const playerReducer = (previousState = initialState, payload) => {
  const { type } = payload
  switch (type) {
    case PLAYER_CLEAR_QUEUE:
      return reduceClearQueue()
    case PLAYER_PLAY_TRACKS:
      return reducePlayTracks(previousState, payload)
    case PLAYER_SET_TRACK:
      return reduceSetTrack(previousState, payload)
    case PLAYER_ADD_TRACKS:
      return reduceAddTracks(previousState, payload)
    case PLAYER_SYNC_RADIO_TRACKS:
      return reduceSyncRadioTracks(previousState, payload)
    case PLAYER_SET_RADIO_MODE:
      return reduceSetRadioMode(previousState, payload)
    case PLAYER_SET_RADIO_AUTOPLAY:
      return reduceSetRadioAutoplay(previousState, payload)
    case PLAYER_END_RADIO_SESSION:
      return reduceEndRadioSession(previousState)
    case PLAYER_REMOVE_RADIO_ITEM:
      return reduceRemoveRadioItem(previousState, payload)
    case PLAYER_PLAY_NEXT:
      return reducePlayNext(previousState, payload)
    case PLAYER_SET_VOLUME:
      return reduceSetVolume(previousState, payload)
    case PLAYER_SYNC_QUEUE:
      return reduceSyncQueue(previousState, payload)
    case PLAYER_CURRENT:
      return reduceCurrent(previousState, payload)
    case PLAYER_SET_MODE:
      return reduceMode(previousState, payload)
    case PLAYER_REFRESH_QUEUE: {
      const resolvedUrls = payload.data || {}
      return {
        ...previousState,
        queue: previousState.queue.map((item) => ({
          ...item,
          musicSrc: item.isRadio
            ? item.musicSrc
            : resolvedUrls[item.trackId] || subsonic.streamUrl(item.trackId),
        })),
        clear: true,
        autoPlay: false,
        playIndex:
          previousState.savedPlayIndex >= 0 ? previousState.savedPlayIndex : 0,
      }
    }
    case PLAYER_SET_RADIO_SESSION: {
      const session = payload.data
      if (!session?.id) return previousState
      // A new Quick Pick selection is not confirmed by CURRENT yet, so use
      // its pending index before falling back to the last confirmed index.
      const seedIndex =
        previousState.playIndex != null
          ? previousState.playIndex
          : previousState.savedPlayIndex || 0
      return {
        ...previousState,
        radioSession: session,
        queue: session.seedItemId
          ? previousState.queue.map((item, index) =>
              index === seedIndex
                ? {
                    ...item,
                    radioSessionId: session.id,
                    radioItemId: session.seedItemId,
                    radioItemType: 'seed',
                    song: {
                      ...item.song,
                      radioSessionId: session.id,
                      radioItemId: session.seedItemId,
                      radioItemType: 'seed',
                    },
                  }
                : item,
            )
          : previousState.queue,
      }
    }
    case PLAYER_SET_RADIO_PLANNING:
      return {
        ...previousState,
        radioSession: previousState.radioSession
          ? {
              ...previousState.radioSession,
              planningStatus: payload.data,
            }
          : previousState.radioSession,
      }
    case PLAYER_RESOLVE_QUEUE_URLS: {
      const resolvedUrls = payload.data || {}
      const ids = Object.keys(resolvedUrls)
      if (!ids.length) return previousState

      let changed = false
      const queue = previousState.queue.map((item) => {
        const url = resolvedUrls[item.trackId]
        if (item.isRadio || !url || typeof item.musicSrc !== 'function') {
          return item
        }
        changed = true
        return { ...item, musicSrc: url }
      })
      if (!changed) return previousState

      // clear drives the player's quiet replacement path so the materialized
      // list replaces its internal list without restarting playback.
      return { ...previousState, queue, clear: true }
    }
    default:
      return previousState
  }
}
