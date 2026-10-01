import { useCallback, useEffect, useRef, useState } from 'react'
import { useHistory, useLocation } from 'react-router-dom'
import {
  Box,
  ButtonBase,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  IconButton,
  InputAdornment,
  Menu,
  MenuItem,
  TextField,
  Typography,
} from '@material-ui/core'
import MoreVertIcon from '@material-ui/icons/MoreVert'
import SearchIcon from '@material-ui/icons/Search'
import { useDispatch, useSelector } from 'react-redux'
import { makeStyles } from '@material-ui/core/styles'
import * as musicProvider from './provider'
import ExternalArtwork from './ExternalArtwork'
import { useDownloadJobs } from './useDownloadJobs'
import { requestSearchPlay } from '../actions'
import { sidebarColors } from '../layout/sidebarStyles'

const RECENT_SONGS_KEY = 'navidrome.externalMusic.recentSongs.v1'
const MAX_RECENT_SONGS = 8
const SEARCH_DEBOUNCE_MS = 500
const SONG_HISTORY_FIELDS = [
  'id',
  'source',
  'localMediaFileId',
  'title',
  'artistName',
  'albumTitle',
  'imageUrl',
  'artworkUrls',
]

const useStyles = makeStyles({
  root: {
    background: sidebarColors.background,
    backgroundColor: sidebarColors.background,
    boxSizing: 'border-box',
    color: `${sidebarColors.text} !important`,
    minHeight: '100%',
    margin: '0 auto',
    maxWidth: 900,
    minWidth: 0,
    padding: '24px',
    width: '100%',
    '@media (max-width:599.95px)': {
      padding: '16px',
    },
  },
  heading: {
    color: `${sidebarColors.text} !important`,
    fontSize: '56px',
    fontWeight: 800,
    letterSpacing: '-0.04em',
    lineHeight: 1.04,
    marginBottom: 28,
    '@media (max-width:599.95px)': {
      fontSize: '40px',
      marginBottom: 20,
    },
  },
  headingAccent: {
    color: `${sidebarColors.accent} !important`,
  },
  searchForm: {
    marginBottom: 28,
  },
  search: {
    '& .MuiOutlinedInput-root': {
      background: `${sidebarColors.divider} !important`,
      backgroundColor: `${sidebarColors.divider} !important`,
      borderRadius: 16,
      color: `${sidebarColors.text} !important`,
      minHeight: 64,
      paddingLeft: 20,
      '& fieldset': {
        borderColor: `${sidebarColors.accent} !important`,
        borderWidth: 1,
      },
      '&:hover fieldset': {
        borderColor: `${sidebarColors.activeText} !important`,
      },
      '&.Mui-focused fieldset': {
        borderColor: `${sidebarColors.accent} !important`,
        borderWidth: 2,
      },
      '@media (max-width:599.95px)': {
        minHeight: 56,
      },
    },
    '& .MuiInputBase-input': {
      color: `${sidebarColors.text} !important`,
      fontSize: 16,
      '&::placeholder': {
        color: `${sidebarColors.secondary} !important`,
        opacity: 1,
      },
    },
  },
  submit: {
    color: `${sidebarColors.accent} !important`,
    padding: 8,
    '&:focus-visible': {
      outline: `2px solid ${sidebarColors.accent}`,
      outlineOffset: 2,
    },
  },
  sectionHeading: {
    color: `${sidebarColors.text} !important`,
    fontSize: 18,
    fontWeight: 700,
    marginBottom: 12,
  },
  results: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  card: {
    background: '#191919',
    backgroundColor: '#191919',
    border: `1px solid ${sidebarColors.divider}`,
    borderRadius: 14,
    color: `${sidebarColors.text} !important`,
    overflow: 'hidden',
    width: '100%',
  },
  row: {
    alignItems: 'center',
    display: 'flex',
    gap: 12,
    minWidth: 0,
    width: '100%',
  },
  playbackButton: {
    alignItems: 'stretch',
    color: `${sidebarColors.text} !important`,
    display: 'flex',
    flex: 1,
    minWidth: 0,
    textAlign: 'left',
    '&:hover': {
      background: `${sidebarColors.selection} !important`,
    },
    '&:focus-visible': {
      outline: `2px solid ${sidebarColors.accent}`,
      outlineOffset: -2,
    },
    '&.Mui-disabled': {
      color: `${sidebarColors.text} !important`,
      opacity: 0.75,
    },
  },
  content: {
    alignItems: 'center',
    display: 'flex',
    gap: 12,
    minWidth: 0,
    padding: '12px 0 12px 12px',
    width: '100%',
    '&:last-child': {
      paddingBottom: 12,
    },
  },
  artwork: {
    alignItems: 'center',
    aspectRatio: '1 / 1',
    background: sidebarColors.divider,
    borderRadius: 8,
    color: `${sidebarColors.activeText} !important`,
    display: 'flex',
    flex: '0 0 64px',
    fontSize: 22,
    height: 64,
    justifyContent: 'center',
    objectFit: 'cover',
    overflow: 'hidden',
    width: 64,
    '@media (max-width:599.95px)': {
      flexBasis: 56,
      height: 56,
      width: 56,
    },
  },
  text: {
    minWidth: 0,
  },
  title: {
    color: `${sidebarColors.text} !important`,
    fontSize: 16,
    fontWeight: 700,
  },
  subtitle: {
    color: `${sidebarColors.secondary} !important`,
    fontSize: 14,
  },
  kind: {
    color: `${sidebarColors.secondary} !important`,
    fontSize: 12,
    textTransform: 'capitalize',
  },
  inlineStatus: {
    color: `${sidebarColors.activeText} !important`,
    display: 'block',
    fontSize: 12,
    marginTop: 2,
  },
  menuButton: {
    alignSelf: 'center',
    color: `${sidebarColors.secondary} !important`,
    marginRight: 8,
    padding: 8,
    '&:hover': {
      background: sidebarColors.selection,
      color: `${sidebarColors.text} !important`,
    },
    '&:focus-visible': {
      outline: `2px solid ${sidebarColors.accent}`,
      outlineOffset: -2,
    },
  },
  menuPaper: {
    background: `${sidebarColors.background} !important`,
    border: `1px solid ${sidebarColors.divider}`,
    color: `${sidebarColors.text} !important`,
  },
  menuItem: {
    color: `${sidebarColors.text} !important`,
    '&:hover, &:focus': {
      background: `${sidebarColors.selection} !important`,
    },
  },
  history: {
    marginBottom: 28,
  },
  refreshing: {
    alignItems: 'center',
    color: `${sidebarColors.secondary} !important`,
    display: 'flex',
    gap: 8,
    marginBottom: 12,
  },
  spinner: {
    color: `${sidebarColors.accent} !important`,
  },
  notice: {
    color: `${sidebarColors.secondary} !important`,
  },
  error: {
    color: `${sidebarColors.activeText} !important`,
  },
  genre: {
    background: sidebarColors.selection,
    color: `${sidebarColors.activeText} !important`,
    '&:hover': {
      background: `${sidebarColors.divider} !important`,
    },
    '&:focus-visible': {
      outline: `2px solid ${sidebarColors.accent}`,
    },
  },
})

const queryFromSearch = (search) => new URLSearchParams(search).get('q') || ''

const compatibilityResults = (response) =>
  Array.isArray(response?.results)
    ? response.results
    : [
        ...(response?.artists || []).map((artist) => ({
          kind: 'artist',
          artist,
        })),
        ...(response?.albums || []).map((album) => ({ kind: 'album', album })),
        ...(response?.songs || []).map((song) => ({ kind: 'song', song })),
        ...(response?.genres || []).map((genre) => ({ kind: 'genre', genre })),
      ]

const getUserId = () => {
  try {
    return window.localStorage?.getItem('userId') || ''
  } catch {
    return ''
  }
}

const historyKey = (userId) => (userId ? `${RECENT_SONGS_KEY}:${userId}` : null)

const readStorage = (key) => {
  if (!key) return null
  try {
    return window.localStorage?.getItem(key)
  } catch {
    return null
  }
}

const writeStorage = (key, value) => {
  if (!key) return
  try {
    window.localStorage?.setItem(key, JSON.stringify(value))
  } catch {
    // Private browsing and full storage quotas must not prevent playback.
  }
}

const asStringOrEmpty = (value) =>
  typeof value === 'string' ? value : value == null ? '' : String(value)

const snapshotSong = (song) => {
  const snapshot = {}
  SONG_HISTORY_FIELDS.forEach((field) => {
    if (field === 'artworkUrls') {
      snapshot[field] = Array.isArray(song?.[field])
        ? song[field].filter((value) => typeof value === 'string')
        : []
      return
    }
    if (field === 'source') {
      snapshot[field] =
        typeof song?.source === 'string' && song.source.trim()
          ? song.source
          : 'catalog'
      return
    }
    snapshot[field] = typeof song?.[field] === 'string' ? song[field] : ''
  })
  return snapshot
}

const validHistoryEntry = (entry) => {
  if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return null
  const song = entry.song && typeof entry.song === 'object' ? entry.song : entry
  if (
    !song ||
    typeof song.id !== 'string' ||
    !song.id.trim() ||
    typeof song.title !== 'string' ||
    !song.title.trim()
  )
    return null
  if (
    Object.prototype.hasOwnProperty.call(entry, 'query') &&
    typeof entry.query !== 'string'
  )
    return null
  for (const field of [
    'source',
    'localMediaFileId',
    'artistName',
    'albumTitle',
    'imageUrl',
  ]) {
    if (
      Object.prototype.hasOwnProperty.call(song, field) &&
      typeof song[field] !== 'string'
    )
      return null
  }
  if (
    Object.prototype.hasOwnProperty.call(song, 'artworkUrls') &&
    !Array.isArray(song.artworkUrls)
  )
    return null
  const snapshot = snapshotSong(song)
  return {
    query: asStringOrEmpty(entry.query),
    song: snapshot,
  }
}

const readRecentSongs = (userId) => {
  const raw = readStorage(historyKey(userId))
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    const seen = new Set()
    return parsed
      .map(validHistoryEntry)
      .filter(Boolean)
      .filter((entry) => {
        const identity = `${entry.song.source || 'catalog'}:${entry.song.id}`
        if (seen.has(identity)) return false
        seen.add(identity)
        return true
      })
      .slice(0, MAX_RECENT_SONGS)
  } catch {
    return []
  }
}

const songIdentity = (song) => `${song?.source || 'catalog'}:${song?.id || ''}`
const songSource = (song) => song?.source || 'catalog'

const MusicSearch = () => {
  const classes = useStyles()
  const history = useHistory()
  const location = useLocation()
  const dispatch = useDispatch()
  const pendingPlay = useSelector((state) => state.player?.pendingSearchPlay)
  const observedUserId = getUserId()
  const [userId, setUserId] = useState(observedUserId)
  const accountReady = observedUserId === userId
  const initialQuery = queryFromSearch(location.search)
  const [query, setQuery] = useState(initialQuery)
  const [recent, setRecent] = useState(() => readRecentSongs(observedUserId))
  const [response, setResponse] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [menu, setMenu] = useState(null)
  const generation = useRef(0)
  const controller = useRef(null)
  const debounceTimer = useRef(null)
  const initialQueryRef = useRef(initialQuery)
  const lastLocationSearch = useRef(location.search)
  const responseQuery = useRef(
    initialQuery.trim().length >= 2 ? initialQuery.trim() : '',
  )
  const recentRef = useRef(recent)
  const { jobs } = useDownloadJobs()

  const replaceQueryInUrl = useCallback(
    (value) => {
      const params = new URLSearchParams(history.location.search)
      if (value) params.set('q', value)
      else params.delete('q')
      const encoded = params.toString()
      const search = encoded ? `?${encoded}` : ''
      if (history.location.search === search) return
      lastLocationSearch.current = search
      history.replace({ ...history.location, search })
    },
    [history],
  )

  const clearSearch = useCallback(() => {
    generation.current += 1
    controller.current?.abort()
    controller.current = null
    clearTimeout(debounceTimer.current)
    debounceTimer.current = null
    responseQuery.current = ''
    setResponse(null)
    setError('')
    setLoading(false)
    replaceQueryInUrl('')
  }, [replaceQueryInUrl])

  const runSearch = useCallback(
    (value) => {
      const trimmed = value.trim()
      if (trimmed.length < 2) {
        clearSearch()
        return
      }
      const requestGeneration = ++generation.current
      controller.current?.abort()
      controller.current = new AbortController()
      setQuery(trimmed)
      setLoading(true)
      setError('')
      replaceQueryInUrl(trimmed)
      musicProvider
        .search(trimmed, { signal: controller.current.signal, limit: 30 })
        .then((value) => {
          if (requestGeneration !== generation.current) return
          setResponse(value)
          responseQuery.current = trimmed
        })
        .catch((requestError) => {
          if (
            requestGeneration === generation.current &&
            requestError?.name !== 'AbortError'
          ) {
            setError('Search is unavailable right now.')
          }
        })
        .finally(() => {
          if (requestGeneration === generation.current) setLoading(false)
        })
    },
    [clearSearch, replaceQueryInUrl],
  )

  const scheduleSearch = useCallback(
    (value) => {
      clearTimeout(debounceTimer.current)
      const trimmed = value.trim()
      if (trimmed.length < 2) {
        clearSearch()
        return
      }
      debounceTimer.current = setTimeout(
        () => runSearch(value),
        SEARCH_DEBOUNCE_MS,
      )
    },
    [clearSearch, runSearch],
  )

  useEffect(() => {
    if (initialQueryRef.current.trim().length < 2) {
      if (initialQueryRef.current) clearSearch()
      return undefined
    }
    debounceTimer.current = setTimeout(
      () => runSearch(initialQueryRef.current),
      SEARCH_DEBOUNCE_MS,
    )
    return () => clearTimeout(debounceTimer.current)
  }, [clearSearch, runSearch])

  useEffect(() => {
    if (observedUserId === userId) return
    setUserId(observedUserId)
    const nextRecent = readRecentSongs(observedUserId)
    recentRef.current = nextRecent
    setRecent(nextRecent)
    setMenu(null)
    generation.current += 1
    controller.current?.abort()
    controller.current = null
    clearTimeout(debounceTimer.current)
    responseQuery.current = ''
    setResponse(null)
    setError('')
    setLoading(false)
    if (query.trim().length >= 2) scheduleSearch(query)
  }, [observedUserId, query, scheduleSearch, userId])

  useEffect(() => {
    if (location.search === lastLocationSearch.current) return
    lastLocationSearch.current = location.search
    const routeQuery = queryFromSearch(location.search)
    setQuery(routeQuery)
    if (routeQuery.trim().length < 2) {
      clearSearch()
      return
    }
    // A browser back/forward or an external route update must invalidate the
    // request that rendered the previous route before its late response can
    // replace the newly selected query's results.
    generation.current += 1
    controller.current?.abort()
    controller.current = null
    clearTimeout(debounceTimer.current)
    setResponse(null)
    responseQuery.current = ''
    setError('')
    setLoading(false)
    scheduleSearch(routeQuery)
  }, [clearSearch, location.search, scheduleSearch])

  useEffect(() => {
    return () => {
      generation.current += 1
      controller.current?.abort()
      clearTimeout(debounceTimer.current)
    }
  }, [])

  const recordRecentSong = useCallback(
    (song, originatingQuery) => {
      const entry = {
        query: asStringOrEmpty(originatingQuery).trim(),
        song: snapshotSong(song),
      }
      const identity = songIdentity(entry.song)
      const next = [
        entry,
        ...recentRef.current.filter(
          (current) => songIdentity(current.song) !== identity,
        ),
      ].slice(0, MAX_RECENT_SONGS)
      recentRef.current = next
      setRecent(next)
      writeStorage(historyKey(userId), next)
    },
    [userId],
  )

  const removeRecentSong = useCallback(
    (entry) => {
      const identity = songIdentity(entry.song)
      const next = recentRef.current.filter(
        (current) => songIdentity(current.song) !== identity,
      )
      recentRef.current = next
      setRecent(next)
      writeStorage(historyKey(userId), next)
    },
    [userId],
  )

  const playSong = useCallback(
    (song, canPlay, originatingQuery) => {
      if (!canPlay) return
      dispatch(requestSearchPlay(song))
      recordRecentSong(song, originatingQuery)
    },
    [dispatch, recordRecentSong],
  )

  const handleInputChange = (value) => {
    setQuery(value)
    if (value.trim().length < 2) clearSearch()
    else scheduleSearch(value)
  }

  const runImmediate = (value) => {
    clearTimeout(debounceTimer.current)
    debounceTimer.current = null
    runSearch(value)
  }

  const openMenu = (event, entry) => {
    event.stopPropagation()
    setMenu({ anchorEl: event.currentTarget, entry })
  }

  const closeMenu = () => setMenu(null)

  const activateHistorySong = (entry) => {
    playSong(entry.song, true, entry.query)
  }

  const renderSongStatus = (entity, activeJob, playingThis) => {
    if (playingThis) {
      const status = pendingPlay?.status
      return status === 'queued'
        ? 'Queued to play'
        : status === 'running'
          ? 'Downloading'
          : 'Starting…'
    }
    if (activeJob?.status === 'queued') return 'Queued'
    if (activeJob?.status === 'running') return 'Downloading'
    if (entity.localMediaFileId) return 'Downloaded'
    return ''
  }

  const renderRow = (entity, kind, index, options = {}) => {
    if (!entity) return null
    const { historyEntry } = options
    const availableJobs = jobs || []
    const title = kind === 'artist' ? entity.name : entity.title
    const subtitle =
      kind === 'song'
        ? [entity.artistName, entity.albumTitle].filter(Boolean).join(' • ')
        : kind === 'album'
          ? [entity.artistName, entity.year].filter(Boolean).join(' • ')
          : entity.disambiguation
    const destination =
      kind === 'artist'
        ? `/search/artist/${entity.id}`
        : kind === 'album'
          ? `/search/album/${entity.id}`
          : null
    const activeJob =
      kind === 'song' && songSource(entity) !== 'library'
        ? availableJobs.find(
            (job) =>
              job.kind === 'song' &&
              job.sourceId === entity.id &&
              (job.status === 'queued' || job.status === 'running'),
          )
        : null
    const playingThis =
      kind === 'song' &&
      pendingPlay?.source === songSource(entity) &&
      pendingPlay?.sourceId === entity.id
    const libraryUnavailable =
      kind === 'song' &&
      response?.degradedSources?.includes('library') &&
      !entity.localMediaFileId
    const canPlay = kind === 'song' && !libraryUnavailable
    const status =
      kind === 'song'
        ? renderSongStatus(entity, activeJob, playingThis) ||
          (libraryUnavailable ? 'Library status unavailable' : '')
        : ''
    const key = `${kind}-${entity.id || entity.name}-${index}`
    const artwork = (
      <ExternalArtwork
        artworkUrls={entity.artworkUrls}
        imageUrl={entity.imageUrl}
        alt={title}
        className={classes.artwork}
      />
    )
    const body = (
      <CardContent className={classes.content}>
        {artwork}
        <Box className={classes.text} flex={1}>
          <Typography className={classes.title} noWrap variant="h6">
            {title}
          </Typography>
          {subtitle && (
            <Typography className={classes.subtitle} noWrap variant="body2">
              {subtitle}
            </Typography>
          )}
          {status && status !== 'Downloaded' && (
            <Typography className={classes.inlineStatus} noWrap>
              {status}
            </Typography>
          )}
          {kind !== 'song' && (
            <Typography className={classes.kind} variant="caption">
              {kind}
            </Typography>
          )}
        </Box>
      </CardContent>
    )
    const action = historyEntry
      ? () => activateHistorySong(historyEntry)
      : kind === 'song'
        ? () => playSong(entity, canPlay, responseQuery.current)
        : () => history.push(destination)
    const ariaLabel =
      kind === 'song'
        ? `Play ${title}`
        : kind === 'artist'
          ? `Open artist ${title}`
          : `Open album ${title}`

    return (
      <Card className={classes.card} key={key}>
        <Box className={classes.row}>
          <ButtonBase
            aria-disabled={kind === 'song' && !canPlay ? 'true' : undefined}
            aria-label={ariaLabel}
            className={classes.playbackButton}
            disabled={kind === 'song' && (!canPlay || playingThis)}
            onClick={action}
            type="button"
          >
            {body}
          </ButtonBase>
          {historyEntry && (
            <IconButton
              aria-label={`More options for ${title}`}
              aria-haspopup="menu"
              className={classes.menuButton}
              onClick={(event) => openMenu(event, historyEntry)}
            >
              <MoreVertIcon />
            </IconButton>
          )}
        </Box>
      </Card>
    )
  }

  const renderHit = (hit, index) => {
    const entity = hit?.[hit.kind]
    if (!entity) return null
    if (
      ['artist', 'album', 'song'].includes(hit.kind) &&
      (typeof entity.id !== 'string' || !entity.id.trim())
    )
      return null
    if (hit.kind === 'genre')
      return (
        <Chip
          className={classes.genre}
          key={`${hit.kind}-${entity.name}-${index}`}
          label={entity.name}
          onClick={() => runImmediate(entity.name)}
        />
      )
    return renderRow(entity, hit.kind, index)
  }

  const hits = compatibilityResults(response)
  const showHistory = accountReady && query.trim().length < 2

  return (
    <Box className={classes.root}>
      <Typography className={classes.heading} component="h1">
        Search <span className={classes.headingAccent}>music</span>
      </Typography>
      <form
        className={classes.searchForm}
        onSubmit={(event) => {
          event.preventDefault()
          runImmediate(query)
        }}
      >
        <TextField
          className={classes.search}
          fullWidth
          inputProps={{ 'aria-label': 'Search music' }}
          placeholder="Search artists, albums, songs…"
          value={query}
          variant="outlined"
          onChange={(event) => handleInputChange(event.target.value)}
          InputProps={{
            startAdornment: (
              <InputAdornment position="start">
                <ButtonBase
                  aria-label="Search"
                  className={classes.submit}
                  type="submit"
                >
                  <SearchIcon />
                </ButtonBase>
              </InputAdornment>
            ),
          }}
        />
      </form>

      {showHistory && (
        <Box className={classes.history}>
          <Typography className={classes.sectionHeading} component="h2">
            Recent searches
          </Typography>
          {recent.length > 0 ? (
            <Box className={classes.results}>
              {recent.map((entry, index) =>
                renderRow(entry.song, 'song', `history-${index}`, {
                  historyEntry: entry,
                }),
              )}
            </Box>
          ) : (
            <Typography className={classes.notice}>
              Songs you play from search will appear here.
            </Typography>
          )}
        </Box>
      )}

      {accountReady && loading && (
        <Box className={classes.refreshing}>
          <CircularProgress className={classes.spinner} size={20} />
          <Typography className={classes.notice}>
            Refreshing results…
          </Typography>
        </Box>
      )}
      {accountReady && response?.partial && (
        <Typography className={classes.notice} paragraph>
          Some sources are temporarily unavailable. Showing partial results.
        </Typography>
      )}
      {accountReady && error && (
        <Typography className={classes.error}>{error}</Typography>
      )}
      {accountReady && !showHistory && response && (
        <Box className={classes.results}>
          {hits.map(renderHit)}
          {hits.length === 0 && (
            <Typography className={classes.notice}>
              No results found.
            </Typography>
          )}
        </Box>
      )}

      <Menu
        anchorEl={menu?.anchorEl}
        keepMounted
        open={accountReady && Boolean(menu)}
        PaperProps={{ className: classes.menuPaper }}
        onClose={closeMenu}
      >
        <MenuItem
          className={classes.menuItem}
          onClick={(event) => {
            event.stopPropagation()
            if (menu?.entry) removeRecentSong(menu.entry)
            closeMenu()
          }}
        >
          Remove from history
        </MenuItem>
      </Menu>
    </Box>
  )
}

export default MusicSearch
