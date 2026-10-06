import { useCallback, useEffect, useRef, useState } from 'react'
import { useDataProvider, useNotify } from 'react-admin'
import subsonic from '../subsonic'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

export const useToggleLove = (resource, record = {}) => {
  const [loading, setLoading] = useState(false)
  const notify = useNotify()

  const mountedRef = useRef(false)
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const dataProvider = useDataProvider()

  const refreshRecord = useCallback(() => {
    const promises = []

    // Always refresh the original resource
    const isVideo = resource === 'hometubeVideo' || record.source === 'hometube'
    const params = { id: isVideo ? record.videoId || record.id : record.id }
    if (record.playlistId && !isVideo) {
      params.filter = { playlist_id: record.playlistId }
    }
    promises.push(
      dataProvider.getOne(isVideo ? 'hometubeVideo' : resource, params),
    )

    if (isVideo && record.playlistId) {
      promises.push(
        dataProvider.getOne('playlistTrack', {
          id: record.id,
          filter: { playlist_id: record.playlistId },
        }),
      )
    }
    // If we have a mediaFileId, also refresh the song
    if (record.mediaFileId) {
      promises.push(dataProvider.getOne('song', { id: record.mediaFileId }))
    }

    return Promise.all(promises)
      .catch((e) => {
        // eslint-disable-next-line no-console
        console.log('Error encountered: ' + e)
      })
      .finally(() => {
        if (mountedRef.current) {
          setLoading(false)
        }
      })
  }, [
    dataProvider,
    record.mediaFileId,
    record.id,
    record.playlistId,
    record.source,
    record.videoId,
    resource,
  ])

  const toggleLove = () => {
    const video = resource === 'hometubeVideo' || record.source === 'hometube'
    const toggle = video
      ? async (id) => {
          await httpClient(
            `${REST_URL}/hometubeVideo/${encodeURIComponent(id)}`,
            { method: 'PUT', body: JSON.stringify(record.video || record) },
          )
          return httpClient(
            `${REST_URL}/hometubeVideo/${encodeURIComponent(id)}/favorite`,
            {
              method: 'PUT',
              body: JSON.stringify({ starred: !record.starred }),
            },
          )
        }
      : record.starred
        ? subsonic.unstar
        : subsonic.star
    const id = record.videoId || record.mediaFileId || record.id

    setLoading(true)
    return toggle(id)
      .then(refreshRecord)
      .catch((e) => {
        // eslint-disable-next-line no-console
        console.log('Error toggling love: ', e)
        notify('ra.page.error', 'warning')
        if (mountedRef.current) {
          setLoading(false)
        }
      })
  }

  return [toggleLove, loading]
}
