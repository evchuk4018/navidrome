import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useLayoutEffect,
} from 'react'
import { createPortal } from 'react-dom'
import HomeTubePlayerView from './HomeTubePlayerView'
import { GlobalHotKeys } from 'react-hotkeys'
import { keyMap } from '../hotkeys'
import { makeStyles } from '@material-ui/core/styles'
import { usePlaybackQueue } from '../audioplayer/PlaybackQueueContext'
import { useMediaSessionSource } from '../audioplayer/MediaSessionCoordinator'
import { playAudio, pauseAudio } from '../audioplayer/playback'
import { homeTubeApiPath } from './api'

const useStyles = makeStyles(() => ({
  shell: {
    position: 'relative',
    '&.hometube-fullscreen': {
      position: 'fixed',
      inset: 0,
      zIndex: 1400,
      background: '#0d0d0d',
      '& .hometube-artwork': {
        position: 'fixed',
        inset: '0 0 100px',
        width: '100% !important',
        height: 'auto !important',
        maxHeight: 'calc(100dvh - 100px)',
        margin: 0,
        zIndex: 1250,
      },
      '@media (max-width:768px) and (orientation:portrait)': {
        '& .hometube-artwork': {
          bottom: 300,
          maxHeight: 'calc(100dvh - 300px)',
        },
      },
    },
  },
  mediaFrame: {
    position: 'relative',
    width: '100%',
    height: '100%',
    backgroundColor: '#000',
    aspectRatio: '16/9',
  },
  video: {
    display: 'block',
    width: '100%',
    height: '100%',
    objectFit: 'contain',
  },
  hiddenMedia: {
    position: 'fixed',
    width: 1,
    height: 1,
    opacity: 0,
    pointerEvents: 'none',
  },
  waiting: {
    position: 'absolute',
    inset: 0,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: '#0d0d0d',
    gap: 8,
    color: '#ff91be',
    textAlign: 'center',
  },
}))

// Move a stable portal host between the dependency's two artwork slots. The
// media element survives portrait/landscape selection as well as minimizing.
const VideoArtworkSlot = ({ host }) => {
  const slot = useRef(null)
  useLayoutEffect(() => {
    slot.current.appendChild(host)
  }, [host])
  return <div ref={slot} style={{ display: 'contents' }} />
}

const getDuration = (video) =>
  Number(video?.durationSeconds || video?.playbackDurationSeconds || 0)

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
  onSleepExpire,
  onUserPlayback,
}) => {
  const classes = useStyles({ expanded })
  const playbackQueue = usePlaybackQueue()
  const [mediaHost] = useState(() => {
    const host = document.createElement('div')
    host.style.display = 'contents'
    return host
  })
  const shellRef = useRef(null)
  const videoRef = useRef(null)
  const audioRef = useRef(null)
  const [videoElement, setVideoElement] = useState(null)
  const [audioElement, setAudioElement] = useState(null)
  const [fullscreenMode, setFullscreenMode] = useState('none')
  const [isPlaying, setIsPlaying] = useState(false)
  const [position, setPosition] = useState(0)
  const [duration, setDuration] = useState(0)
  const restoredVideoId = useRef(null)
  const touchStartY = useRef(null)
  const autoplayCancelledRef = useRef(false)
  const activeRef = useRef(active)
  activeRef.current = active
  const mediaReady = video?.mediaStatus === 'ready'
  const hasBackgroundAudio = Boolean(video?.hasBackgroundAudio)
  const mediaElement = hasBackgroundAudio ? audioElement : videoElement

  useEffect(() => {
    setPosition(Number(video?.playbackPositionSeconds) || 0)
    setDuration(0)
  }, [video?.id, video?.playbackPositionSeconds])

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

  const coordinator = useMediaSessionSource({
    source: 'hometube',
    element: mediaElement,
    metadata,
    active: Boolean(mediaElement && active),
    onPrevious: playbackQueue?.previous,
    onNext: playbackQueue?.next || (queue[1] ? onNext : undefined),
    onSleepExpire,
    onUserPlayback,
  })

  useEffect(() => {
    onElementChange?.(mediaElement || null)
  }, [mediaElement, onElementChange])

  useEffect(() => {
    if (!mediaElement) return undefined
    const report = (event) => {
      if (!activeRef.current) {
        if (event.type === 'play' || event.type === 'playing')
          pauseAudio(mediaElement)
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
      setPosition(Math.max(0, Number(mediaElement.currentTime) || 0))
      if (Number.isFinite(mediaElement.duration))
        setDuration(mediaElement.duration)
      onProgress?.({
        position: mediaElement.currentTime,
        duration: mediaElement.duration,
        event: eventType,
      })
      if (eventType === 'play') onPlay?.()
      if (eventType === 'ended') onEnded?.()
    }
    const events = [
      'play',
      'playing',
      'pause',
      'timeupdate',
      'seeked',
      'ended',
      'loadedmetadata',
      'durationchange',
    ]
    events.forEach((event) => mediaElement.addEventListener(event, report))
    return () =>
      events.forEach((event) => mediaElement.removeEventListener(event, report))
  }, [active, mediaElement, onEnded, onPlay, onProgress])

  useEffect(() => {
    if (!mediaElement || !video || restoredVideoId.current === video.id) return
    const restore = () => {
      if (restoredVideoId.current === video.id) return
      if (!Number.isFinite(mediaElement.duration) || mediaElement.duration <= 0)
        return
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
    if (!active || !mediaElement || !mediaReady || !autoplayNonce)
      return undefined
    let cancelled = false
    autoplayCancelledRef.current = false
    const attempt = () => {
      if (
        !active ||
        cancelled ||
        autoplayCancelledRef.current ||
        coordinator?.isPlaybackBlocked?.() ||
        !mediaElement.paused ||
        mediaElement.ended
      )
        return
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
  }, [
    active,
    autoplayNonce,
    coordinator,
    mediaElement,
    mediaReady,
    onAutoplayRejected,
  ])

  useEffect(() => {
    if (!hasBackgroundAudio || !videoElement || !audioElement) return undefined
    const syncPosition = (force = false) => {
      const gap = Math.abs(
        (audioElement.currentTime || 0) - (videoElement.currentTime || 0),
      )
      if (force || gap > 0.25) {
        try {
          videoElement.currentTime = audioElement.currentTime
        } catch {
          // The visual element can still be loading its metadata.
        }
      }
    }
    const syncVisual = () => {
      if (
        !expanded ||
        document.hidden ||
        audioElement.paused ||
        audioElement.ended
      ) {
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
    if (
      typeof navigator === 'undefined' ||
      !expanded ||
      !navigator.wakeLock?.request
    )
      return undefined
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
      request
        .then(() => setFullscreenMode('element'))
        .catch(() => {
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
    const dialog = event.target.closest('[role=dialog]')
    touchStartY.current =
      event.target.closest('button, input, [role=slider], [role=button]') ||
      (dialog && dialog !== shellRef.current) ||
      (event.target.closest('.react-jinke-music-player-mobile')?.scrollTop ||
        shellRef.current?.scrollTop) > 0
        ? null
        : (event.touches[0]?.clientY ?? null)
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

  const progress =
    queue[0]?.job?.progress ?? (video.mediaStatus === 'downloading' ? 5 : 0)
  const videoSrc = mediaReady
    ? homeTubeApiPath(`/api/videos/${video.id}/stream`)
    : undefined
  const audioSrc =
    mediaReady && hasBackgroundAudio
      ? homeTubeApiPath(`/api/videos/${video.id}/audio`)
      : undefined

  const totalDuration = Math.max(0, duration || getDuration(video))
  const safePosition = Math.min(position, totalDuration || position)
  const userPlayback = (command) => {
    coordinator?.allowPlayback?.()
    onUserPlayback?.()
    command()
  }

  const artwork = (
    <div className={`${classes.mediaFrame} img-content hometube-artwork`}>
      <video
        ref={setVideoNode}
        className={classes.video}
        src={videoSrc}
        poster={video.thumbnailUrl || undefined}
        controls={false}
        muted={hasBackgroundAudio}
        playsInline
        preload="metadata"
        aria-label={video.title}
      />
      {!mediaReady && (
        <div className={classes.waiting} data-testid="hometube-preparing">
          <strong>
            {video.mediaError
              ? 'Download interrupted'
              : 'Getting your video ready'}
          </strong>
          <span>
            {video.mediaError || queue[0]?.job?.stage || 'Starting download'}
          </span>
          {video.mediaError ? (
            <button type="button" onClick={onRetry}>
              Retry download
            </button>
          ) : (
            <progress max="100" value={progress} />
          )}
        </div>
      )}
    </div>
  )
  const handlers = active
    ? {
        TOGGLE_PLAY: (event) => {
          event.preventDefault()
          userPlayback(() =>
            mediaElement?.paused
              ? playAudio(mediaElement)
              : pauseAudio(mediaElement),
          )
        },
        PREV_SONG: (event) => {
          if (!event.metaKey) playbackQueue?.previous()
        },
        NEXT_SONG: (event) => {
          if (!event.metaKey) playbackQueue ? playbackQueue.next() : onNext?.()
        },
        VOL_UP: () => {
          if (mediaElement)
            mediaElement.volume = Math.min(1, mediaElement.volume + 0.1)
        },
        VOL_DOWN: () => {
          if (mediaElement)
            mediaElement.volume = Math.max(0, mediaElement.volume - 0.1)
        },
      }
    : {}
  return (
    <div
      ref={shellRef}
      className={`${classes.shell} ${['element', 'viewport'].includes(fullscreenMode) ? 'hometube-fullscreen' : ''}`}
      data-fullscreen-mode={fullscreenMode}
      data-testid="hometube-player"
      onTouchStart={expanded ? handleTouchStart : undefined}
      onTouchEnd={expanded ? handleTouchEnd : undefined}
    >
      <HomeTubePlayerView
        video={video}
        active={active}
        expanded={expanded}
        artwork={<VideoArtworkSlot host={mediaHost} />}
        element={mediaElement}
        position={safePosition}
        duration={totalDuration}
        playing={isPlaying}
        ready={mediaReady}
        queue={queue}
        onClose={() => {
          if (fullscreenMode !== 'none') {
            document.exitFullscreen?.()?.catch?.(() => {})
            setFullscreenMode('none')
          } else onClose?.()
        }}
        onToggle={() =>
          userPlayback(() =>
            mediaElement?.paused
              ? playAudio(mediaElement)
              : pauseAudio(mediaElement),
          )
        }
        onNext={(requested) => userPlayback(() => onNext?.(requested))}
        onDismiss={onDismiss}
        onFullscreen={enterFullscreen}
        fullscreen={fullscreenMode !== 'none'}
      />
      {createPortal(artwork, mediaHost)}
      <GlobalHotKeys handlers={handlers} keyMap={keyMap} allowChanges />
      {hasBackgroundAudio && (
        <audio
          ref={setAudioNode}
          className={classes.hiddenMedia}
          src={audioSrc}
          controls={false}
          preload="metadata"
          aria-label={`Playback controls for ${video.title}`}
        />
      )}
    </div>
  )
}

export default HomeTubePlayer
