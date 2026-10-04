import React, { useCallback, useEffect, useRef, useState } from 'react'
import {
  Button,
  CircularProgress,
  makeStyles,
  Typography,
} from '@material-ui/core'
import { useDataProvider, useLocale, useTranslate } from 'react-admin'
import { Link } from 'react-router-dom'
import MusicNoteOutlinedIcon from '@material-ui/icons/MusicNoteOutlined'
import { Artwork } from '../common/Artwork'
import { sortPlaylists } from '../common/playlistOrder'
import { sidebarColors } from '../layout/sidebarStyles'
import {
  getQuickPickCacheEntry,
  getQuickPickCacheGeneration,
  loadQuickPickSection,
} from './cache'

const useStyles = makeStyles((theme) => ({
  root: { marginTop: theme.spacing(2.5) },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: theme.spacing(1.5),
    margin: 0,
    padding: 0,
    listStyle: 'none',
  },
  row: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(3),
    padding: theme.spacing(1.5),
    borderRadius: 16,
    backgroundColor: 'rgba(255,255,255,.04)',
    color: sidebarColors.text,
    textDecoration: 'none',
    '&:hover': { backgroundColor: sidebarColors.selection },
    '&:focus-visible': {
      outline: `2px solid ${sidebarColors.accent}`,
      outlineOffset: -2,
    },
    [theme.breakpoints.down('xs')]: {
      gap: theme.spacing(2),
      padding: theme.spacing(1),
      borderRadius: 12,
    },
  },
  cover: {
    position: 'relative',
    display: 'grid',
    placeItems: 'center',
    width: 'clamp(52px, 14cqw, 96px)',
    aspectRatio: '1 / 1',
    flexShrink: 0,
    overflow: 'hidden',
    borderRadius: 8,
    backgroundColor: sidebarColors.divider,
    color: sidebarColors.secondary,
  },
  placeholder: { fontSize: 30 },
  artwork: { position: 'absolute', inset: 0, width: '100%', height: '100%' },
  details: { minWidth: 0, flex: 1 },
  name: {
    '&&': {
      color: `${sidebarColors.text} !important`,
      fontFamily: 'inherit',
      fontSize: 'clamp(16px, 4.2cqw, 26px)',
      fontWeight: 700,
      lineHeight: 1.25,
    },
  },
  subtitle: {
    '&&': {
      color: `${sidebarColors.secondary} !important`,
      fontFamily: 'inherit',
      fontSize: 'clamp(14px, 3.6cqw, 22px)',
      lineHeight: 1.25,
      marginTop: 4,
    },
  },
  status: {
    display: 'flex',
    minHeight: 72,
    gap: theme.spacing(1.5),
    alignItems: 'center',
    justifyContent: 'center',
    flexWrap: 'wrap',
  },
  statusText: { '&&': { color: `${sidebarColors.secondary} !important` } },
  accent: { '&&': { color: `${sidebarColors.accent} !important` } },
}))

const userId = () => localStorage.getItem('userId') || ''

const initialPlaylistState = (identity) => {
  const entry = getQuickPickCacheEntry(identity, 'playlists')
  if (entry?.status === 'success') {
    return { identity, playlists: entry.value, status: 'success' }
  }
  if (entry?.status === 'error') {
    return { identity, playlists: [], status: 'error' }
  }
  return { identity, playlists: [], status: 'loading' }
}

const QuickPickPlaylists = () => {
  const classes = useStyles()
  const locale = useLocale()
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const identity = userId()

  const orderOwnedPlaylists = useCallback(
    (records) =>
      sortPlaylists(
        records.filter((playlist) => playlist.ownerId === identity),
        locale,
        identity,
      ),
    [identity, locale],
  )

  const [playlistState, setPlaylistState] = useState(() =>
    initialPlaylistState(identity),
  )
  const [retryVersion, setRetryVersion] = useState(0)
  const consumedRetry = useRef(0)

  useEffect(() => {
    if (!identity) return undefined

    let alive = true
    const retry = retryVersion !== consumedRetry.current
    if (retry) consumedRetry.current = retryVersion
    const requestGeneration = getQuickPickCacheGeneration()
    const cached = getQuickPickCacheEntry(identity, 'playlists')

    if (!retry && cached?.status === 'success') {
      setPlaylistState({
        identity,
        playlists: cached.value,
        status: 'success',
      })
      return () => {
        alive = false
      }
    }
    if (!retry && cached?.status === 'error') {
      setPlaylistState({ identity, playlists: [], status: 'error' })
      return () => {
        alive = false
      }
    }

    setPlaylistState({ identity, playlists: [], status: 'loading' })
    const load = () =>
      dataProvider
        .getList('playlist', {
          pagination: { page: 1, perPage: 0 },
          sort: { field: 'name', order: 'ASC' },
          filter: { owner_id: identity },
        })
        .then((result) => {
          const records = Array.isArray(result?.data)
            ? result.data
            : Object.values(result?.data || {})
          return orderOwnedPlaylists(records)
        })

    loadQuickPickSection(identity, 'playlists', load, { retry })
      .then((playlists) => {
        if (
          alive &&
          getQuickPickCacheGeneration() === requestGeneration &&
          userId() === identity
        ) {
          setPlaylistState({ identity, playlists, status: 'success' })
        }
      })
      .catch(() => {
        if (
          alive &&
          getQuickPickCacheGeneration() === requestGeneration &&
          userId() === identity
        ) {
          setPlaylistState({ identity, playlists: [], status: 'error' })
        }
      })

    return () => {
      alive = false
    }
  }, [dataProvider, identity, locale, orderOwnedPlaylists, retryVersion])

  if (!identity) return null

  const currentState =
    playlistState.identity === identity
      ? playlistState
      : { identity, playlists: [], status: 'loading' }
  const playlists = currentState.playlists

  return (
    <section className={classes.root} aria-label="My playlists">
      {currentState.status === 'error' ? (
        <div className={classes.status} role="alert">
          <Typography className={classes.statusText}>
            Unable to load playlists.
          </Typography>
          <Button
            className={classes.accent}
            onClick={() => setRetryVersion((version) => version + 1)}
            aria-label="Retry playlists"
          >
            Retry
          </Button>
        </div>
      ) : currentState.status !== 'success' ? (
        <div className={classes.status}>
          <CircularProgress
            size={28}
            className={classes.accent}
            aria-label="Loading playlists"
          />
        </div>
      ) : playlists.length === 0 ? (
        <div className={classes.status}>
          <Typography className={classes.statusText}>
            No playlists yet.
          </Typography>
        </div>
      ) : (
        <ul className={classes.list}>
          {playlists.map((playlist) => (
            <li key={playlist.id}>
              <Link
                className={classes.row}
                to={`/playlist/${playlist.id}/show`}
                aria-label={playlist.name}
              >
                <div className={classes.cover}>
                  <MusicNoteOutlinedIcon
                    className={classes.placeholder}
                    aria-hidden="true"
                  />
                  <Artwork
                    record={playlist}
                    size={192}
                    className={classes.artwork}
                    title=""
                  />
                </div>
                <div className={classes.details}>
                  <Typography
                    component="span"
                    display="block"
                    noWrap
                    className={classes.name}
                  >
                    {playlist.name}
                  </Typography>
                  <Typography
                    component="span"
                    display="block"
                    noWrap
                    className={classes.subtitle}
                  >
                    {translate('resources.playlist.name', {
                      smart_count: 1,
                      _: 'Playlist',
                    })}
                  </Typography>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export default QuickPickPlaylists
