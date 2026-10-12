import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import { playbackSession } from '../playbacksession/controller'

const ownershipOptions = () => {
  const epoch = playbackSession.epoch()
  return epoch
    ? { headers: new Headers({ 'X-ND-Playback-Epoch': String(epoch) }) }
    : {}
}

const jsonRequest = (path, options = {}) =>
  httpClient(`${REST_URL}${path}`, options).then(({ json }) => json)

export const getQuickPick = () => jsonRequest('/quick-pick')

export const recordQuickPickImpressions = (viewId, itemKeys) =>
  httpClient(`${REST_URL}/quick-pick/impressions`, {
    method: 'POST',
    body: JSON.stringify({ viewId, itemKeys }),
  })

export const recordQuickPickClick = (viewId, itemKey) =>
  httpClient(`${REST_URL}/quick-pick/clicks`, {
    method: 'POST',
    body: JSON.stringify({ viewId, itemKey }),
  })

export const recordPlaylistPlay = (playlistId) =>
  httpClient(`${REST_URL}/playlist/${playlistId}/plays`, { method: 'POST' })

export const createPersonalRadio = async (seedMediaFileId, mode) => {
  if (!(await playbackSession.explicitPlay()))
    throw new Error('Playback moved to another device')
  return jsonRequest('/personal-radio/sessions', {
    ...ownershipOptions(),
    method: 'POST',
    body: JSON.stringify({ seedMediaFileId, ...(mode ? { mode } : {}) }),
  })
}

export const refillPersonalRadio = (sessionId, context = {}) =>
  jsonRequest(`/personal-radio/sessions/${sessionId}/refill`, {
    ...ownershipOptions(),
    method: 'POST',
    body: JSON.stringify(context),
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

export const sendRadioFeedback = (sessionId, feedback, options = {}) =>
  httpClient(`${REST_URL}/personal-radio/sessions/${sessionId}/feedback`, {
    ...options,
    method: 'POST',
    body: JSON.stringify(feedback),
  })

export const radioSongs = (response) => {
  const data = {}
  const ids = []
  const orderedItems = [...response.items].sort(
    (left, right) => left.position - right.position,
  )
  orderedItems
    .filter((item) => item.status !== 'failed')
    .forEach((item) => {
      const key = `radio-${item.id}`
      const pending = item.status === 'downloading'
      data[key] = {
        ...item.song,
        radioSessionId: response.session.id,
        radioItemId: item.id,
        radioItemType: item.type,
        radioPending: pending,
      }
      if (pending) {
        data[key] = {
          ...data[key],
          id: undefined,
          name: data[key].title
            ? `Downloading: ${data[key].title}`
            : 'Pending download…',
          streamUrl: null,
        }
      }
      ids.push(key)
    })
  return { data, ids }
}
