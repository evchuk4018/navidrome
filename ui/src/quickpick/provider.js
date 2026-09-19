import { v4 as uuidv4 } from 'uuid'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

const jsonRequest = (path, options = {}) =>
  httpClient(`${REST_URL}${path}`, options).then(({ json }) => json)

export const getQuickPick = (options = {}) =>
  jsonRequest('/quick-pick', options)

export const recordQuickPickImpression = (viewId, itemKeys = []) =>
  jsonRequest('/quick-pick/impressions', {
    method: 'POST',
    body: JSON.stringify({ viewId, itemKeys }),
  })

export const recordPlaylistPlay = (playlistId) =>
  httpClient(`${REST_URL}/playlist/${playlistId}/plays`, { method: 'POST' })

export const createPersonalRadio = (seedOrOptions, mode) => {
  const options =
    seedOrOptions && typeof seedOrOptions === 'object'
      ? seedOrOptions
      : {
          seedMediaFileId: seedOrOptions,
          ...(mode ? { mode } : {}),
        }
  return jsonRequest('/personal-radio/sessions', {
    method: 'POST',
    body: JSON.stringify({
      ...options,
      clientRequestId: options.clientRequestId || uuidv4(),
    }),
  })
}

export const refillPersonalRadio = (sessionId, context = {}) =>
  jsonRequest(`/personal-radio/sessions/${sessionId}/refill`, {
    method: 'POST',
    body: JSON.stringify(context),
  })

export const endPersonalRadio = (sessionId, payload = {}) =>
  jsonRequest(`/personal-radio/sessions/${sessionId}/end`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })

export const radioErrorDetails = (error) => {
  const body = error?.body
  const bodyError = body?.error
  const message =
    (typeof body === 'string' && body) ||
    (typeof bodyError === 'string' && bodyError) ||
    bodyError?.message ||
    body?.message ||
    error?.message ||
    'Unknown error'

  return {
    status: error?.status,
    message,
    body,
  }
}

export const sendRadioFeedback = (sessionId, feedback) =>
  httpClient(`${REST_URL}/personal-radio/sessions/${sessionId}/feedback`, {
    method: 'POST',
    body: JSON.stringify(feedback),
  })

const isPlayableStatus = (status) =>
  !status || ['ready', 'available', 'played'].includes(status)

export const radioSongs = (response = {}) => {
  const data = {}
  const ids = []
  const seen = new Set()
  const allItems = Array.isArray(response.items) ? response.items : []
  const nextItems = Array.isArray(response.upNext) ? response.upNext : allItems
  // A session can retain the seed in `items` while exposing only ready
  // continuation rows in `upNext`. PendingItems are deliberately never added.
  const sourceItems = [
    ...allItems.filter((item) => item.type === 'seed'),
    ...nextItems,
  ]
  const orderedItems = sourceItems
    .filter((item) => {
      const identity = item?.id || item?.mediaFileId || item?.song?.id
      if (!identity || seen.has(identity)) return false
      seen.add(identity)
      return true
    })
    .sort((left, right) => (left.position || 0) - (right.position || 0))

  orderedItems
    .filter((item) => {
      if (!isPlayableStatus(item.status)) return false
      const songId = item.song?.id || item.mediaFileId || item.song?.mediaFileId
      return !!(songId || item.streamUrl || item.song?.streamUrl)
    })
    .forEach((item) => {
      const key = `radio-${item.id || item.mediaFileId || item.song?.id}`
      const songId = item.song?.id || item.mediaFileId || item.song?.mediaFileId
      const song = {
        ...(item.song || {}),
        ...(songId ? { id: songId } : {}),
        ...(item.streamUrl || item.song?.streamUrl
          ? { streamUrl: item.streamUrl || item.song.streamUrl }
          : {}),
      }
      data[key] = {
        ...song,
        radioSessionId: response.session?.id,
        radioItemId: item.id || item.mediaFileId || item.song?.id,
        radioItemType: item.type,
        radioTrackKey: item.trackKey || item.song?.radioTrackKey,
        isRadio: true,
        radioPending: false,
      }
      ids.push(key)
    })

  return {
    data,
    ids,
    revision: response.revision,
    sessionId: response.session?.id,
    authoritative: Array.isArray(response.upNext),
  }
}
