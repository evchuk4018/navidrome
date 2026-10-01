import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Button,
  CircularProgress,
  makeStyles,
  Typography,
} from '@material-ui/core'
import { useNotify } from 'react-admin'
import { useDispatch } from 'react-redux'
import { Artwork } from '../common/Artwork'
import { sidebarColors } from '../layout/sidebarStyles'
import QuickPickPlaylists from './QuickPickPlaylists'
import {
  getQuickPick,
  recordQuickPickClick,
  recordQuickPickImpressions,
} from './provider'
import { startRelatedRadio } from './startRelatedRadio'

const useStyles = makeStyles((theme) => ({
  root: {
    width: '100%',
    maxWidth: 1040,
    minWidth: 0,
    boxSizing: 'border-box',
    margin: '0 auto',
    padding: theme.spacing(3),
    containerType: 'inline-size',
    fontFamily: "system-ui, 'Helvetica Neue', Helvetica, Arial, sans-serif",
    color: sidebarColors.text,
    [theme.breakpoints.down('xs')]: { padding: theme.spacing(2) },
  },
  grid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(3, minmax(0, 1fr))',
    gap: theme.spacing(2.5),
    [theme.breakpoints.down('xs')]: { gap: theme.spacing(1.5) },
  },
  tile: {
    position: 'relative',
    aspectRatio: '4 / 5',
    border: 0,
    padding: 0,
    borderRadius: 16,
    overflow: 'hidden',
    backgroundColor: 'rgba(255,255,255,.04)',
    color: sidebarColors.text,
    fontFamily: 'inherit',
    cursor: 'pointer',
    transition: 'transform 120ms ease, background-color 120ms ease',
    '&:hover': {
      transform: 'translateY(-2px)',
      backgroundColor: sidebarColors.selection,
    },
    '&:focus-visible': {
      outline: `2px solid ${sidebarColors.accent}`,
      outlineOffset: -2,
    },
    '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
    [theme.breakpoints.down('xs')]: { borderRadius: 12 },
  },
  cover: {
    position: 'absolute',
    top: theme.spacing(1.5),
    left: theme.spacing(1.5),
    right: theme.spacing(1.5),
    aspectRatio: '1 / 1',
    borderRadius: 10,
    overflow: 'hidden',
    backgroundColor: sidebarColors.divider,
    [theme.breakpoints.down('xs')]: {
      top: theme.spacing(0.75),
      left: theme.spacing(0.75),
      right: theme.spacing(0.75),
      borderRadius: 8,
    },
  },
  artwork: { position: 'absolute', inset: 0, width: '100%', height: '100%' },
  fallback: {
    position: 'absolute',
    inset: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 'clamp(1.5rem, 7cqw, 4rem)',
    fontWeight: 800,
    letterSpacing: '-0.06em',
    color: sidebarColors.secondary,
  },
  shade: {
    position: 'absolute',
    inset: 0,
    background: 'linear-gradient(transparent 40%, rgba(13,13,13,.9) 90%)',
    pointerEvents: 'none',
  },
  label: {
    position: 'absolute',
    left: theme.spacing(1.5),
    right: theme.spacing(1.5),
    bottom: theme.spacing(1.75),
    textAlign: 'left',
    textShadow: '0 1px 3px #000',
    [theme.breakpoints.down('xs')]: {
      left: theme.spacing(1),
      right: theme.spacing(1),
      bottom: theme.spacing(1),
    },
  },
  title: {
    '&&': {
      color: `${sidebarColors.text} !important`,
      fontFamily: 'inherit',
      fontSize: 'clamp(12px, 4.2cqw, 26px)',
      fontWeight: 700,
      lineHeight: 1.2,
    },
  },
  subtitle: {
    '&&': {
      color: `${sidebarColors.secondary} !important`,
      fontFamily: 'inherit',
      fontSize: 'clamp(11px, 3.6cqw, 22px)',
      lineHeight: 1.25,
      marginTop: 4,
    },
  },
  status: {
    display: 'flex',
    minHeight: 96,
    gap: theme.spacing(1.5),
    alignItems: 'center',
    justifyContent: 'center',
    flexWrap: 'wrap',
  },
  statusText: { '&&': { color: `${sidebarColors.secondary} !important` } },
  accent: { '&&': { color: `${sidebarColors.accent} !important` } },
}))

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
  const notify = useNotify()
  const [response, setResponse] = useState(null)
  const [error, setError] = useState(false)
  const [loadVersion, setLoadVersion] = useState(0)
  const impressions = useRef(null)
  const recommendations = useMemo(
    () =>
      (response?.items || []).filter(
        (item) => item.section === 'start_radio' && item.song?.id,
      ),
    [response],
  )

  useEffect(() => {
    let alive = true
    setError(false)
    setResponse(null)
    getQuickPick()
      .then((data) => alive && setResponse(data))
      .catch(() => alive && setError(true))
    return () => {
      alive = false
    }
  }, [loadVersion])

  useEffect(() => {
    if (!response?.viewId || impressions.current?.viewId === response.viewId)
      return
    const itemKeys = recommendations.map((item) => item.itemKey).filter(Boolean)
    if (!itemKeys.length) return
    const request = recordQuickPickImpressions(response.viewId, itemKeys)
    request.catch(() => {})
    impressions.current = { viewId: response.viewId, request }
  }, [response, recommendations])

  const playSongRadio = useCallback(
    (item) => {
      startRelatedRadio(dispatch, notify, item.song)
      const tracked = impressions.current
      if (tracked && item.itemKey && item.viewId === tracked.viewId) {
        tracked.request
          .then(() => recordQuickPickClick(item.viewId, item.itemKey))
          .catch(() => {})
      }
    },
    [dispatch, notify],
  )

  const renderTiles = () =>
    recommendations.map((item) => {
      const record = item.song
      const title = record.title
      const subtitle = record.artist
      return (
        <button
          type="button"
          key={`${item.kind}-${record.id}`}
          className={classes.tile}
          onClick={() => playSongRadio(item)}
          aria-label={`Play ${title} radio`}
        >
          <div className={classes.cover}>
            <div className={classes.fallback} aria-hidden="true">
              {initials(subtitle || title || '')}
            </div>
            <Artwork
              record={record}
              square
              className={classes.artwork}
              title=""
            />
          </div>
          <div className={classes.shade} />
          <div className={classes.label}>
            <Typography className={classes.title} noWrap>
              {title}
            </Typography>
            <Typography className={classes.subtitle} variant="body2" noWrap>
              {subtitle}
            </Typography>
          </div>
        </button>
      )
    })

  return (
    <div className={classes.root}>
      <section aria-label="Discover">
        {error ? (
          <div className={classes.status} role="alert">
            <Typography className={classes.statusText}>
              Unable to load discoveries.
            </Typography>
            <Button
              className={classes.accent}
              onClick={() => setLoadVersion((version) => version + 1)}
              aria-label="Retry discoveries"
            >
              Retry
            </Button>
          </div>
        ) : response == null ? (
          <div className={classes.status}>
            <CircularProgress
              size={28}
              className={classes.accent}
              aria-label="Loading discoveries"
            />
          </div>
        ) : recommendations.length === 0 ? (
          <div className={classes.status}>
            <Typography className={classes.statusText}>
              No songs to discover yet.
            </Typography>
          </div>
        ) : (
          <div className={classes.grid}>{renderTiles()}</div>
        )}
      </section>
      <QuickPickPlaylists />
    </div>
  )
}

export default QuickPick
