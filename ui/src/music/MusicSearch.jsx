import { useState } from 'react'
import { useHistory } from 'react-router-dom'
import {
  Avatar,
  Box,
  ButtonBase,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Grid,
  InputAdornment,
  TextField,
  Typography,
} from '@material-ui/core'
import SearchIcon from '@material-ui/icons/Search'
import { makeStyles } from '@material-ui/core/styles'
import * as musicProvider from './provider'
import { DownloadButton, DownloadStatus } from './DownloadStatus'
import { useDownloadJobs } from './useDownloadJobs'

const RECENT_SEARCHES_KEY = 'navidrome.externalMusic.recentSearches'

const useStyles = makeStyles((theme) => ({
  root: {
    margin: '0 auto',
    maxWidth: 1100,
    padding: theme.spacing(3),
  },
  search: {
    background: theme.palette.background.paper,
    borderRadius: theme.shape.borderRadius * 2,
    marginBottom: theme.spacing(3),
  },
  resultButton: {
    display: 'block',
    textAlign: 'left',
    width: '100%',
  },
  card: {
    height: '100%',
  },
  image: {
    height: 150,
    width: '100%',
    borderRadius: theme.shape.borderRadius,
    objectFit: 'cover',
  },
  placeholder: {
    alignItems: 'center',
    background: theme.palette.action.hover,
    display: 'flex',
    height: 150,
    justifyContent: 'center',
    width: '100%',
  },
  actions: {
    display: 'flex',
    justifyContent: 'flex-end',
    marginTop: theme.spacing(1),
  },
  recent: {
    marginBottom: theme.spacing(3),
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
  const next = [query, ...readRecentSearches().filter((value) => value !== query)].slice(0, 8)
  localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(next))
  return next
}

const Artwork = ({ src, label, classes }) =>
  src ? (
    <img className={classes.image} src={src} alt={label} />
  ) : (
    <div className={classes.placeholder}>{label?.slice(0, 1) || '♪'}</div>
  )

const ResultSection = ({ title, children }) => {
  if (!children || children.length === 0) return null
  return (
    <Box mb={4}>
      <Typography variant="h5" gutterBottom>
        {title}
      </Typography>
      <Grid container spacing={2}>
        {children}
      </Grid>
    </Box>
  )
}

const MusicSearch = () => {
  const classes = useStyles()
  const history = useHistory()
  const [query, setQuery] = useState('')
  const [recent, setRecent] = useState(readRecentSearches)
  const [results, setResults] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const { jobs, refreshJobs } = useDownloadJobs()

  const runSearch = (value = query) => {
    const trimmed = value.trim()
    if (!trimmed || loading) return
    setQuery(trimmed)
    setRecent(saveRecentSearch(trimmed))
    setLoading(true)
    setError('')
    musicProvider
      .search(trimmed)
      .then(setResults)
      .catch(() => setError('Search is unavailable right now.'))
      .finally(() => setLoading(false))
  }

  const handleSubmit = (event) => {
    event.preventDefault()
    runSearch()
  }

  return (
    <Box className={classes.root}>
      <Typography variant="h4" gutterBottom>
        Search music
      </Typography>
      <form onSubmit={handleSubmit}>
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

      {!results && recent.length > 0 && (
        <Box className={classes.recent}>
          <Typography variant="h6" gutterBottom>
            Recent searches
          </Typography>
          {recent.map((value) => (
            <Chip
              key={value}
              label={value}
              onClick={() => runSearch(value)}
              style={{ margin: 4 }}
            />
          ))}
        </Box>
      )}

      {loading && (
        <Box display="flex" justifyContent="center" p={4}>
          <CircularProgress />
        </Box>
      )}
      {error && <Typography color="error">{error}</Typography>}
      {results && !loading && (
        <>
          <ResultSection title="Artists">
            {results.artists?.map((artist) => (
              <Grid item xs={12} sm={6} md={4} key={artist.id}>
                <ButtonBase
                  className={classes.resultButton}
                  onClick={() => history.push(`/search/artist/${artist.id}`)}
                >
                  <Card className={classes.card}>
                    <CardContent>
                      <Typography variant="h6">{artist.name}</Typography>
                      {artist.disambiguation && (
                        <Typography color="textSecondary" variant="body2">
                          {artist.disambiguation}
                        </Typography>
                      )}
                    </CardContent>
                  </Card>
                </ButtonBase>
              </Grid>
            ))}
          </ResultSection>

          <ResultSection title="Albums">
            {results.albums?.map((album) => (
              <Grid item xs={12} sm={6} md={4} key={album.id}>
                <Card className={classes.card}>
                  <ButtonBase
                    className={classes.resultButton}
                    onClick={() => history.push(`/search/album/${album.id}`)}
                  >
                    <CardContent>
                      <Artwork
                        src={album.imageUrl}
                        label={album.title}
                        classes={classes}
                      />
                      <Typography variant="h6">{album.title}</Typography>
                      <Typography color="textSecondary" variant="body2">
                        {album.artistName} {album.year ? `• ${album.year}` : ''}
                      </Typography>
                    </CardContent>
                  </ButtonBase>
                  <Box className={classes.actions} px={2} pb={2}>
                    <DownloadButton
                      kind="album"
                      id={album.id}
                      onCreated={refreshJobs}
                    />
                  </Box>
                </Card>
              </Grid>
            ))}
          </ResultSection>

          <ResultSection title="Songs">
            {results.songs?.map((song) => (
              <Grid item xs={12} md={6} key={song.id}>
                <Card>
                  <CardContent>
                    <Box display="flex" alignItems="center" gridGap={12}>
                      <Avatar src={song.imageUrl}>{song.title?.slice(0, 1)}</Avatar>
                      <Box flex={1}>
                        <Typography variant="h6">{song.title}</Typography>
                        <Typography color="textSecondary" variant="body2">
                          {song.artistName} {song.albumTitle ? `• ${song.albumTitle}` : ''}
                        </Typography>
                      </Box>
                      <DownloadButton
                        kind="song"
                        id={song.id}
                        onCreated={refreshJobs}
                      />
                    </Box>
                  </CardContent>
                </Card>
              </Grid>
            ))}
          </ResultSection>

          <ResultSection title="Genres">
            {results.genres?.map((genre) => (
              <Grid item key={genre.name}>
                <Chip label={genre.name} onClick={() => runSearch(genre.name)} />
              </Grid>
            ))}
          </ResultSection>

          {!results.artists?.length &&
            !results.albums?.length &&
            !results.songs?.length &&
            !results.genres?.length && (
              <Typography color="textSecondary">No external results found.</Typography>
            )}
        </>
      )}
    </Box>
  )
}

export default MusicSearch
