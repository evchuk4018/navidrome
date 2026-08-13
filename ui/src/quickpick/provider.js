import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

const jsonRequest = (path, options = {}) =>
  httpClient(`${REST_URL}${path}`, options).then(({ json }) => json)

export const getQuickPick = () => jsonRequest('/quick-pick')

export const recordPlaylistPlay = (playlistId) =>
  httpClient(`${REST_URL}/playlist/${playlistId}/plays`, { method: 'POST' })

export const createPersonalRadio = (seedMediaFileId) =>
  jsonRequest('/personal-radio/sessions', {
    method: 'POST',
    body: JSON.stringify({ seedMediaFileId }),
  })

export const refillPersonalRadio = (sessionId) =>
  jsonRequest(`/personal-radio/sessions/${sessionId}`)

export const sendRadioFeedback = (sessionId, feedback) =>
  httpClient(`${REST_URL}/personal-radio/sessions/${sessionId}/feedback`, {
    method: 'POST',
    body: JSON.stringify(feedback),
  })

export const radioSongs = (response) => {
  const data = {}
  const ids = []
  response.items
    .filter(
      (item) =>
        item.song && (item.status === 'ready' || item.status === 'played'),
    )
    .forEach((item) => {
      const key = `radio-${item.id}`
      data[key] = {
        ...item.song,
        radioSessionId: response.session.id,
        radioItemId: item.id,
        radioItemType: item.type,
      }
      ids.push(key)
    })
  return { data, ids }
}
