const DEFAULT_SEARCH_SECTION_ORDER = ['artists', 'albums', 'songs', 'genres']

const normalizeSearchText = (value) =>
  String(value || '')
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
    .replace(/\s+/g, ' ')

const phraseMatches = (query, value) =>
  value === query || value.includes(query) || query.includes(value)

export const isSongTitleSearch = (query, results) => {
  const normalizedQuery = normalizeSearchText(query)
  if (!normalizedQuery) return false

  const hasExactArtistMatch = (results?.artists || []).some(
    (artist) => normalizeSearchText(artist.name) === normalizedQuery,
  )
  if (hasExactArtistMatch) return false

  return (results?.songs || []).some((song) =>
    phraseMatches(normalizedQuery, normalizeSearchText(song.title)),
  )
}

export const getSearchSectionOrder = (query, results) =>
  isSongTitleSearch(query, results)
    ? [
        'songs',
        ...DEFAULT_SEARCH_SECTION_ORDER.filter(
          (section) => section !== 'songs',
        ),
      ]
    : DEFAULT_SEARCH_SECTION_ORDER
