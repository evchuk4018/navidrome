export const isVideo = (item) => item?.source === 'hometube'
export const entryIdentity = (item) =>
  `${item.source || 'music'}:${item.videoId || item.mediaFileId || item.trackId || item.id}`
export const playlistEntry = (item) =>
  isVideo(item)
    ? {
        source: 'hometube',
        id: item.videoId || item.video?.id || item.trackId || item.id,
        video: item.video,
      }
    : { source: 'music', id: item.mediaFileId || item.trackId || item.id }
