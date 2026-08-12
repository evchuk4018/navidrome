import httpClient from '../dataProvider/httpClient'
import { REST_URL } from '../consts'

const request = (path, options) =>
  httpClient(`${REST_URL}${path}`, options).then(({ json }) => json)

export const search = (query) =>
  request(`/music/search?q=${encodeURIComponent(query)}`)

export const getArtist = (id) =>
  request(`/music/artist/${encodeURIComponent(id)}`)

export const getAlbum = (id) =>
  request(`/music/album/${encodeURIComponent(id)}`)

export const createDownload = (kind, id) =>
  request('/music/downloads', {
    method: 'POST',
    headers: new Headers({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ kind, id }),
  })

export const listDownloads = () => request('/music/downloads?limit=20')
