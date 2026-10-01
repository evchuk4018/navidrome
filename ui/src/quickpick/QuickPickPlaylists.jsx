import React, { useCallback, useMemo } from 'react'
import {
  Button,
  CircularProgress,
  makeStyles,
  Typography,
} from '@material-ui/core'
import { useLocale, useQueryWithStore, useTranslate } from 'react-admin'
import { useSelector } from 'react-redux'
import { Link } from 'react-router-dom'
import MusicNoteOutlinedIcon from '@material-ui/icons/MusicNoteOutlined'
import { Artwork } from '../common/Artwork'
import { useRefreshOnEvents } from '../common/useRefreshOnEvents'
import { sidebarColors } from '../layout/sidebarStyles'

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

const QuickPickPlaylists = () => {
  const classes = useStyles()
  const locale = useLocale()
  const translate = useTranslate()
  const userId = localStorage.getItem('userId')
  const playlistData = useSelector(
    (state) => state.admin.resources.playlist?.data,
  )
  const { data, loaded, error, refetch } = useQueryWithStore(
    {
      type: 'getList',
      resource: 'playlist',
      payload: {
        pagination: { page: 1, perPage: 0 },
        sort: { field: 'name', order: 'ASC' },
        filter: { owner_id: userId },
      },
    },
    { action: 'CUSTOM_QUERY', enabled: !!userId },
  )
  const onRefresh = useCallback(async () => refetch(), [refetch])
  useRefreshOnEvents({ events: ['playlist'], onRefresh })

  const playlists = useMemo(() => {
    if (!userId || !loaded || !data) return []
    const collator = new Intl.Collator(locale, { sensitivity: 'base' })
    // Keep query membership while picking up local edits from the resource store.
    return Object.values(data)
      .map((playlist) => playlistData?.[playlist.id] || playlist)
      .filter((playlist) => playlist.ownerId === userId)
      .sort(
        (a, b) =>
          collator.compare(a.name, b.name) ||
          String(a.id).localeCompare(String(b.id)),
      )
  }, [data, loaded, playlistData, locale, userId])

  if (!userId) return null

  return (
    <section className={classes.root} aria-label="My playlists">
      {error ? (
        <div className={classes.status} role="alert">
          <Typography className={classes.statusText}>
            Unable to load playlists.
          </Typography>
          <Button
            className={classes.accent}
            onClick={refetch}
            aria-label="Retry playlists"
          >
            Retry
          </Button>
        </div>
      ) : !loaded ? (
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
