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
      musicSrc: item.radioPending ? null : item.streamUrl,
      singer: item.artist || '',
      cover: item.cover,
      isRadio: true,
      radioPending: item.radioPending || false,
      radioSessionId: item.radioSessionId,
      radioItemId: item.radioItemId,
      radioItemType: item.radioItemType,
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

// Replaces or appends radio items by radioItemId so pending placeholders
// become playable in place when their download resolves. The placeholder's
// uuid is preserved so the player treats the resolved track as the same item.
const reduceSyncRadioTracks = (state, { data }) => {
  const ids = Object.keys(data)
  if (!ids.length) return state
  const queue = [...state.queue]
  let requiresReplacement = false
  const byRadioItemId = new Map(
    queue.map((item) => [item.radioItemId, item]).filter(([id]) => id),
  )
  ids.forEach((id) => {
    const next = mapToAudioLists(data[id])
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
  // Radio updates use the player's quiet replacement path. This keeps the
  // currently playing seed alive while replacing a pending placeholder in the
  // music player's internal list instead of appending a second copy.
  if (queue.every((item, index) => item === state.queue[index])) return state
  return { ...state, queue, clear: requiresReplacement }
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
      return {
        ...previousState,
        radioSession: session,
        queue: previousState.queue.map((item, index) =>
          index === (previousState.savedPlayIndex || 0)
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
        ),
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
    default:
      return previousState
  }
}
