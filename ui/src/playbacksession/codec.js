const videoFields = [
  'id',
  'title',
  'channelId',
  'channelName',
  'thumbnailUrl',
  'durationSeconds',
]
const pick = (value, fields) =>
  Object.fromEntries(
    fields
      .filter((key) => value?.[key] != null)
      .map((key) => [key, value[key]]),
  )

export const snapshotPlayer = (player, progress = {}) => {
  const queue = player.queue || []
  const selected =
    queue[player.playIndex ?? Math.max(0, player.savedPlayIndex || 0)]
  const current = player.current?.uuid === selected?.uuid ? player.current : {}
  const position =
    progress.entryId === selected?.uuid
      ? progress.positionMs
      : current.currentTime != null
        ? Number(current.currentTime) * 1000
        : selected?.resumePositionMs || 0
  return {
    entries: queue.map((item) => ({
      entryId: item.uuid,
      source:
        item.source === 'hometube'
          ? 'hometube'
          : item.isRadio && !item.radioItemId && !item.radioPending
            ? 'radio'
            : 'music',
      id: item.videoId || item.trackId || '',
      title: item.name || item.song?.title || '',
      artist: item.singer || item.song?.artist || '',
      ...(item.source === 'hometube' && {
        video: pick(item.video, videoFields),
      }),
      ...(item.radioItemId && {
        radioSessionId: item.radioSessionId,
        radioItemId: item.radioItemId,
        radioItemType: item.radioItemType,
        pending: Boolean(item.radioPending),
      }),
      // The local cache carries enough metadata to show a paused queue offline.
      song: pick(item.song, [
        'id',
        'title',
        'artist',
        'album',
        'albumId',
        'duration',
        'lyrics',
        'updatedAt',
      ]),
    })),
    selectedEntryId: selected?.uuid || '',
    positionMs: Math.max(0, Math.floor(Number(position) || 0)),
    state: !selected
      ? 'stopped'
      : progress.entryId === selected.uuid
        ? progress.state || 'paused'
        : current.paused === false
          ? 'playing'
          : 'paused',
    mode: player.mode || 'order',
    radioSessionId: player.radioSession?.id || '',
  }
}

export const networkSnapshot = (snapshot) => ({
  ...snapshot,
  entries: snapshot.entries.map(({ song: _song, ...entry }) => entry),
})

export const structureKey = (snapshot) =>
  JSON.stringify({
    entries: networkSnapshot(snapshot).entries,
    selectedEntryId: snapshot.selectedEntryId,
    mode: snapshot.mode,
    radioSessionId: snapshot.radioSessionId,
  })

export const playbackCacheKey = (userId) => `navidrome.playback.v1:${userId}`
export const readPlaybackCache = (userId) => {
  if (!userId) return null
  try {
    const saved = JSON.parse(
      localStorage.getItem(playbackCacheKey(userId)) || 'null',
    )
    if (
      !saved ||
      !Array.isArray(saved.entries) ||
      saved.entries.length > 5000 ||
      !Number.isFinite(saved.positionMs) ||
      saved.positionMs < 0 ||
      typeof saved.selectedEntryId !== 'string' ||
      saved.entries.some(
        (entry) =>
          !entry ||
          typeof entry.entryId !== 'string' ||
          typeof entry.id !== 'string' ||
          !['music', 'hometube', 'radio'].includes(entry.source) ||
          (entry.source === 'hometube' && !entry.video),
      )
    )
      return null
    return saved
  } catch {
    return null
  }
}
export const writePlaybackCache = (userId, snapshot) => {
  if (!userId) return
  try {
    localStorage.setItem(playbackCacheKey(userId), JSON.stringify(snapshot))
  } catch {
    /* Storage failure must not interrupt listening. */
  }
}
