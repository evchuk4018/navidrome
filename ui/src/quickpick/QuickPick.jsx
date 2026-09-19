import React, { useCallback, useEffect, useRef, useState } from 'react'
import {
  Button,
  CircularProgress,
  makeStyles,
  Typography,
} from '@material-ui/core'
import RefreshIcon from '@material-ui/icons/Refresh'
import { useDataProvider, useNotify } from 'react-admin'
import { useDispatch } from 'react-redux'
import { useHistory } from 'react-router-dom'
import { v4 as uuidv4 } from 'uuid'
import { Artwork } from '../common/Artwork'
import { playTracks, setRadioSession, syncRadioTracks } from '../actions'
import {
  createPersonalRadio,
  getQuickPick,
  radioSongs,
  recordPlaylistPlay,
  recordQuickPickImpression,
} from './provider'
import {
  beginQuickPickRequest,
  getQuickPickItemKey,
  isCurrentQuickPickRequest,
  splitQuickPickShelves,
} from './quickPickUtils'

const useStyles = makeStyles((theme) => ({
  root: {
    width: '100%',
    maxWidth: 1040,
    boxSizing: 'border-box',
    margin: '0 auto',
    padding: theme.spacing(3),
  },
  headingRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: theme.spacing(1),
    marginBottom: theme.spacing(2),
  },
  heading: { margin: 0 },
  section: { marginTop: theme.spacing(4), marginBottom: theme.spacing(2) },
  sectionHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginTop: theme.spacing(4),
    marginBottom: theme.spacing(2),
  },
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
    '&:disabled': { cursor: 'wait', opacity: 0.62 },
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
  emptyShelf: { color: theme.palette.text.secondary },
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

const itemRecord = (item) =>
  item?.song || item?.playlist || item?.record || item

const itemTitle = (item) => {
  const record = itemRecord(item)
  return record?.title || record?.name || item?.title || 'Unknown'
}

const itemSubtitle = (item) => {
  const record = itemRecord(item)
  return record?.artist || (item?.playlist || record?.sync ? 'Playlist' : '')
}

const itemIsPlaylist = (item) =>
  !!item?.playlist || item?.kind === 'playlist' || item?.type === 'playlist'

const useVisibleImpressions = (viewId) => {
  const sentRef = useRef(new Set())
  const pendingRef = useRef(new Set())
  const timerRef = useRef(null)

  const flush = useCallback(() => {
    timerRef.current = null
    if (!viewId || !pendingRef.current.size) return
    const itemKeys = Array.from(pendingRef.current)
    pendingRef.current = new Set()
    itemKeys.forEach((itemKey) => sentRef.current.add(itemKey))
    recordQuickPickImpression(viewId, itemKeys).catch(() => {})
  }, [viewId])

  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current)
    flush()
    sentRef.current = new Set()
    pendingRef.current = new Set()
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
      flush()
    }
  }, [flush, viewId])

  return useCallback(
    (item) => {
      const itemKey = getQuickPickItemKey(item)
      if (!viewId || !itemKey || sentRef.current.has(itemKey)) return
      pendingRef.current.add(itemKey)
      if (!timerRef.current) timerRef.current = setTimeout(flush, 75)
    },
    [flush, viewId],
  )
}

const QuickPickTile = ({ item, busy, onPlay, onVisible, classes }) => {
  const [node, setNode] = useState(null)
  const record = itemRecord(item)
  const title = itemTitle(item)
  const subtitle = itemSubtitle(item)
  const identity = `${subtitle}:${title}`

  useEffect(() => {
    if (!node) return undefined
    if (typeof IntersectionObserver === 'undefined') {
      onVisible(item)
      return undefined
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) onVisible(item)
    })
    observer.observe(node)
    return () => observer.disconnect()
  }, [item, node, onVisible])

  return (
    <button
      ref={setNode}
      type="button"
      key={getQuickPickItemKey(item)}
      className={classes.tile}
      onClick={() => onPlay(item)}
      disabled={busy}
      aria-label={`Play ${title}${itemIsPlaylist(item) ? '' : ' radio'}`}
      data-testid={`quick-pick-item-${getQuickPickItemKey(item)}`}
      data-view-id={item.viewId || undefined}
    >
      <div
        className={classes.fallback}
        style={{ background: identityColor(identity) }}
      >
        {initials(subtitle || title)}
      </div>
      {record && (
        <Artwork record={record} square className={classes.artwork} title="" />
      )}
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
}

const QuickPick = () => {
  const classes = useStyles()
  const dispatch = useDispatch()
  const history = useHistory()
  const notify = useNotify()
  const dataProvider = useDataProvider()
  const [items, setItems] = useState(null)
  const [viewId, setViewId] = useState(null)
  const [loading, setLoading] = useState(false)
  const [startingKey, setStartingKey] = useState(null)
  const loadRef = useRef({ token: 0, controller: null })
  const startRef = useRef({ token: 0, key: null, controller: null })
  const onVisible = useVisibleImpressions(viewId)

  const loadItems = useCallback(() => {
    const previous = loadRef.current
    previous.controller?.abort()
    const controller = new AbortController()
    const token = previous.token + 1
    loadRef.current = { token, controller }
    setLoading(true)
    getQuickPick({ signal: controller.signal })
      .then((response) => {
        if (loadRef.current.token !== token) return
        setItems(response?.items || [])
        setViewId(response?.viewId || null)
      })
      .catch((error) => {
        if (error?.name === 'AbortError' || loadRef.current.token !== token)
          return
        notify('Unable to load Quick Pick', 'warning')
      })
      .finally(() => {
        if (loadRef.current.token === token) setLoading(false)
      })
  }, [notify])

  useEffect(() => {
    loadItems()
    return () => loadRef.current.controller?.abort()
  }, [loadItems])

  const attachRadio = useCallback(
    (response, token) => {
      if (!response?.session?.id || startRef.current.token !== token) return
      const enriched = radioSongs(response)
      const playlistContext =
        response.session.sourcePlaylistId || response.sourcePlaylistId
      const seed = playlistContext
        ? undefined
        : (response.items || []).find((item) => item.type === 'seed')
      dispatch(
        setRadioSession({
          ...response.session,
          id: response.session.id,
          seedItemId: seed?.id,
          revision: response.revision,
          mode: response.session.mode || response.mode || 'balanced',
          autoplay: response.session.autoplay !== false,
          sourcePlaylistId: response.session.sourcePlaylistId,
        }),
      )
      const ids = playlistContext
        ? enriched.ids.filter(
            (key) => enriched.data[key].radioItemType !== 'seed',
          )
        : enriched.ids
      if (ids.length) {
        dispatch(
          syncRadioTracks(enriched.data, ids, {
            sessionId: enriched.sessionId || response.session.id,
            revision: enriched.revision,
            authoritative: enriched.authoritative,
          }),
        )
      }
    },
    [dispatch],
  )

  const beginStart = useCallback((item, run) => {
    const key = getQuickPickItemKey(item)
    const request = beginQuickPickRequest(startRef, key)
    if (!request) return
    const { token, controller } = request
    setStartingKey(key)
    Promise.resolve()
      .then(() => run({ token, controller, clientRequestId: uuidv4() }))
      .catch(() => {})
      .finally(() => {
        if (isCurrentQuickPickRequest(startRef, token)) {
          startRef.current = { token, key: null, controller: null }
          setStartingKey(null)
        }
      })
  }, [])

  const playPlaylist = useCallback(
    (item, request) => {
      const playlist = item.playlist || item
      return dataProvider
        .getList('playlistTrack', {
          pagination: { page: 1, perPage: -1 },
          sort: { field: 'id', order: 'ASC' },
          filter: { playlist_id: playlist.id },
        })
        .then(({ data }) => {
          if (startRef.current.token !== request.token) return
          const songs = Object.fromEntries(data.map((song) => [song.id, song]))
          dispatch(
            playTracks(
              songs,
              data.map((song) => song.id),
            ),
          )
          recordPlaylistPlay(playlist.id).catch(() => {})
          history.push(`/playlist/${playlist.id}/show`)
          return createPersonalRadio({
            sourcePlaylistId: playlist.id,
            mode: 'balanced',
            clientRequestId: request.clientRequestId,
          }).then((response) => attachRadio(response, request.token))
        })
        .catch(() => notify('Unable to play playlist', 'warning'))
    },
    [attachRadio, dataProvider, dispatch, history, notify],
  )

  const playSongRadio = useCallback(
    (item, request) => {
      const song = item.song || item
      const seedId = song.mediaFileId || song.id
      if (!seedId) return Promise.resolve()
      // Load the local seed immediately; radio creation is allowed to finish
      // asynchronously without blocking the first audible track.
      dispatch(playTracks({ [song.id || seedId]: song }, [song.id || seedId]))
      return createPersonalRadio({
        seedMediaFileId: seedId,
        mode: 'balanced',
        clientRequestId: request.clientRequestId,
      }).then((response) => attachRadio(response, request.token))
    },
    [attachRadio, dispatch],
  )

  const playItem = useCallback(
    (item) => {
      beginStart(item, (request) =>
        itemIsPlaylist(item)
          ? playPlaylist(item, request)
          : playSongRadio(item, request),
      )
    },
    [beginStart, playPlaylist, playSongRadio],
  )

  if (items == null)
    return (
      <div className={classes.center}>
        <CircularProgress />
      </div>
    )

  const withView = items.map((item) => ({ ...item, viewId }))
  const { listenAgain, startRadio } = splitQuickPickShelves(withView)
  const renderShelf = (shelf, label) => (
    <section aria-label={label}>
      <div className={classes.sectionHeader}>
        <Typography component="h2" variant="h6">
          {label}
        </Typography>
        {label === 'Start radio' && loading && <CircularProgress size={20} />}
      </div>
      {shelf.length ? (
        <div
          className={classes.grid}
          data-testid={`quick-pick-${label.replace(/\s+/g, '-').toLowerCase()}`}
        >
          {shelf.map((item) => (
            <QuickPickTile
              key={getQuickPickItemKey(item)}
              item={item}
              busy={startingKey === getQuickPickItemKey(item)}
              onPlay={playItem}
              onVisible={onVisible}
              classes={classes}
            />
          ))}
        </div>
      ) : (
        <Typography className={classes.emptyShelf}>No picks yet.</Typography>
      )}
    </section>
  )

  return (
    <main className={classes.root}>
      <div className={classes.headingRow}>
        <Typography component="h1" variant="h4" className={classes.heading}>
          Quick Pick
        </Typography>
        <Button
          size="small"
          color="primary"
          startIcon={<RefreshIcon />}
          onClick={() => loadItems(true)}
          disabled={loading}
          data-testid="quick-pick-refresh"
        >
          Refresh
        </Button>
      </div>
      {items.length === 0 ? (
        <Typography>
          Play a few songs or playlists and your favorites will appear here.
        </Typography>
      ) : (
        <>
          {renderShelf(listenAgain, 'Listen again')}
          {renderShelf(startRadio, 'Start radio')}
        </>
      )}
    </main>
  )
}

export default QuickPick
