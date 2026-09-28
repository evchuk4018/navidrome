import * as musicProvider from './provider'

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const RESOLUTION_TIMEOUT_MS = 90_000

const findLocalAfterConflict = async (request) => {
  const query = [request.artist, request.title].filter(Boolean).join(' ')
  const result = await musicProvider.search(query, { limit: 50 })
  const songs = result.results?.length
    ? result.results.filter((hit) => hit.kind === 'song').map((hit) => hit.song)
    : result.songs || []
  const exact = songs.find(
    (song) => song?.id === request.sourceId && song.localMediaFileId,
  )
  if (exact) return exact.localMediaFileId

  const local = songs.filter(
    (song) =>
      song?.localMediaFileId &&
      song.source === 'library' &&
      song.title?.toLowerCase() === request.title?.toLowerCase() &&
      song.artistName?.toLowerCase() === request.artist?.toLowerCase(),
  )
  return local.length === 1 ? local[0].localMediaFileId : null
}

// Resolve a search hit to a full local song record before it enters the player.
// The current-request check prevents navigation or a newer playback choice from
// letting a stale download replace the user's queue.
export const resolveSearchSongForPlayback = async (
  request,
  { dataProvider, isCurrent, onStatus, delay = wait, now = Date.now },
) => {
  let mediaFileId = request.localMediaFileId
  let completedAt = null
  if (!mediaFileId) {
    let job
    try {
      job = await musicProvider.createDownload('song', request.sourceId, {
        playNow: true,
      })
    } catch (error) {
      if (error?.status !== 409 || !isCurrent()) throw error
      mediaFileId = await findLocalAfterConflict(request)
      if (!mediaFileId)
        throw new Error('Song is in the library but could not be located')
    }
    while (job && isCurrent() && !mediaFileId) {
      onStatus(job.status)
      if (job.status === 'failed')
        throw new Error(job.error || 'Download failed')
      if (job.status === 'succeeded') {
        completedAt ??= now()
        mediaFileId = job.mediaFileId
        if (mediaFileId) break
        if (now() - completedAt >= RESOLUTION_TIMEOUT_MS)
          throw new Error('Downloaded song did not appear in the library')
      }
      await delay(2000)
      if (!isCurrent()) return null
      job = await musicProvider.getDownload(job.id)
    }
  }
  if (!isCurrent() || !mediaFileId) return null

  // A successful import may become visible to the REST song resource a little
  // after the download job resolves its media file ID.
  for (;;) {
    if (!isCurrent()) return null
    try {
      const { data } = await dataProvider.getOne('song', { id: mediaFileId })
      return isCurrent() ? data : null
    } catch (error) {
      if (completedAt == null || now() - completedAt >= RESOLUTION_TIMEOUT_MS)
        throw error
      await delay(2000)
    }
  }
}
