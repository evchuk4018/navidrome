import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { NavLink, useHistory, useParams } from 'react-router-dom'
import { useHomeTubePlayback } from './HomeTubePlaybackContext'
import {
  addHomeTubeChannel,
  getHomeTubeChannel,
  getHomeTubeFeed,
  getHomeTubeJob,
  isHomeTubeConfigured,
  listHomeTubeChannels,
  refreshHomeTubeChannel,
  refreshHomeTubeFeed,
  reportHomeTubeImpressions,
  requestHomeTubeDownload,
  updateHomeTubeSubscription,
} from './api'
import useStyles from './styles'

const FEED_LIMIT = 40
const CHANNEL_PAGE_SIZE = 50
const PULL_THRESHOLD = 80
const MAX_PULL = 110
const PULL_DAMPING = 0.5

const getErrorMessage = (error, fallback) =>
  error instanceof Error && error.message ? error.message : fallback

const clampWatchPercentage = (value) =>
  Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0))

const formatDuration = (seconds) => {
  if (!Number.isFinite(seconds)) return null
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remaining = Math.floor(seconds % 60)
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, '0')}:${String(remaining).padStart(2, '0')}`
    : `${minutes}:${String(remaining).padStart(2, '0')}`
}

const formatDate = (value) => {
  if (!value) return 'Date unavailable'
  const [year, month, day] = value.split('-').map(Number)
  if (![year, month, day].every(Number.isFinite)) return 'Date unavailable'
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(new Date(Date.UTC(year, month - 1, day)))
}

const compactNumber = (value) =>
  new Intl.NumberFormat('en-US', {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value)

const statusLabel = (video) => {
  if (video.mediaStatus === 'ready') return 'Downloaded'
  if (video.mediaStatus === 'downloading' || video.mediaStatus === 'queued') {
    return 'Downloading'
  }
  if (video.mediaStatus === 'failed') return 'Download failed'
  return 'Not downloaded'
}

const isActiveJob = (job) =>
  job?.status === 'queued' || job?.status === 'running'

const BrandMark = () => {
  const classes = useStyles()
  return (
    <svg className={classes.brandMark} viewBox="0 0 32 24" aria-hidden="true">
      <rect width="32" height="24" rx="7" fill="currentColor" />
      <path d="m13 7 8 5-8 5V7Z" fill="#fff" />
    </svg>
  )
}

const RefreshIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path
      d="M20 12a8 8 0 1 1-2.34-5.66M20 4v6h-6"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
)

const DownloadIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path
      d="M12 3v11m0 0 4-4m-4 4-4-4M5 19h14"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
)

const PlayIcon = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="m8 5 11 7-11 7V5Z" fill="currentColor" />
  </svg>
)

const getScrollContainer = (element) => {
  if (typeof document === 'undefined') return null
  let parent = element?.parentElement
  while (parent && parent !== document.body) {
    const style = window.getComputedStyle(parent)
    if (/(auto|scroll|overlay)/.test(style.overflowY)) return parent
    parent = parent.parentElement
  }
  return document.scrollingElement || document.documentElement
}

const scrollTop = (container) => {
  if (!container) return 0
  if (
    container === document.scrollingElement ||
    container === document.documentElement
  ) {
    return window.scrollY || container.scrollTop || 0
  }
  return container.scrollTop
}

const HomeTubeUnavailable = ({ disabled = false }) => {
  const classes = useStyles()
  return (
    <HomeTubeLayout active="feed">
      <section className={classes.state} role="status">
        <h1 className={classes.stateTitle}>HomeTube unavailable</h1>
        <p className={classes.stateText}>
          {disabled
            ? 'HomeTube is not configured for this Navidrome server.'
            : 'The HomeTube service is unavailable. Your music library is still ready to use.'}
        </p>
        {!disabled && (
          <button
            className={classes.button}
            type="button"
            onClick={() => window.location.reload()}
          >
            Retry
          </button>
        )}
      </section>
    </HomeTubeLayout>
  )
}

const HomeTubeLayout = ({ active, children, actions = null, title = null }) => {
  const classes = useStyles()
  return (
    <main className={classes.root} data-testid="hometube-root">
      <div className={classes.content}>
        <header className={classes.header}>
          <NavLink
            className={classes.brand}
            to="/hometube"
            exact
            aria-label="HomeTube feed"
          >
            <BrandMark />
            <span>HomeTube</span>
          </NavLink>
          <nav className={classes.sectionNav} aria-label="HomeTube navigation">
            <NavLink
              className={classes.sectionLink}
              to="/hometube"
              exact
              aria-current={active === 'feed' ? 'page' : undefined}
            >
              Feed
            </NavLink>
            <NavLink
              className={classes.sectionLink}
              to="/hometube/channels"
              exact
              aria-current={active === 'channels' ? 'page' : undefined}
            >
              Channels
            </NavLink>
          </nav>
        </header>
        {(title || actions) && (
          <div className={classes.header}>
            {title && <div className={classes.titleBlock}>{title}</div>}
            {actions && <div className={classes.actions}>{actions}</div>}
          </div>
        )}
        {children}
      </div>
    </main>
  )
}

const LoadingState = ({ label = 'Loading HomeTube' }) => {
  const classes = useStyles()
  return (
    <section className={classes.state} role="status" aria-label={label}>
      <span className={classes.spinner} aria-hidden="true">
        <RefreshIcon />
      </span>
      <span>{label}…</span>
    </section>
  )
}

const ErrorState = ({ message, onRetry, label }) => {
  const classes = useStyles()
  return (
    <section className={classes.state} role="alert">
      <h1 className={classes.stateTitle}>
        {label || 'Unable to load HomeTube'}
      </h1>
      <p className={classes.stateText}>{message}</p>
      <button
        className={`${classes.button} ${classes.primaryButton}`}
        type="button"
        onClick={onRetry}
      >
        Retry
      </button>
    </section>
  )
}

const PullToRefresh = ({
  shellRef,
  pull,
  refreshing,
  onPull,
  onRefresh,
  children,
}) => {
  const classes = useStyles()
  const pullRef = useRef(0)
  const pulledRef = useRef(false)

  useEffect(() => {
    const shell = shellRef.current
    if (!shell) return undefined
    const container = getScrollContainer(shell)
    let startY = 0
    let tracking = false
    let pulling = false

    const onTouchStart = (event) => {
      if (event.touches.length !== 1 || scrollTop(container) > 0 || refreshing)
        return
      tracking = true
      pulling = false
      pulledRef.current = false
      startY = event.touches[0].clientY
    }
    const onTouchMove = (event) => {
      if (!tracking) return
      if (!pulling && scrollTop(container) > 0) return
      const deltaY = event.touches[0].clientY - startY
      if (deltaY > 0 || pulling) {
        pulling = true
        if (event.cancelable) event.preventDefault()
        const next = Math.max(0, Math.min(deltaY * PULL_DAMPING, MAX_PULL))
        pullRef.current = next
        onPull(next)
        shell.dataset.hometubePull = String(next)
      }
    }
    const endPull = () => {
      if (!tracking) return
      tracking = false
      pulledRef.current = pulling
      const shouldRefresh = pullRef.current >= PULL_THRESHOLD
      pullRef.current = 0
      onPull(0)
      delete shell.dataset.hometubePull
      if (shouldRefresh) void onRefresh()
    }
    shell.addEventListener('touchstart', onTouchStart, { passive: true })
    shell.addEventListener('touchmove', onTouchMove, { passive: false })
    shell.addEventListener('touchend', endPull, { passive: true })
    shell.addEventListener('touchcancel', endPull, { passive: true })
    return () => {
      shell.removeEventListener('touchstart', onTouchStart)
      shell.removeEventListener('touchmove', onTouchMove)
      shell.removeEventListener('touchend', endPull)
      shell.removeEventListener('touchcancel', endPull)
    }
  }, [onPull, onRefresh, refreshing, shellRef])

  const indicatorActive = pull > 0 || refreshing
  return (
    <div
      ref={shellRef}
      className={classes.feedShell}
      data-testid="hometube-feed-shell"
      style={{
        transform: indicatorActive ? `translateY(${pull}px)` : undefined,
      }}
    >
      <span
        className={`${classes.refreshIndicator} ${indicatorActive ? classes.refreshIndicatorActive : ''} ${refreshing ? classes.refreshIndicatorSpinning : ''}`}
        aria-hidden="true"
      >
        <RefreshIcon />
      </span>
      {children}
    </div>
  )
}

const VideoThumbnail = ({ video, classes }) => {
  const duration = formatDuration(video.durationSeconds)
  const progress = clampWatchPercentage(video.watchPercentage)
  return (
    <div className={classes.thumbnail}>
      {video.thumbnailUrl ? (
        <img
          className={classes.thumbnailImage}
          src={video.thumbnailUrl}
          alt=""
          loading="lazy"
        />
      ) : (
        <div className={classes.thumbnailPlaceholder} aria-hidden="true">
          <BrandMark />
        </div>
      )}
      {duration && <span className={classes.badge}>{duration}</span>}
      {video.liveStatus === 'is_live' && (
        <span className={classes.liveBadge}>LIVE</span>
      )}
      {video.watchState === 'in_progress' && (
        <span
          className={classes.watchProgress}
          style={{ transform: `scaleX(${progress})` }}
        />
      )}
    </div>
  )
}

const VideoMeta = ({ video, classes }) => (
  <>
    <h2 className={classes.cardTitle}>{video.title}</h2>
    <p className={classes.cardMeta}>{video.channelName}</p>
    <p className={classes.cardMeta}>
      {video.viewCount !== null && video.viewCount !== undefined
        ? `${compactNumber(video.viewCount)} views · `
        : ''}
      {formatDate(video.uploadDate)}
    </p>
  </>
)

const FeedCard = ({ video }) => {
  const classes = useStyles()
  const { playVideo } = useHomeTubePlayback()
  return (
    <article className={classes.feedCard}>
      <button
        className={classes.cardButton}
        type="button"
        onClick={() => playVideo(video)}
        aria-label={`Play ${video.title}`}
      >
        <VideoThumbnail video={video} classes={classes} />
        <div className={classes.cardBody}>
          <VideoMeta video={video} classes={classes} />
          <div className={classes.statusRow}>
            <span>{statusLabel(video)}</span>
            {video.watchState === 'in_progress' && (
              <span>
                {Math.round(clampWatchPercentage(video.watchPercentage) * 100)}%
                watched
              </span>
            )}
          </div>
        </div>
      </button>
    </article>
  )
}

export const HomeTubeFeed = () => {
  const classes = useStyles()
  const shellRef = useRef(null)
  const requestGeneration = useRef(0)
  const [videos, setVideos] = useState([])
  const videosRef = useRef(videos)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [pull, setPull] = useState(0)
  const [error, setError] = useState(null)

  useEffect(() => {
    videosRef.current = videos
  }, [videos])

  const loadFeed = useCallback(async (mode = 'initial') => {
    const generation = ++requestGeneration.current
    const isRefresh = mode === 'refresh'
    if (isRefresh) setRefreshing(true)
    else setLoading(true)
    setError(null)
    try {
      let payload
      if (isRefresh && videosRef.current.length > 0) {
        try {
          payload = await refreshHomeTubeFeed(
            videosRef.current.slice(0, 2).map(({ id }) => id),
            FEED_LIMIT,
          )
        } catch {
          payload = await getHomeTubeFeed(FEED_LIMIT)
        }
      } else {
        payload = await getHomeTubeFeed(FEED_LIMIT)
      }
      if (
        generation !== requestGeneration.current ||
        !payload ||
        !Array.isArray(payload.videos)
      )
        return
      setVideos(payload.videos)
    } catch (cause) {
      if (generation === requestGeneration.current)
        setError(getErrorMessage(cause, 'Unable to load the HomeTube feed.'))
    } finally {
      if (generation === requestGeneration.current) {
        setLoading(false)
        setRefreshing(false)
        setPull(0)
      }
    }
  }, [])

  useEffect(() => {
    if (!isHomeTubeConfigured()) return undefined
    void loadFeed()
    return () => {
      requestGeneration.current += 1
    }
  }, [loadFeed])

  useEffect(() => {
    if (!videos.length || !isHomeTubeConfigured()) return undefined
    void reportHomeTubeImpressions(videos.map(({ id }) => id)).catch(
      () => undefined,
    )
    return undefined
  }, [videos])

  const refresh = useCallback(async () => {
    setPull(0)
    await loadFeed('refresh')
  }, [loadFeed])

  if (!isHomeTubeConfigured()) return <HomeTubeUnavailable disabled />

  const title = (
    <div className={classes.titleBlock}>
      <p className={classes.eyebrow}>Recommended for you</p>
      <h1 className={classes.title}>Your Home feed</h1>
      <p className={classes.subtitle}>
        Recent videos from your subscribed channels.
      </p>
    </div>
  )

  return (
    <HomeTubeLayout
      active="feed"
      title={title}
      actions={
        <button
          className={`${classes.button} ${classes.iconButton}`}
          type="button"
          onClick={() => void refresh()}
          disabled={loading || refreshing}
          aria-label="Refresh HomeTube feed"
        >
          <RefreshIcon />
        </button>
      }
    >
      <PullToRefresh
        shellRef={shellRef}
        pull={pull}
        refreshing={refreshing}
        onPull={setPull}
        onRefresh={refresh}
      >
        {error && videos.length > 0 && (
          <p className={classes.alert} role="alert">
            {error}{' '}
            <button
              className={classes.button}
              type="button"
              onClick={() => void refresh()}
            >
              Retry
            </button>
          </p>
        )}
        {loading && videos.length === 0 ? (
          <LoadingState label="Loading HomeTube feed" />
        ) : error && videos.length === 0 ? (
          <ErrorState
            message={error}
            onRetry={() => void loadFeed()}
            label="Unable to load your feed"
          />
        ) : videos.length === 0 ? (
          <section className={classes.empty}>
            <h2 className={classes.emptyTitle}>
              Your Home feed is ready for channels
            </h2>
            <p className={classes.emptyText}>
              Subscribe to a YouTube channel and its videos will begin appearing
              here.
            </p>
            <NavLink
              className={`${classes.button} ${classes.primaryButton}`}
              to="/hometube/channels"
            >
              Add channels
            </NavLink>
          </section>
        ) : (
          <section className={classes.feedGrid} aria-label="Recommended videos">
            {videos.map((video) => (
              <FeedCard key={video.id} video={video} />
            ))}
          </section>
        )}
      </PullToRefresh>
    </HomeTubeLayout>
  )
}

const ChannelAvatar = ({ channel, classes, identity = false }) => (
  <span
    className={`${classes.avatar} ${identity ? classes.identityAvatar : ''}`}
  >
    {channel.thumbnailUrl ? (
      <img
        className={classes.thumbnailImage}
        src={channel.thumbnailUrl}
        alt=""
      />
    ) : (
      channel.name?.slice(0, 1).toUpperCase()
    )}
  </span>
)

const ChannelEntryForm = ({ onAdded }) => {
  const classes = useStyles()
  const [url, setUrl] = useState('')
  const [error, setError] = useState(null)
  const [submitting, setSubmitting] = useState(false)

  const submit = async (event) => {
    event.preventDefault()
    const value = url.trim()
    if (!value) return
    setSubmitting(true)
    setError(null)
    try {
      const result = await addHomeTubeChannel(value)
      if (!result?.channelId)
        throw new Error(result?.error || 'Unable to add this channel.')
      onAdded(result.channelId)
    } catch (cause) {
      setError(getErrorMessage(cause, 'Unable to add this channel.'))
      setSubmitting(false)
    }
  }

  return (
    <form className={classes.form} onSubmit={submit} autoComplete="off">
      <div className={classes.formField}>
        <label htmlFor="hometube-channel-url">YouTube channel URL</label>
        <input
          id="hometube-channel-url"
          type="url"
          inputMode="url"
          autoCapitalize="none"
          autoCorrect="off"
          placeholder="https://youtube.com/@channel"
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          required
          disabled={submitting}
        />
      </div>
      <button
        className={`${classes.button} ${classes.primaryButton}`}
        type="submit"
        disabled={submitting}
      >
        {submitting ? 'Opening…' : 'Open channel'}
      </button>
      {error && (
        <p className={classes.alert} role="alert">
          {error}
        </p>
      )}
    </form>
  )
}

export const HomeTubeChannels = () => {
  const classes = useStyles()
  const history = useHistory()
  const requestGeneration = useRef(0)
  const [channels, setChannels] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  const loadChannels = useCallback(async () => {
    const generation = ++requestGeneration.current
    setLoading(true)
    setError(null)
    try {
      const payload = await listHomeTubeChannels()
      if (
        generation !== requestGeneration.current ||
        !payload ||
        !Array.isArray(payload.channels)
      )
        return
      setChannels(payload.channels)
    } catch (cause) {
      if (generation === requestGeneration.current)
        setError(getErrorMessage(cause, 'Unable to load your channels.'))
    } finally {
      if (generation === requestGeneration.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!isHomeTubeConfigured()) return undefined
    void loadChannels()
    return () => {
      requestGeneration.current += 1
    }
  }, [loadChannels])

  if (!isHomeTubeConfigured()) return <HomeTubeUnavailable disabled />
  const title = (
    <div className={classes.titleBlock}>
      <p className={classes.eyebrow}>YOUR SUBSCRIPTIONS</p>
      <h1 className={classes.title}>Channels</h1>
      <p className={classes.subtitle}>
        Keep your favorite YouTube channels close to the music.
      </p>
    </div>
  )

  return (
    <HomeTubeLayout
      active="channels"
      title={title}
      actions={
        <button
          className={`${classes.button} ${classes.iconButton}`}
          type="button"
          onClick={() => void loadChannels()}
          disabled={loading}
          aria-label="Refresh channels"
        >
          <RefreshIcon />
        </button>
      }
    >
      <ChannelEntryForm
        onAdded={(channelId) => history.push(`/hometube/channels/${channelId}`)}
      />
      {error && (
        <ErrorState
          message={error}
          onRetry={() => void loadChannels()}
          label="Unable to load channels"
        />
      )}
      {loading && !channels.length ? (
        <LoadingState label="Loading channels" />
      ) : !error && channels.length === 0 ? (
        <section className={classes.empty}>
          <h2 className={classes.emptyTitle}>No subscriptions yet</h2>
          <p className={classes.emptyText}>
            Add your first YouTube channel above.
          </p>
        </section>
      ) : (
        <section
          className={classes.channelList}
          aria-label="Subscribed channels"
        >
          {channels.map((channel) => (
            <NavLink
              className={classes.channelRow}
              to={`/hometube/channels/${channel.id}`}
              key={channel.id}
            >
              <ChannelAvatar channel={channel} classes={classes} />
              <span className={classes.channelCopy}>
                <span className={classes.channelName}>{channel.name}</span>
                <span className={classes.channelMeta}>
                  {channel.handle ||
                    `${channel.videoCount.toLocaleString()} videos`}
                </span>
              </span>
              <span className={classes.chevron} aria-hidden="true">
                ›
              </span>
            </NavLink>
          ))}
        </section>
      )}
    </HomeTubeLayout>
  )
}

const SubscriptionButton = ({ channel, onChange, saving }) => {
  const classes = useStyles()
  return (
    <button
      className={`${classes.button} ${channel.subscribed ? '' : classes.primaryButton}`}
      type="button"
      onClick={() => onChange(!channel.subscribed)}
      disabled={saving}
    >
      {saving ? 'Saving…' : channel.subscribed ? 'Subscribed' : 'Subscribe'}
    </button>
  )
}

const ChannelVideoCard = ({ video, job, busy, onDownload }) => {
  const classes = useStyles()
  const { playVideo } = useHomeTubePlayback()
  const active =
    video.mediaStatus === 'queued' || video.mediaStatus === 'downloading'
  const progress =
    job?.progress ?? (video.mediaStatus === 'downloading' ? 5 : 0)
  return (
    <article className={classes.videoCard}>
      <VideoThumbnail video={video} classes={classes} />
      <div className={classes.videoBody}>
        <VideoMeta video={video} classes={classes} />
        {active && (
          <div className={classes.downloadProgress} aria-live="polite">
            <div className={classes.progressLabel}>
              <span>
                {job?.stage ||
                  (video.mediaStatus === 'queued' ? 'Queued' : 'Downloading')}
              </span>
              <span>{job ? `${Math.round(progress)}%` : ''}</span>
            </div>
            <progress className={classes.progress} max="100" value={progress} />
          </div>
        )}
        {video.mediaError && (
          <p className={classes.errorText}>{video.mediaError}</p>
        )}
        <div className={classes.cardActions}>
          {video.mediaStatus === 'ready' ? (
            <button
              className={`${classes.button} ${classes.primaryButton} ${classes.actionButton}`}
              type="button"
              onClick={() => playVideo(video)}
            >
              <PlayIcon /> Play
            </button>
          ) : (
            <button
              className={`${classes.button} ${classes.actionButton}`}
              type="button"
              onClick={onDownload}
              disabled={!video.downloadable || active || busy}
            >
              <DownloadIcon />{' '}
              {busy || active
                ? 'Downloading'
                : video.mediaStatus === 'failed'
                  ? 'Retry'
                  : video.downloadable
                    ? 'Download'
                    : 'Unavailable'}
            </button>
          )}
        </div>
      </div>
    </article>
  )
}

export const HomeTubeChannel = () => {
  const classes = useStyles()
  const { id } = useParams()
  const history = useHistory()
  const requestGeneration = useRef(0)
  const limitRef = useRef(CHANNEL_PAGE_SIZE)
  const channelIdentityRef = useRef(id)
  channelIdentityRef.current = id
  const [payload, setPayload] = useState(null)
  const [downloadJobs, setDownloadJobs] = useState({})
  const [busyDownloads, setBusyDownloads] = useState({})
  const [savingSubscription, setSavingSubscription] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [notice, setNotice] = useState(null)

  const loadChannel = useCallback(
    async ({ limit = limitRef.current, offset = 0, append = false } = {}) => {
      const generation = ++requestGeneration.current
      setLoading(true)
      try {
        const next = await getHomeTubeChannel(id, limit, offset)
        if (
          generation !== requestGeneration.current ||
          channelIdentityRef.current !== id
        )
          return
        if (!next?.channel || !Array.isArray(next.videos))
          throw new Error('HomeTube returned an invalid channel response.')
        setPayload((current) => {
          if (!append || !current) return next
          const existing = new Set(current.videos.map((video) => video.id))
          return {
            ...next,
            videos: [
              ...current.videos,
              ...next.videos.filter((video) => !existing.has(video.id)),
            ],
          }
        })
        limitRef.current = Math.max(
          limitRef.current,
          offset + next.videos.length,
        )
        setError(null)
      } catch (cause) {
        if (
          generation === requestGeneration.current &&
          channelIdentityRef.current === id
        )
          setError(getErrorMessage(cause, 'Unable to load this channel.'))
      } finally {
        if (generation === requestGeneration.current) setLoading(false)
      }
    },
    [id],
  )

  useEffect(() => {
    limitRef.current = CHANNEL_PAGE_SIZE
    setPayload(null)
    setDownloadJobs({})
    setBusyDownloads({})
    setNotice(null)
    setError(null)
    if (!isHomeTubeConfigured()) return undefined
    void loadChannel({ limit: CHANNEL_PAGE_SIZE, offset: 0 })
    return () => {
      requestGeneration.current += 1
    }
  }, [id, loadChannel])

  const activeDownloadJobs = useMemo(
    () => Object.entries(downloadJobs).filter(([, job]) => isActiveJob(job)),
    [downloadJobs],
  )
  const shouldPoll = Boolean(
    payload &&
    (isActiveJob(payload.activeJob) ||
      payload.videos.some(
        (video) =>
          video.mediaStatus === 'queued' || video.mediaStatus === 'downloading',
      ) ||
      activeDownloadJobs.length),
  )

  useEffect(() => {
    if (!shouldPoll) return undefined
    const timer = window.setInterval(() => {
      void loadChannel({
        limit: Math.min(200, Math.max(CHANNEL_PAGE_SIZE, limitRef.current)),
        offset: 0,
      })
      activeDownloadJobs.forEach(([videoId, job]) => {
        void getHomeTubeJob(job.id)
          .then((updated) => {
            if (!updated || channelIdentityRef.current !== id) return
            setDownloadJobs((current) =>
              current[videoId]?.id === job.id
                ? { ...current, [videoId]: updated }
                : current,
            )
            if (updated.status === 'ready' || updated.status === 'failed')
              void loadChannel({
                limit: Math.min(
                  200,
                  Math.max(CHANNEL_PAGE_SIZE, limitRef.current),
                ),
                offset: 0,
              })
          })
          .catch(() => undefined)
      })
    }, 1500)
    return () => window.clearInterval(timer)
  }, [activeDownloadJobs, id, loadChannel, shouldPoll])

  const download = async (video) => {
    setNotice(null)
    setBusyDownloads((current) => ({ ...current, [video.id]: true }))
    try {
      const result = await requestHomeTubeDownload(video.id)
      if (channelIdentityRef.current !== id) return
      if (result?.job)
        setDownloadJobs((current) => ({ ...current, [video.id]: result.job }))
      await loadChannel({
        limit: Math.min(200, Math.max(CHANNEL_PAGE_SIZE, limitRef.current)),
        offset: 0,
      })
    } catch (cause) {
      setNotice(getErrorMessage(cause, 'Unable to start the download.'))
    } finally {
      setBusyDownloads((current) => ({ ...current, [video.id]: false }))
    }
  }

  const refreshCatalog = async () => {
    setRefreshing(true)
    setNotice(null)
    try {
      await refreshHomeTubeChannel(id)
      if (channelIdentityRef.current === id)
        await loadChannel({
          limit: Math.min(200, Math.max(CHANNEL_PAGE_SIZE, limitRef.current)),
          offset: 0,
        })
    } catch (cause) {
      setNotice(getErrorMessage(cause, 'Unable to refresh this channel.'))
    } finally {
      setRefreshing(false)
    }
  }

  const toggleSubscription = async (subscribed) => {
    if (!payload) return
    setSavingSubscription(true)
    setNotice(null)
    try {
      const result = await updateHomeTubeSubscription(id, subscribed)
      const nextChannel = result?.channel || result
      if (channelIdentityRef.current === id && nextChannel?.id)
        setPayload((current) =>
          current ? { ...current, channel: nextChannel } : current,
        )
    } catch (cause) {
      setNotice(getErrorMessage(cause, 'Unable to update this subscription.'))
    } finally {
      setSavingSubscription(false)
    }
  }

  const loadMore = async () => {
    const offset = payload?.videos.length || 0
    await loadChannel({ limit: CHANNEL_PAGE_SIZE, offset, append: true })
  }

  if (!isHomeTubeConfigured()) return <HomeTubeUnavailable disabled />
  if (loading && !payload)
    return (
      <HomeTubeLayout active="channels">
        <LoadingState label="Loading channel" />
      </HomeTubeLayout>
    )
  if (error && !payload)
    return (
      <HomeTubeLayout active="channels">
        <ErrorState
          message={error}
          onRetry={() =>
            void loadChannel({ limit: CHANNEL_PAGE_SIZE, offset: 0 })
          }
          label="Unable to load channel"
        />
      </HomeTubeLayout>
    )
  if (!payload)
    return (
      <HomeTubeLayout active="channels">
        <ErrorState
          message="This channel could not be found."
          onRetry={() => history.push('/hometube/channels')}
          label="Channel unavailable"
        />
      </HomeTubeLayout>
    )

  const importRunning = isActiveJob(payload.activeJob)
  const title = (
    <div className={classes.titleBlock}>
      <p className={classes.eyebrow}>CHANNEL</p>
      <h1 className={classes.title}>{payload.channel.name}</h1>
      <p className={classes.subtitle}>
        {payload.channel.handle ||
          `${payload.channel.videoCount.toLocaleString()} videos`}
      </p>
    </div>
  )
  return (
    <HomeTubeLayout
      active="channels"
      title={title}
      actions={
        <>
          <button
            className={`${classes.button} ${classes.iconButton}`}
            type="button"
            onClick={() => void refreshCatalog()}
            disabled={refreshing || importRunning}
            aria-label="Refresh channel"
          >
            <RefreshIcon />
          </button>
          <SubscriptionButton
            channel={payload.channel}
            onChange={(next) => void toggleSubscription(next)}
            saving={savingSubscription}
          />
        </>
      }
    >
      <div className={classes.channelIdentity}>
        <ChannelAvatar channel={payload.channel} classes={classes} identity />
        <div className={classes.identityCopy}>
          <h2 className={classes.identityTitle}>{payload.channel.name}</h2>
          <p className={classes.identityMeta}>
            {payload.channel.handle ||
              `${payload.channel.videoCount.toLocaleString()} videos`}
          </p>
        </div>
      </div>
      {payload.activeJob && importRunning && (
        <section className={classes.importProgress} aria-live="polite">
          <div className={classes.progressLabel}>
            <span>{payload.activeJob.stage}</span>
            <span>{Math.round(payload.activeJob.progress)}%</span>
          </div>
          <progress
            className={classes.progress}
            max="100"
            value={payload.activeJob.progress}
          />
        </section>
      )}
      {payload.channel.importStatus === 'failed' && (
        <p className={classes.alert} role="alert">
          {payload.channel.importError || 'Channel import failed.'}
        </p>
      )}
      {notice && (
        <p className={classes.alert} role="alert">
          {notice}
        </p>
      )}
      <section
        className={classes.videoGrid}
        aria-label={`${payload.channel.name} videos`}
      >
        {payload.videos.map((video) => (
          <ChannelVideoCard
            key={video.id}
            video={video}
            job={downloadJobs[video.id]}
            busy={Boolean(busyDownloads[video.id])}
            onDownload={() => void download(video)}
          />
        ))}
      </section>
      {payload.videos.length === 0 && !importRunning && (
        <section className={classes.empty}>
          <h2 className={classes.emptyTitle}>No videos found</h2>
          <p className={classes.emptyText}>Try refreshing the channel.</p>
        </section>
      )}
      {payload.videos.length < payload.total && (
        <button
          className={`${classes.button} ${classes.loadMore}`}
          type="button"
          onClick={() => void loadMore()}
          disabled={loading}
        >
          {loading ? 'Loading…' : 'Load more videos'}
        </button>
      )}
    </HomeTubeLayout>
  )
}
