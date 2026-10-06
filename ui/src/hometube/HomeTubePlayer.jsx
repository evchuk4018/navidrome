import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { makeStyles } from '@material-ui/core/styles'
import CloseIcon from '@material-ui/icons/Close'
import FullscreenIcon from '@material-ui/icons/Fullscreen'
import FullscreenExitIcon from '@material-ui/icons/FullscreenExit'
import PauseIcon from '@material-ui/icons/Pause'
import PlayArrowIcon from '@material-ui/icons/PlayArrow'
import SkipNextIcon from '@material-ui/icons/SkipNext'
import { useMediaSessionSource } from '../audioplayer/MediaSessionCoordinator'
import { playAudio, pauseAudio } from '../audioplayer/playback'
import { homeTubeApiPath } from './api'

const useStyles = makeStyles((theme) => ({
  shell: {
    position: 'fixed',
    right: 0,
    bottom: 0,
    left: 0,
    zIndex: 1300,
    color: '#ffe9f2',
    pointerEvents: (props) => (props.expanded ? 'auto' : 'none'),
  },
  hiddenMedia: {
    position: 'fixed',
    width: 1,
    height: 1,
    opacity: 0,
    pointerEvents: 'none',
  },
  overlay: {
    position: 'fixed',
    inset: 0,
    display: 'flex',
    flexDirection: 'column',
    overflowY: 'auto',
    zIndex: 1302,
    background: 'linear-gradient(to bottom, rgba(13, 13, 13, 0.86), rgba(13, 13, 13, 0.18) 42%, rgba(13, 13, 13, 0.98) 74%)',
    pointerEvents: 'none',
    touchAction: 'pan-y',
  },
  topBar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    minHeight: 56,
    padding: theme.spacing(1, 2),
    pointerEvents: 'auto',
  },
  closeButton: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 44,
    height: 44,
    color: '#ffe9f2',
    background: 'transparent',
    border: 0,
    borderRadius: '50%',
    cursor: 'pointer',
    '&:hover': { backgroundColor: '#29141f' },
    '&:focus-visible': { outline: '2px solid #ff2a7f' },
  },
  mediaFrame: {
    position: 'relative',
    width: 'min(100%, 1100px)',
    margin: '0 auto',
    backgroundColor: 'transparent',
    pointerEvents: 'none',
    aspectRatio: '16 / 9',
  },
  video: {
    position: 'fixed',
    top: 56,
    left: 0,
    zIndex: 1301,
    display: 'block',
    width: '100vw',
    height: 'min(64vh, calc(100vw * 0.5625))',
    objectFit: 'contain',
    backgroundColor: '#000',
  },
  backgroundAudio: {
    position: 'fixed',
    right: theme.spacing(2),
    bottom: theme.spacing(2),
    left: theme.spacing(2),
    width: 'calc(100% - 32px)',
    zIndex: 1303,
    opacity: 0.9,
  },
  fullscreenButton: {
    position: 'absolute',
    right: theme.spacing(1),
    bottom: theme.spacing(1),
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 42,
    height: 42,
    color: '#fff',
    background: 'rgba(0, 0, 0, 0.6)',
    border: '1px solid rgba(255, 255, 255, 0.2)',
    borderRadius: '50%',
    cursor: 'pointer',
  },
  waiting: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 230,
    padding: theme.spacing(4),
    textAlign: 'center',
    background: 'linear-gradient(140deg, #29141f, #0d0d0d)',
    pointerEvents: 'auto',
  },
  progress: { width: 'min(380px, 80%)', accentColor: '#ff2a7f' },
  metadata: { padding: theme.spacing(2, 2, 4), margin: '0 auto', width: 'min(100%, 1100px)', pointerEvents: 'auto' },
  title: { margin: 0, fontSize: '1.25rem', fontWeight: 600 },
  channel: { margin: theme.spacing(0.5, 0, 1), color: '#ff91be' },
  actions: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing(1) },
  action: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    minHeight: 38,
    padding: theme.spacing(0.5, 1.25),
    color: '#ffe9f2',
    background: '#29141f',
    border: '1px solid #54253b',
    borderRadius: 18,
    cursor: 'pointer',
    '&:hover': { background: '#462033' },
    '&:focus-visible': { outline: '2px solid #ff2a7f' },
  },
  queue: { marginTop: theme.spacing(2), padding: 0, listStyle: 'none' },
  queueEntry: { display: 'flex', alignItems: 'center', gap: theme.spacing(1), padding: theme.spacing(0.5, 0) },
  queueButton: { flex: 1, minWidth: 0, color: '#ffe9f2', textAlign: 'left', background: 'transparent', border: 0, cursor: 'pointer', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  queueDismiss: { color: '#c6aab5', background: 'transparent', border: 0, cursor: 'pointer' },
}))

const getDuration = (video) => Number(video?.durationSeconds || video?.playbackDurationSeconds || 0)

const HomeTubePlayer = ({
  video,
  queue = [],
  active = false,
  expanded,
  autoplayNonce = 0,
  onElementChange,
  onProgress,
  onEnded,
  onAutoplayRejected,
  onClose,
  onPlay,
  onRetry,
  onNext,
  onDismiss,
  sleepTimer,
}) => {
  const classes = useStyles({ expanded })
  const shellRef = useRef(null)
  const videoRef = useRef(null)
  const audioRef = useRef(null)
  const [videoElement, setVideoElement] = useState(null)
  const [audioElement, setAudioElement] = useState(null)
  const [fullscreenMode, setFullscreenMode] = useState('none')
  const [isPlaying, setIsPlaying] = useState(false)
  const restoredVideoId = useRef(null)
  const touchStartY = useRef(null)
  const autoplayCancelledRef = useRef(false)
  const activeRef = useRef(active)
  activeRef.current = active
  const mediaReady = video?.mediaStatus === 'ready'
  const hasBackgroundAudio = Boolean(video?.hasBackgroundAudio)
  const mediaElement = hasBackgroundAudio ? audioElement : videoElement

  const setVideoNode = useCallback((node) => {
    videoRef.current = node
    setVideoElement((current) => (current === node ? current : node))
  }, [])
  const setAudioNode = useCallback((node) => {
    audioRef.current = node
    setAudioElement((current) => (current === node ? current : node))
  }, [])

  const metadata = useMemo(
    () =>
      video
        ? {
            title: video.title,
            artist: video.channelName || '',
            album: 'HomeTube',
            artwork: video.thumbnailUrl
              ? [{ src: video.thumbnailUrl, sizes: '480x360' }]
              : [],
          }
        : null,
    [video],
  )

  useMediaSessionSource({
    source: 'hometube',
    element: mediaElement,
    metadata,
    active: Boolean(mediaElement && active),
    onNext,
  })

  useEffect(() => {
    onElementChange?.(mediaElement || null)
  }, [mediaElement, onElementChange])

  useEffect(() => {
    if (!mediaElement) return undefined
    const report = (event) => {
      if (!activeRef.current) {
        if (event.type === 'play' || event.type === 'playing') pauseAudio(mediaElement)
        return
      }
      const eventType =
        event.type === 'play' || event.type === 'playing'
          ? 'play'
          : event.type === 'pause'
            ? 'pause'
            : event.type === 'seeked'
              ? 'seeked'
              : event.type === 'ended'
                ? 'ended'
                : 'timeupdate'
      setIsPlaying(!mediaElement.paused && !mediaElement.ended)
      onProgress?.({
        position: mediaElement.currentTime,
        duration: mediaElement.duration,
        event: eventType,
      })
      if (eventType === 'play') onPlay?.()
      if (eventType === 'ended') onEnded?.()
    }
    const events = ['play', 'playing', 'pause', 'timeupdate', 'seeked', 'ended']
    events.forEach((event) => mediaElement.addEventListener(event, report))
    return () => events.forEach((event) => mediaElement.removeEventListener(event, report))
  }, [active, mediaElement, onEnded, onPlay, onProgress])

  useEffect(() => {
    if (!mediaElement || !video || restoredVideoId.current === video.id) return
    const restore = () => {
      if (restoredVideoId.current === video.id) return
      if (!Number.isFinite(mediaElement.duration) || mediaElement.duration <= 0) return
      const saved = Number(video.playbackPositionSeconds || 0)
      const position =
        video.watchState === 'watched' || saved >= mediaElement.duration - 5
          ? 0
          : Math.max(0, Math.min(saved, mediaElement.duration))
      restoredVideoId.current = video.id
      if (position > 0) {
        try {
          mediaElement.currentTime = position
        } catch {
          // Metadata may be invalidated while the source changes.
        }
      }
    }
    mediaElement.addEventListener('loadedmetadata', restore)
    mediaElement.addEventListener('durationchange', restore)
    restore()
    return () => {
      mediaElement.removeEventListener('loadedmetadata', restore)
      mediaElement.removeEventListener('durationchange', restore)
    }
  }, [mediaElement, video])

  useEffect(() => {
    if (!active || !mediaElement || !mediaReady || !autoplayNonce) return undefined
    let cancelled = false
    autoplayCancelledRef.current = false
    const attempt = () => {
      if (!active || cancelled || autoplayCancelledRef.current || !mediaElement.paused || mediaElement.ended) return
      const result = mediaElement.play()
      if (result?.catch) result.catch(() => onAutoplayRejected?.())
    }
    const cancel = () => {
      autoplayCancelledRef.current = true
    }
    attempt()
    mediaElement.addEventListener('canplay', attempt)
    mediaElement.addEventListener('loadedmetadata', attempt)
    mediaElement.addEventListener('pause', cancel)
    return () => {
      cancelled = true
      autoplayCancelledRef.current = true
      mediaElement.removeEventListener('canplay', attempt)
      mediaElement.removeEventListener('loadedmetadata', attempt)
      mediaElement.removeEventListener('pause', cancel)
    }
  }, [active, autoplayNonce, mediaElement, mediaReady, onAutoplayRejected])

  useEffect(() => {
    if (!hasBackgroundAudio || !videoElement || !audioElement) return undefined
    const syncPosition = (force = false) => {
      const gap = Math.abs((audioElement.currentTime || 0) - (videoElement.currentTime || 0))
      if (force || gap > 0.25) {
        try {
          videoElement.currentTime = audioElement.currentTime
        } catch {
          // The visual element can still be loading its metadata.
        }
      }
    }
    const syncVisual = () => {
      if (!expanded || document.hidden || audioElement.paused || audioElement.ended) {
        if (!videoElement.paused) videoElement.pause()
        return
      }
      syncPosition()
      if (videoElement.paused) playAudio(videoElement)
    }
    const onSeek = () => syncPosition(true)
    const onRate = () => {
      videoElement.playbackRate = audioElement.playbackRate
    }
    audioElement.addEventListener('play', syncVisual)
    audioElement.addEventListener('playing', syncVisual)
    audioElement.addEventListener('pause', syncVisual)
    audioElement.addEventListener('waiting', syncVisual)
    audioElement.addEventListener('timeupdate', syncVisual)
    audioElement.addEventListener('seeking', onSeek)
    audioElement.addEventListener('seeked', syncVisual)
    audioElement.addEventListener('ratechange', onRate)
    syncVisual()
    return () => {
      audioElement.removeEventListener('play', syncVisual)
      audioElement.removeEventListener('playing', syncVisual)
      audioElement.removeEventListener('pause', syncVisual)
      audioElement.removeEventListener('waiting', syncVisual)
      audioElement.removeEventListener('timeupdate', syncVisual)
      audioElement.removeEventListener('seeking', onSeek)
      audioElement.removeEventListener('seeked', syncVisual)
      audioElement.removeEventListener('ratechange', onRate)
    }
  }, [audioElement, expanded, hasBackgroundAudio, videoElement])

  useEffect(() => {
    const onFullscreen = () =>
      setFullscreenMode(document.fullscreenElement ? 'element' : 'none')
    const onNativeEnter = () => setFullscreenMode('native')
    const onNativeExit = () => setFullscreenMode('none')
    document.addEventListener('fullscreenchange', onFullscreen)
    videoElement?.addEventListener('webkitbeginfullscreen', onNativeEnter)
    videoElement?.addEventListener('webkitendfullscreen', onNativeExit)
    return () => {
      document.removeEventListener('fullscreenchange', onFullscreen)
      videoElement?.removeEventListener('webkitbeginfullscreen', onNativeEnter)
      videoElement?.removeEventListener('webkitendfullscreen', onNativeExit)
    }
  }, [videoElement])

  useEffect(() => {
    if (typeof navigator === 'undefined' || !expanded || !navigator.wakeLock?.request) return undefined
    let lock
    let cancelled = false
    navigator.wakeLock
      .request('screen')
      .then((nextLock) => {
        if (cancelled) {
          const release = nextLock.release?.()
          return release?.catch?.(() => {})
        }
        lock = nextLock
        return undefined
      })
      .catch(() => {})
    return () => {
      cancelled = true
      const release = lock?.release?.()
      release?.catch?.(() => {})
    }
  }, [expanded])

  const enterFullscreen = useCallback(() => {
    if (fullscreenMode === 'viewport') {
      setFullscreenMode('none')
      return
    }
    if (document.fullscreenElement) {
      const result = document.exitFullscreen?.()
      result?.catch?.(() => {})
      return
    }
    const shell = shellRef.current
    if (shell?.requestFullscreen) {
      const request = shell.requestFullscreen()
      if (!request?.then) {
        setFullscreenMode('element')
        return
      }
      request.then(() => setFullscreenMode('element')).catch(() => {
        const nativeVideo = videoRef.current
        if (nativeVideo?.webkitEnterFullscreen) {
          try {
            nativeVideo.webkitEnterFullscreen()
            setFullscreenMode('native')
            return
          } catch {
            // Continue to the viewport fallback.
          }
        }
        setFullscreenMode('viewport')
      })
      return
    }
    const nativeVideo = videoRef.current
    if (nativeVideo?.webkitEnterFullscreen) {
      try {
        nativeVideo.webkitEnterFullscreen()
        setFullscreenMode('native')
        return
      } catch {
        // Continue to the viewport fallback.
      }
    }
    setFullscreenMode('viewport')
  }, [fullscreenMode])

  const handleTouchStart = useCallback((event) => {
    touchStartY.current = event.touches[0]?.clientY ?? null
  }, [])

  const handleTouchEnd = useCallback(
    (event) => {
      const startY = touchStartY.current
      const endY = event.changedTouches[0]?.clientY
      touchStartY.current = null
      if (startY != null && endY != null && endY - startY >= 60) onClose?.()
    },
    [onClose],
  )

  if (!video) return null

  const progress = queue[0]?.job?.progress ?? (video.mediaStatus === 'downloading' ? 5 : 0)
  const videoSrc = mediaReady ? homeTubeApiPath(`/api/videos/${video.id}/stream`) : undefined
  const audioSrc = mediaReady && hasBackgroundAudio ? homeTubeApiPath(`/api/videos/${video.id}/audio`) : undefined

  return (
    <div
      ref={shellRef}
      className={classes.shell}
      data-testid="hometube-player"
      onTouchStart={expanded ? handleTouchStart : undefined}
      onTouchEnd={expanded ? handleTouchEnd : undefined}
    >
      <video
        ref={setVideoNode}
        className={expanded ? classes.video : classes.hiddenMedia}
        src={videoSrc}
        poster={video.thumbnailUrl || undefined}
        controls={expanded && !hasBackgroundAudio}
        muted={hasBackgroundAudio}
        playsInline
        preload="metadata"
        aria-label={video.title}
      />
      {hasBackgroundAudio && (
        <audio
          ref={setAudioNode}
          className={expanded ? classes.backgroundAudio : classes.hiddenMedia}
          src={audioSrc}
          controls={expanded}
          preload="metadata"
          aria-label={`Playback controls for ${video.title}`}
        />
      )}
      {expanded && (
        <div className={classes.overlay} role="dialog" aria-label="HomeTube player">
          <div className={classes.topBar}>
            <button type="button" className={classes.closeButton} onClick={onClose} aria-label="Minimize HomeTube player">
              <CloseIcon />
            </button>
            <span>HomeTube</span>
            <button type="button" className={classes.closeButton} onClick={enterFullscreen} aria-label={fullscreenMode !== 'none' ? 'Exit fullscreen' : 'Enter fullscreen'}>
              {fullscreenMode !== 'none' ? <FullscreenExitIcon /> : <FullscreenIcon />}
            </button>
          </div>
          <div className={classes.mediaFrame}>
            {!mediaReady && (
              <div className={classes.waiting} data-testid="hometube-preparing">
                <strong>{video.mediaError ? 'Download interrupted' : 'Getting your video ready'}</strong>
                <span>{video.mediaError || queue[0]?.job?.stage || 'Starting download'}</span>
                {video.mediaError ? (
                  <>
                    <span>{video.mediaError}</span>
                    <button type="button" className={classes.action} onClick={onRetry}>
                      Retry download
                    </button>
                  </>
                ) : <progress className={classes.progress} max="100" value={progress} />}
              </div>
            )}
          </div>
          <section className={classes.metadata}>
            <h1 className={classes.title}>{video.title}</h1>
            <div className={classes.channel}>
              <a href={`#/hometube/channels/${video.channelId}`} onClick={onClose} style={{ color: 'inherit' }}>
                {video.channelName}
              </a>
            </div>
            <div className={classes.actions}>
              <button type="button" className={classes.action} onClick={() => (mediaElement?.paused ? playAudio(mediaElement) : pauseAudio(mediaElement))} disabled={!mediaReady}>
                {isPlaying ? <PauseIcon /> : <PlayArrowIcon />}
                {isPlaying ? 'Pause' : 'Play'}
              </button>
              <button type="button" className={classes.action} onClick={() => onNext?.()} disabled={!queue[1]}>
                <SkipNextIcon /> Next
              </button>
              {sleepTimer}
            </div>
            {queue.length > 1 && (
              <ol className={classes.queue} aria-label="Autoplay queue">
                {queue.slice(1).map((entry) => (
                  <li key={entry.video.id} className={classes.queueEntry}>
                    <button type="button" className={classes.queueButton} onClick={() => onNext?.(entry.video)}>
                      {entry.video.title}
                    </button>
                    <button type="button" className={classes.queueDismiss} onClick={() => onDismiss?.(entry.video.id)} aria-label={`Dismiss ${entry.video.title}`}>
                      ×
                    </button>
                  </li>
                ))}
              </ol>
            )}
          </section>
        </div>
      )}
    </div>
  )
}

export default HomeTubePlayer
