import { useCallback, useEffect, useRef, useState } from 'react'
import { useHistory } from 'react-router-dom'
import {
  Box,
  ButtonBase,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  InputAdornment,
  TextField,
  Typography,
} from '@material-ui/core'
import SearchIcon from '@material-ui/icons/Search'
import { makeStyles } from '@material-ui/core/styles'
import * as musicProvider from './provider'
import { DownloadButton, DownloadStatus } from './DownloadStatus'
import ExternalArtwork from './ExternalArtwork'
import { useDownloadJobs } from './useDownloadJobs'

const RECENT_SEARCHES_KEY = 'navidrome.externalMusic.recentSearches'

const useStyles = makeStyles((theme) => ({
  root: { margin: '0 auto', maxWidth: 900, padding: theme.spacing(3) },
  search: {
    background: theme.palette.background.paper,
    borderRadius: theme.shape.borderRadius * 2,
    marginBottom: theme.spacing(2),
  },
  results: {
    display: 'flex',
    flexDirection: 'column',
    gap: theme.spacing(1.5),
  },
  card: { width: '100%' },
  row: {
    alignItems: 'center',
    display: 'flex',
    gap: theme.spacing(2),
    width: '100%',
  },
  resultButton: { display: 'block', flex: 1, textAlign: 'left' },
  content: { minWidth: 0 },
  artwork: {
    alignItems: 'center',
    aspectRatio: '1 / 1',
    background: theme.palette.action.hover,
    borderRadius: theme.shape.borderRadius,
    display: 'flex',
    flex: '0 0 64px',
    height: 64,
    justifyContent: 'center',
    objectFit: 'cover',
    width: 64,
  },
  recent: { marginBottom: theme.spacing(3) },
  refreshing: {
    alignItems: 'center',
    display: 'flex',
    gap: theme.spacing(1),
    marginBottom: theme.spacing(1),
  },
}))

const readRecentSearches = () => {
  try {
    const values = JSON.parse(localStorage.getItem(RECENT_SEARCHES_KEY) || '[]')
    return Array.isArray(values) ? values.filter(Boolean).slice(0, 8) : []
  } catch {
    return []
  }
}
const saveRecentSearch = (query) => {
  const next = [
    query,
    ...readRecentSearches().filter((value) => value !== query),
  ].slice(0, 8)
  localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(next))
  return next
}
const initialQuery = () =>
  new URLSearchParams(window.location.search).get('q') || ''
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

const MusicSearch = () => {
  const classes = useStyles(),
    history = useHistory()
  const [query, setQuery] = useState(initialQuery),
    [recent, setRecent] = useState(readRecentSearches)
  const [response, setResponse] = useState(null),
    [loading, setLoading] = useState(false),
    [error, setError] = useState('')
  const generation = useRef(0),
    controller = useRef(null),
    debounceTimer = useRef(null)
  const { jobs, refreshJobs } = useDownloadJobs()

  const runSearch = useCallback(
    (value) => {
      const trimmed = value.trim()
      if (trimmed.length < 2) return
      const requestGeneration = ++generation.current
      controller.current?.abort()
      controller.current = new AbortController()
      setQuery(trimmed)
      setRecent(saveRecentSearch(trimmed))
      setLoading(true)
      setError('')
      const params = new URLSearchParams(window.location.search)
      params.set('q', trimmed)
      if (history.replace)
        history.replace(`${window.location.pathname}?${params.toString()}`)
      musicProvider
        .search(trimmed, { signal: controller.current.signal, limit: 30 })
        .then((value) => {
          if (requestGeneration === generation.current) setResponse(value)
        })
        .catch((requestError) => {
          if (
            requestGeneration === generation.current &&
            requestError?.name !== 'AbortError'
          )
            setError('Search is unavailable right now.')
        })
        .finally(() => {
          if (requestGeneration === generation.current) setLoading(false)
        })
    },
    [history],
  )

  useEffect(() => {
    if (query.trim().length < 2) return undefined
    debounceTimer.current = setTimeout(() => runSearch(query), 500)
    return () => clearTimeout(debounceTimer.current)
  }, [query, runSearch])
  useEffect(() => {
    const restore = () => setQuery(initialQuery())
    window.addEventListener('popstate', restore)
    return () => {
      window.removeEventListener('popstate', restore)
      controller.current?.abort()
    }
  }, [])

  const hits = compatibilityResults(response)
  const runImmediate = (value) => {
    clearTimeout(debounceTimer.current)
    runSearch(value)
  }
  const renderHit = (hit, index) => {
    const entity = hit[hit.kind]
    if (!entity) return null
    if (hit.kind === 'genre')
      return (
        <Chip
          key={`${hit.kind}-${entity.name}-${index}`}
          label={entity.name}
          onClick={() => runImmediate(entity.name)}
        />
      )
    const title = hit.kind === 'artist' ? entity.name : entity.title
    const subtitle =
      hit.kind === 'song'
        ? [entity.artistName, entity.albumTitle].filter(Boolean).join(' • ')
        : hit.kind === 'album'
          ? [entity.artistName, entity.year].filter(Boolean).join(' • ')
          : entity.disambiguation
    const destination =
      hit.kind === 'artist'
        ? `/search/artist/${entity.id}`
        : hit.kind === 'album'
          ? `/search/album/${entity.id}`
          : null
    const body = (
      <CardContent className={classes.row}>
        <ExternalArtwork
          artworkUrls={entity.artworkUrls}
          imageUrl={entity.imageUrl}
          alt={title}
          className={classes.artwork}
        />
        <Box className={classes.content} flex={1}>
          <Typography variant="h6" noWrap>
            {title}
          </Typography>
          {subtitle && (
            <Typography color="textSecondary" variant="body2" noWrap>
              {subtitle}
            </Typography>
          )}
          <Typography color="textSecondary" variant="caption">
            {hit.kind}
          </Typography>
        </Box>
      </CardContent>
    )
    return (
      <Card className={classes.card} key={`${hit.kind}-${entity.id}-${index}`}>
        <Box className={classes.row}>
          {destination ? (
            <ButtonBase
              className={classes.resultButton}
              onClick={() => history.push(destination)}
            >
              {body}
            </ButtonBase>
          ) : (
            body
          )}
          {(hit.kind === 'song' || hit.kind === 'album') && (
            <Box pr={2}>
              <DownloadButton
                kind={hit.kind}
                id={entity.id}
                onCreated={refreshJobs}
              />
            </Box>
          )}
        </Box>
      </Card>
    )
  }

  return (
    <Box className={classes.root}>
      <Typography variant="h4" gutterBottom>
        Search music
      </Typography>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          runImmediate(query)
        }}
      >
        <TextField
          className={classes.search}
          fullWidth
          variant="outlined"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search artists, albums, songs, or genres"
          InputProps={{
            endAdornment: (
              <InputAdornment position="end">
                <ButtonBase type="submit" aria-label="Search">
                  <SearchIcon />
                </ButtonBase>
              </InputAdornment>
            ),
          }}
        />
      </form>
      <DownloadStatus jobs={jobs} />
      {!response && recent.length > 0 && (
        <Box className={classes.recent}>
          <Typography variant="h6" gutterBottom>
            Recent searches
          </Typography>
          {recent.map((value) => (
            <Chip
              key={value}
              label={value}
              onClick={() => runImmediate(value)}
              style={{ margin: 4 }}
            />
          ))}
        </Box>
      )}
      {loading && (
        <Box className={classes.refreshing}>
          <CircularProgress size={20} />
          <Typography color="textSecondary">Refreshing results…</Typography>
        </Box>
      )}
      {response?.partial && (
        <Typography color="textSecondary" paragraph>
          Some sources are temporarily unavailable. Showing partial results.
        </Typography>
      )}
      {error && <Typography color="error">{error}</Typography>}
      {response && (
        <Box className={classes.results}>
          {hits.map(renderHit)}
          {hits.length === 0 && (
            <Typography color="textSecondary">
              No external results found.
            </Typography>
          )}
        </Box>
      )}
    </Box>
  )
}

export default MusicSearch
