import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

const jsonRequest = (path, options = {}) =>
  httpClient(`${REST_URL}${path}`, options).then(({ json }) => json)

export const getQuickPick = () => jsonRequest('/quick-pick')

export const recordPlaylistPlay = (playlistId) =>
  httpClient(`${REST_URL}/playlist/${playlistId}/plays`, { method: 'POST' })

export const createPersonalRadio = (seedMediaFileId, mode) =>
  jsonRequest('/personal-radio/sessions', {
    method: 'POST',
    body: JSON.stringify({ seedMediaFileId, ...(mode ? { mode } : {}) }),
  })

export const refillPersonalRadio = (sessionId, context = {}) =>
  jsonRequest(`/personal-radio/sessions/${sessionId}/refill`, {
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

export const sendRadioFeedback = (sessionId, feedback) =>
  httpClient(`${REST_URL}/personal-radio/sessions/${sessionId}/feedback`, {
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
