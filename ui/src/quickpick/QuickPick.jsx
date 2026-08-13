import React, { useCallback, useEffect, useState } from 'react'
import { CircularProgress, makeStyles, Typography } from '@material-ui/core'
import { useDataProvider, useNotify } from 'react-admin'
import { useDispatch } from 'react-redux'
import { useHistory } from 'react-router-dom'
import { Artwork } from '../common/Artwork'
import { playTracks, setRadioSession } from '../actions'
import {
  createPersonalRadio,
  getQuickPick,
  radioSongs,
  recordPlaylistPlay,
} from './provider'

const useStyles = makeStyles((theme) => ({
  root: {
    width: '100%',
    maxWidth: 1040,
    boxSizing: 'border-box',
    margin: '0 auto',
    padding: theme.spacing(3),
  },
  heading: { marginBottom: theme.spacing(2) },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(3, minmax(0, 1fr))',
    gap: theme.spacing(2),
    [theme.breakpoints.down('xs')]: { gap: theme.spacing(1) },
  },
  tile: {
    position: 'relative',
    aspectRatio: '1 / 1',
    border: 0,
    padding: 0,
    borderRadius: theme.shape.borderRadius * 2,
    overflow: 'hidden',
    background: 'none',
    color: '#fff',
    cursor: 'pointer',
    boxShadow: theme.shadows[4],
    transition: 'transform 120ms ease, box-shadow 120ms ease',
    '&:hover, &:focus-visible': {
      transform: 'translateY(-2px)',
      boxShadow: theme.shadows[8],
      outline: `2px solid ${theme.palette.primary.main}`,
    },
  },
  artwork: { position: 'absolute', inset: 0, width: '100%', height: '100%' },
  fallback: {
    position: 'absolute',
    inset: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 'clamp(2rem, 7vw, 5rem)',
    fontWeight: 800,
    letterSpacing: '-0.06em',
  },
  shade: {
    position: 'absolute',
    inset: 0,
    background: 'linear-gradient(transparent 42%, rgba(0,0,0,.88))',
  },
  label: {
    position: 'absolute',
    left: theme.spacing(1.5),
    right: theme.spacing(1.5),
    bottom: theme.spacing(1.25),
    textAlign: 'left',
    textShadow: '0 1px 3px #000',
  },
  title: { fontWeight: 700, lineHeight: 1.15 },
  subtitle: { opacity: 0.86, marginTop: 3 },
  center: { display: 'grid', minHeight: 300, placeItems: 'center' },
}))

const identityColor = (value) => {
  let hash = 0
  for (let i = 0; i < value.length; i += 1)
    hash = (hash * 31 + value.charCodeAt(i)) | 0
  const hue = Math.abs(hash) % 360
  return `linear-gradient(145deg, hsl(${hue}, 62%, 42%), hsl(${(hue + 48) % 360}, 58%, 22%))`
}

const initials = (value) =>
  value
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0])
    .join('')
    .toUpperCase()

const QuickPick = () => {
  const classes = useStyles()
  const dispatch = useDispatch()
  const history = useHistory()
  const notify = useNotify()
  const dataProvider = useDataProvider()
  const [items, setItems] = useState(null)

  useEffect(() => {
    let alive = true
    getQuickPick()
      .then((response) => alive && setItems(response.items || []))
      .catch(() => alive && notify('Unable to load Quick Pick', 'warning'))
    return () => {
      alive = false
    }
  }, [notify])

  const playPlaylist = useCallback(
    (playlist) => {
      dataProvider
        .getList('playlistTrack', {
          pagination: { page: 1, perPage: -1 },
          sort: { field: 'id', order: 'ASC' },
          filter: { playlist_id: playlist.id },
        })
        .then(({ data }) => {
          const songs = Object.fromEntries(data.map((song) => [song.id, song]))
          dispatch(
            playTracks(
              songs,
              data.map((song) => song.id),
            ),
          )
          recordPlaylistPlay(playlist.id).catch(() => {})
          history.push(`/playlist/${playlist.id}/show`)
        })
        .catch(() => notify('Unable to play playlist', 'warning'))
    },
    [dataProvider, dispatch, history, notify],
  )

  const playSongRadio = useCallback(
    (song) => {
      dispatch(playTracks({ [song.id]: song }, [song.id]))
      createPersonalRadio(song.id)
        .then((response) => {
          const enriched = radioSongs(response)
          const seed = response.items.find((item) => item.type === 'seed')
          dispatch(
            setRadioSession({ id: response.session.id, seedItemId: seed?.id }),
          )
          const rest = enriched.ids.filter(
            (key) => enriched.data[key].radioItemType !== 'seed',
          )
          if (rest.length)
            dispatch({
              type: 'PLAYER_ADD_TRACKS',
              data: Object.fromEntries(
                rest.map((key) => [key, enriched.data[key]]),
              ),
            })
        })
        .catch(() =>
          notify(
            'Radio could not be started; playing the selected song',
            'warning',
          ),
        )
    },
    [dispatch, notify],
  )

  if (items == null)
    return (
      <div className={classes.center}>
        <CircularProgress />
      </div>
    )

  return (
    <main className={classes.root}>
      <Typography component="h1" variant="h4" className={classes.heading}>
        Quick Pick
      </Typography>
      {items.length === 0 ? (
        <Typography>
          Play a few songs or playlists and your favorites will appear here.
        </Typography>
      ) : (
        <div className={classes.grid}>
          {items.map((item) => {
            const record = item.song || item.playlist
            const title = item.song?.title || item.playlist?.name
            const subtitle = item.song?.artist || 'Playlist'
            const identity = `${subtitle}:${title}`
            return (
              <button
                type="button"
                key={`${item.kind}-${record.id}`}
                className={classes.tile}
                onClick={() =>
                  item.kind === 'playlist'
                    ? playPlaylist(item.playlist)
                    : playSongRadio(item.song)
                }
                aria-label={`Play ${title}${item.kind === 'song' ? ' radio' : ''}`}
              >
                <div
                  className={classes.fallback}
                  style={{ background: identityColor(identity) }}
                >
                  {initials(item.song?.artist || title)}
                </div>
                <Artwork
                  record={record}
                  square
                  className={classes.artwork}
                  title=""
                />
                <div className={classes.shade} />
                <div className={classes.label}>
                  <Typography className={classes.title} noWrap>
                    {title}
                  </Typography>
                  <Typography
                    className={classes.subtitle}
                    variant="body2"
                    noWrap
                  >
                    {subtitle}
                  </Typography>
                </div>
              </button>
            )
          })}
        </div>
      )}
    </main>
  )
}

export default QuickPick
