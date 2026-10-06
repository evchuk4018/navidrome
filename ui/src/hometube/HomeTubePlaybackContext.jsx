import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useMediaSessionCoordinator } from '../audioplayer/MediaSessionCoordinator'
import MiniPlayer from '../audioplayer/MiniPlayer'
import { pauseAudio, playAudio } from '../audioplayer/playback'
import config from '../config'
import HomeTubePlayer from './HomeTubePlayer'
import { homeTubeApiPath, homeTubeRequest } from './api'

const HomeTubeContext = createContext(null)

const DISABLED_VALUE = {
  enabled: false,
  activeSource: null,
  currentVideo: null,
  queue: [],
  status: 'idle',
  expanded: false,
  playVideo: () => Promise.resolve(false),
  claimMusic: () => {},
}

const getVideoDuration = (video) =>
  Number(video?.durationSeconds || video?.playbackDurationSeconds || 0)

const isReady = (video) => video?.mediaStatus === 'ready'

const shouldResume = (video, duration) => {
  if (video?.watchState === 'watched') return 0
  const saved = Number(video?.playbackPositionSeconds || 0)
  if (
    !Number.isFinite(saved) ||
    saved <= 0 ||
    !Number.isFinite(duration) ||
    duration <= 0
  ) {
    return 0
  }
  return saved < Math.max(0, duration - 5) ? Math.min(saved, duration) : 0
}

const requestJson = (path, options) => homeTubeRequest(path, options)

const HomeTubeMiniPlayer = ({
  video,
  element,
  currentTime,
  duration,
  isPlaying,
  onExpand,
  onTogglePlayback,
}) => {
  if (!video) return null
  return (
    <MiniPlayer
      track={{
        name: video.title,
        singer: video.channelName,
        cover: video.thumbnailUrl || '',
        duration: duration || getVideoDuration(video),
      }}
      audioInstance={element}
      currentTime={currentTime}
      duration={duration || getVideoDuration(video)}
      isPlaying={isPlaying}
      onExpand={onExpand}
      onTogglePlayback={onTogglePlayback}
      openLabel="Open HomeTube player"
      playLabel="Play HomeTube video"
      pauseLabel="Pause HomeTube video"
    />
  )
}

const HomeTubePlayback = ({ children }) => {
  const enabled = Boolean(config.homeTubeBaseURL)
  const coordinator = useMediaSessionCoordinator()
  const [currentVideo, setCurrentVideo] = useState(null)
  const [queue, setQueue] = useState([])
  const [status, setStatus] = useState('idle')
  const [activeSource, setActiveSource] = useState(null)
  const [expanded, setExpanded] = useState(false)
  const [element, setElement] = useState(null)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [autoplayNonce, setAutoplayNonce] = useState(0)
  const [sleepExpired, setSleepExpired] = useState(false)
  const sleepExpiredRef = useRef(false)
  const operationRef = useRef(0)
  const progressRef = useRef({ position: 0, duration: 0 })
  const lastProgressRef = useRef(0)
  const currentVideoRef = useRef(null)
  const queueRef = useRef(queue)
  const expandedRef = useRef(expanded)

  currentVideoRef.current = currentVideo
  queueRef.current = queue
  expandedRef.current = expanded

  const isCurrentOperation = useCallback(
    (operation) => operationRef.current === operation,
    [],
  )

  const updateProgress = useCallback(
    (position, nextDuration, force = false) => {
      const safePosition = Math.max(0, Number(position) || 0)
      const safeDuration = Math.max(0, Number(nextDuration) || 0)
      progressRef.current = { position: safePosition, duration: safeDuration }
      if (force || Date.now() - lastProgressRef.current >= 250) {
        lastProgressRef.current = Date.now()
        setCurrentTime(safePosition)
        if (safeDuration) setDuration(safeDuration)
      }
    },
    [],
  )

  const saveProgress = useCallback(
    async (video, position, nextDuration, keepalive = false) => {
      if (
        !enabled ||
        !video?.id ||
        !Number.isFinite(Number(nextDuration)) ||
        Number(nextDuration) <= 0
      ) {
        return
      }
      try {
        await requestJson(`/api/videos/${video.id}/progress`, {
          method: 'PUT',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({
            positionSeconds: Math.max(0, Number(position) || 0),
            durationSeconds: Number(nextDuration),
          }),
          keepalive,
        })
      } catch {
        // Progress is advisory. A transient HomeTube outage must not interrupt
        // playback or transfer ownership back to the music engine.
      }
    },
    [enabled],
  )

  const refreshQueue = useCallback(
    async (videoId, operation) => {
      if (!enabled || !videoId) return []
      try {
        const result = await requestJson('/api/queue', {
          method: 'PUT',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ currentVideoId: videoId }),
        })
        if (!isCurrentOperation(operation)) return []
        const entries = Array.isArray(result?.entries) ? result.entries : []
        setQueue(entries)
        const head = entries[0]?.video
        if (head?.id === videoId) {
          setCurrentVideo(head)
          currentVideoRef.current = head
        }
        return entries
      } catch {
        return []
      }
    },
    [enabled, isCurrentOperation],
  )

  const startDownload = useCallback(
    async (video, operation) => {
      if (!enabled || !video?.id) return false
      try {
        await requestJson(`/api/videos/${video.id}/download`, {
          method: 'POST',
        })
        if (!isCurrentOperation(operation)) return false
        return true
      } catch (error) {
        if (isCurrentOperation(operation)) {
          setStatus('error')
          setCurrentVideo((current) =>
            current?.id === video.id
              ? {
                  ...current,
                  mediaError:
                    error?.message || 'Unable to start this download.',
                }
              : current,
          )
        }
        return false
      }
    },
    [enabled, isCurrentOperation],
  )

  const selectVideo = useCallback(
    (video, shouldAutoplay = true, advanceQueue = false, openPlayer = true) => {
      if (!video?.id) return
      const previousVideo = currentVideoRef.current
      if (previousVideo && previousVideo.id !== video.id) {
        const { position, duration: previousDuration } = progressRef.current
        void saveProgress(previousVideo, position, previousDuration)
      }
      setCurrentVideo(video)
      currentVideoRef.current = video
      if (advanceQueue) {
        setQueue((currentQueue) => {
          const index = currentQueue.findIndex(
            (entry) => entry.video.id === video.id,
          )
          return index >= 0 ? currentQueue.slice(index) : currentQueue
        })
      }
      setCurrentTime(Number(video.playbackPositionSeconds) || 0)
      setDuration(getVideoDuration(video))
      progressRef.current = {
        position: Number(video.playbackPositionSeconds) || 0,
        duration: getVideoDuration(video),
      }
      setStatus(isReady(video) ? 'ready' : 'preparing')
      setActiveSource('hometube')
      setExpanded(openPlayer)
      sleepExpiredRef.current = false
      setSleepExpired(false)
      if (isReady(video) && shouldAutoplay)
        setAutoplayNonce((nonce) => nonce + 1)
    },
    [saveProgress],
  )

  const playVideo = useCallback(
    async (video, options = {}) => {
      if (!enabled || !video?.id) return false
      if (
        options.automatic &&
        (sleepExpiredRef.current || coordinator?.isPlaybackBlocked?.())
      )
        return false
      if (!options.automatic) coordinator?.allowPlayback?.()
      const operation = operationRef.current + 1
      operationRef.current = operation
      // Explicit HomeTube selection always pauses whichever source currently
      // owns playback. Background queue responses never call this path.
      coordinator?.pauseActive?.()
      const selectedWasReady = isReady(video)
      selectVideo(
        video,
        true,
        false,
        options.preserveView ? expandedRef.current : true,
      )

      // Keep the recommendation/open signal separate from queue hydration.
      // The operation guard below makes a late response harmless after a
      // different source or video takes ownership.
      void requestJson(`/api/videos/${video.id}/open`, {
        method: 'POST',
      }).catch(() => {})

      let entries = await refreshQueue(video.id, operation)
      if (!isCurrentOperation(operation)) return false
      const current = entries[0]?.video || video
      if (!isReady(current)) {
        setStatus('preparing')
        await startDownload(current, operation)
        if (!isCurrentOperation(operation)) return false
        entries = await refreshQueue(current.id, operation)
      }
      if (isCurrentOperation(operation)) {
        const readyVideo = entries[0]?.video
        if (readyVideo?.id === current.id && isReady(readyVideo)) {
          setCurrentVideo(readyVideo)
          setStatus('ready')
          if (!selectedWasReady) setAutoplayNonce((nonce) => nonce + 1)
        }
      }
      return isCurrentOperation(operation)
    },
    [
      coordinator,
      enabled,
      isCurrentOperation,
      refreshQueue,
      selectVideo,
      startDownload,
    ],
  )

  const claimMusic = useCallback(() => {
    if (activeSource === 'hometube') {
      // This is an explicit source switch. Invalidate every in-flight queue or
      // download poll before pausing HomeTube so a late response cannot start
      // it again over the newly playing music.
      operationRef.current += 1
      element?.pause?.()
      coordinator?.pauseActive?.()
    }
    setActiveSource('music')
    setExpanded(false)
  }, [activeSource, coordinator, element])

  const claimHomeTube = useCallback(() => {
    setActiveSource('hometube')
    setStatus((current) => (current === 'preparing' ? current : 'playing'))
  }, [])

  const handleSleepPause = useCallback(() => {
    sleepExpiredRef.current = true
    operationRef.current += 1
    setSleepExpired(true)
    setAutoplayNonce(0)
    setStatus('paused')
    element?.pause?.()
  }, [element])

  const handleSleepRearm = useCallback(() => {
    coordinator?.allowPlayback?.()
    sleepExpiredRef.current = false
    setSleepExpired(false)
  }, [coordinator])

  const handleAutoplayRejected = useCallback(() => {
    setStatus('paused')
  }, [])

  const handleProgress = useCallback(
    ({ position, duration: nextDuration, event = 'timeupdate' }) => {
      updateProgress(position, nextDuration, event !== 'timeupdate')
      const video = currentVideoRef.current
      if (!video) return
      if (event === 'play') {
        claimHomeTube()
      } else if (event === 'pause') {
        setStatus('paused')
        void saveProgress(video, position, nextDuration)
      } else if (event === 'seeked' || event === 'ended') {
        void saveProgress(video, position, nextDuration)
      }
    },
    [claimHomeTube, saveProgress, updateProgress],
  )

  const handleEnded = useCallback(() => {
    const next = queueRef.current[1]?.video
    if (
      next &&
      !sleepExpiredRef.current &&
      !coordinator?.isPlaybackBlocked?.()
    ) {
      void playVideo(next, { preserveView: true, automatic: true })
      return
    }
    setStatus('paused')
    setActiveSource('hometube')
  }, [coordinator, playVideo])

  const dismissEntry = useCallback(
    async (videoId) => {
      const current = currentVideoRef.current
      if (!current || !videoId) return
      const operation = operationRef.current
      try {
        const result = await requestJson('/api/queue/dismiss', {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ currentVideoId: current.id, videoId }),
        })
        if (
          operationRef.current === operation &&
          currentVideoRef.current?.id === current.id &&
          Array.isArray(result?.entries)
        ) {
          setQueue(result.entries)
        }
      } catch {
        if (currentVideoRef.current?.id === current.id) {
          await refreshQueue(current.id, operationRef.current)
        }
      }
    },
    [refreshQueue],
  )

  useEffect(() => {
    if (!enabled || !currentVideo || !queue.length || sleepExpired)
      return undefined
    const pending = queue.filter(
      (entry) => entry?.job && !['ready', 'failed'].includes(entry.job.status),
    )
    if (!pending.length) return undefined
    const operation = operationRef.current
    const timer = window.setInterval(async () => {
      const updates = await Promise.all(
        pending.map(async (entry) => {
          try {
            const job = await requestJson(`/api/jobs/${entry.job.id}`, {
              cache: 'no-store',
            })
            let video = entry.video
            if (job?.status === 'ready' || job?.status === 'failed') {
              try {
                video = await requestJson(`/api/videos/${entry.video.id}`, {
                  cache: 'no-store',
                })
              } catch {
                // Keep the queue's previous video summary until the next poll.
              }
            }
            return { ...entry, job, video }
          } catch {
            return null
          }
        }),
      )
      if (!isCurrentOperation(operation)) return
      const fresh = updates.filter(Boolean)
      if (!fresh.length) return
      setQueue((currentQueue) =>
        currentQueue.map(
          (entry) =>
            fresh.find((next) => next.video.id === entry.video.id) || entry,
        ),
      )
      const currentUpdate = fresh.find(
        (entry) => entry.video.id === currentVideoRef.current?.id,
      )
      if (currentUpdate?.video) {
        setCurrentVideo(currentUpdate.video)
        if (isReady(currentUpdate.video)) {
          setStatus('ready')
          setAutoplayNonce((nonce) => nonce + 1)
        }
      }
    }, 1500)
    return () => window.clearInterval(timer)
  }, [currentVideo, enabled, isCurrentOperation, queue, sleepExpired])

  useEffect(() => {
    if (!enabled) return undefined
    const saveOnExit = () => {
      const video = currentVideoRef.current
      const { position, duration: nextDuration } = progressRef.current
      if (video) void saveProgress(video, position, nextDuration, true)
    }
    const saveWhenHidden = () => {
      if (document.visibilityState === 'hidden') saveOnExit()
    }
    window.addEventListener('pagehide', saveOnExit)
    document.addEventListener('visibilitychange', saveWhenHidden)
    return () => {
      window.removeEventListener('pagehide', saveOnExit)
      document.removeEventListener('visibilitychange', saveWhenHidden)
      saveOnExit()
    }
  }, [enabled, saveProgress])

  useEffect(() => {
    if (!enabled || !currentVideo || !element) return undefined
    let lastSavedAt = 0
    const savePeriodically = () => {
      const now = Date.now()
      const { position, duration: nextDuration } = progressRef.current
      if (now - lastSavedAt < 10000) return
      lastSavedAt = now
      void saveProgress(currentVideoRef.current, position, nextDuration)
    }
    element.addEventListener('timeupdate', savePeriodically)
    return () => element.removeEventListener('timeupdate', savePeriodically)
  }, [currentVideo, enabled, element, saveProgress])

  const value = useMemo(
    () => ({
      enabled,
      activeSource,
      currentVideo,
      queue,
      status,
      expanded,
      playVideo,
      claimMusic,
      claimHomeTube,
      setExpanded,
      sleepExpired,
      dismissEntry,
      homeTubeApiPath,
    }),
    [
      activeSource,
      claimHomeTube,
      claimMusic,
      currentVideo,
      dismissEntry,
      enabled,
      expanded,
      playVideo,
      queue,
      sleepExpired,
      status,
    ],
  )

  if (!enabled)
    return (
      <HomeTubeContext.Provider value={DISABLED_VALUE}>
        {children}
      </HomeTubeContext.Provider>
    )

  return (
    <HomeTubeContext.Provider value={value}>
      {children}
      <HomeTubePlayer
        video={currentVideo}
        queue={queue}
        active={activeSource === 'hometube'}
        expanded={expanded && activeSource === 'hometube'}
        autoplayNonce={autoplayNonce}
        onElementChange={setElement}
        onProgress={handleProgress}
        onEnded={handleEnded}
        onAutoplayRejected={handleAutoplayRejected}
        onClose={() => setExpanded(false)}
        onPlay={claimHomeTube}
        onSleepExpire={handleSleepPause}
        onUserPlayback={handleSleepRearm}
        onNext={(requestedVideo) => {
          const next = requestedVideo?.id
            ? requestedVideo
            : queueRef.current[1]?.video
          if (next) void playVideo(next, { preserveView: true })
        }}
        onDismiss={dismissEntry}
        onRetry={() => {
          if (currentVideoRef.current) void playVideo(currentVideoRef.current)
        }}
      />
      {activeSource === 'hometube' && currentVideo && !expanded && (
        <HomeTubeMiniPlayer
          video={currentVideo}
          element={element}
          currentTime={currentTime}
          duration={duration}
          isPlaying={status === 'playing' || (element && !element.paused)}
          onExpand={() => setExpanded(true)}
          onTogglePlayback={() => {
            handleSleepRearm()
            if (element?.paused) playAudio(element)
            else pauseAudio(element)
          }}
        />
      )}
    </HomeTubeContext.Provider>
  )
}

export const HomeTubePlaybackProvider = HomeTubePlayback

// eslint-disable-next-line react-refresh/only-export-components
export const useHomeTubePlayback = () =>
  useContext(HomeTubeContext) || DISABLED_VALUE

// eslint-disable-next-line react-refresh/only-export-components
export { shouldResume }

export default HomeTubePlaybackProvider
