import { isSmartPlaylist } from './playlistUtils'

export const LIKED_MUSIC_PLAYLIST_NAME = 'liked music'
export const PLAYLIST_DEFAULT_SORT_FIELD = 'liked_songs_first'
export const PLAYLIST_DEFAULT_SORT = {
  field: PLAYLIST_DEFAULT_SORT_FIELD,
  order: 'ASC',
}

const normalizedName = (name) =>
  String(name || '')
    .trim()
    .toLowerCase()

export const isLikedMusicPlaylist = (playlist, ownerId) => {
  if (!playlist || !ownerId || isSmartPlaylist(playlist)) return false
  return (
    String(playlist.ownerId || '') === String(ownerId || '') &&
    normalizedName(playlist.name) === LIKED_MUSIC_PLAYLIST_NAME
  )
}

// Returns a new array so callers can safely retain the data-provider/cache
// snapshot. The owner check keeps a shared or foreign playlist with the
// reserved name from taking the current user's pinned position.
export const sortPlaylists = (playlists, locale, ownerId) => {
  const collator = new Intl.Collator(locale, { sensitivity: 'base' })
  return [...playlists].sort((a, b) => {
    const aIsLiked = isLikedMusicPlaylist(a, ownerId)
    const bIsLiked = isLikedMusicPlaylist(b, ownerId)
    if (aIsLiked !== bIsLiked) return aIsLiked ? -1 : 1

    return (
      collator.compare(String(a?.name || ''), String(b?.name || '')) ||
      String(a?.id || '').localeCompare(String(b?.id || ''))
    )
  })
}
