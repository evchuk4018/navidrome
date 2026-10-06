import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { pauseAudio, playAudio } from './playback'

const MEDIA_SESSION_ACTIONS = [
  'play',
  'pause',
  'seekbackward',
  'seekforward',
  'previoustrack',
  'nexttrack',
  'seekto',
]

const MediaSessionContext = createContext(null)

const setActionHandler = (mediaSession, action, handler) => {
  try {
    mediaSession.setActionHandler(action, handler)
  } catch {
    // Media Session is implemented piecemeal by browsers. Unsupported
    // actions should not make a player unusable.
  }
}

const getMediaSession = () =>
  typeof navigator === 'undefined' ? null : navigator.mediaSession || null

const makeMetadata = (metadata) => {
  if (!metadata) return null
  if (typeof MediaMetadata === 'function') return new MediaMetadata(metadata)
  return metadata
}

const updatePositionState = (mediaSession, element) => {
  if (!mediaSession || !element) return
  const duration = Number(element.duration)
  if (!Number.isFinite(duration) || duration <= 0) return
  try {
    mediaSession.setPositionState({
      duration,
      playbackRate: Number(element.playbackRate) || 1,
      position: Math.max(
        0,
        Math.min(Number(element.currentTime) || 0, duration),
      ),
    })
  } catch {
    // Position state is best effort (for example, Safari rejects a stale
    // duration while a source is being replaced).
  }
}

/**
 * Owns the browser MediaSession for every player in the application.
 *
 * Music and HomeTube each register a source, but only the source that most
 * recently began playback is allowed to install metadata and action handlers.
 * This prevents an inactive video element or a late music queue response from
 * stealing the controls shown by the operating system.
 */
export const MediaSessionCoordinator = ({ children }) => {
  const registrations = useRef(new Map())
  const activeId = useRef(null)
  const sleepBlocked = useRef(false)
  const sleepTimerCheck = useRef(null)
  const [revision, setRevision] = useState(0)
  const redraw = useCallback(() => setRevision((revision) => revision + 1), [])

  const register = useCallback(
    (id, descriptor) => {
      registrations.current.set(id, descriptor)
      const guard = (event) => {
        sleepTimerCheck.current?.()
        if (!sleepBlocked.current) return
        pauseAudio(descriptor.element)
        // The music engine advances its queue unconditionally on ended.
        // Capture prevents that handler running at or after the deadline.
        event?.stopImmediatePropagation?.()
      }
      const events = ['play', 'playing', 'ended']
      events.forEach((event) =>
        descriptor.element.addEventListener(event, guard, true),
      )
      redraw()
      return () => {
        events.forEach((event) =>
          descriptor.element.removeEventListener(event, guard, true),
        )
        registrations.current.delete(id)
        if (activeId.current === id) activeId.current = null
        redraw()
      }
    },
    [redraw],
  )

  const update = useCallback(
    (id, descriptor) => {
      if (!registrations.current.has(id)) return
      registrations.current.set(id, descriptor)
      redraw()
    },
    [redraw],
  )

  const activate = useCallback(
    (id) => {
      if (!registrations.current.has(id)) return
      activeId.current = id
      redraw()
    },
    [redraw],
  )

  const deactivate = useCallback(
    (id) => {
      if (activeId.current === id) {
        // Keep the paused source selected. The OS play button can resume it,
        // and a newly playing source will explicitly replace it.
        redraw()
      }
    },
    [redraw],
  )

  const pauseActive = useCallback(() => {
    const descriptor = registrations.current.get(activeId.current)
    if (descriptor?.element) pauseAudio(descriptor.element)
    return activeId.current
  }, [])

  const expireSleep = useCallback(() => {
    sleepBlocked.current = true
    const descriptor = registrations.current.get(activeId.current)
    registrations.current.forEach((source) => source.onSleepExpire?.())
    if (descriptor?.element) pauseAudio(descriptor.element)
  }, [])

  const allowPlayback = useCallback(() => {
    sleepTimerCheck.current?.()
    sleepBlocked.current = false
  }, [])

  const isPlaybackBlocked = useCallback(() => {
    sleepTimerCheck.current?.()
    return sleepBlocked.current
  }, [])

  const setSleepTimerCheck = useCallback((check) => {
    sleepTimerCheck.current = check
    return () => {
      if (sleepTimerCheck.current === check) sleepTimerCheck.current = null
    }
  }, [])

  const value = useMemo(
    () => ({
      register,
      update,
      activate,
      deactivate,
      pauseActive,
      expireSleep,
      allowPlayback,
      isPlaybackBlocked,
      setSleepTimerCheck,
    }),
    [
      activate,
      deactivate,
      pauseActive,
      register,
      update,
      expireSleep,
      allowPlayback,
      isPlaybackBlocked,
      setSleepTimerCheck,
    ],
  )

  useEffect(() => {
    const mediaSession = getMediaSession()
    if (!mediaSession?.setActionHandler) return undefined

    const descriptor = registrations.current.get(activeId.current)
    const element = descriptor?.element
    if (!descriptor || !element) {
      MEDIA_SESSION_ACTIONS.forEach((action) =>
        setActionHandler(mediaSession, action, null),
      )
      mediaSession.metadata = null
      mediaSession.playbackState = 'none'
      return undefined
    }

    mediaSession.metadata = makeMetadata(descriptor.metadata)
    const userPlayback = (command) => () => {
      allowPlayback()
      descriptor.onUserPlayback?.()
      command()
    }
    const play = userPlayback(() => playAudio(element, descriptor.audioContext))
    const pause = () => pauseAudio(element)
    const seekBackward = (details = {}) => {
      if (typeof descriptor.onSeekBackward === 'function') {
        descriptor.onSeekBackward(details)
        return
      }
      element.currentTime = Math.max(
        0,
        element.currentTime - Number(details.seekOffset || 10),
      )
    }
    const seekForward = (details = {}) => {
      if (typeof descriptor.onSeekForward === 'function') {
        descriptor.onSeekForward(details)
        return
      }
      element.currentTime = Math.min(
        Number.isFinite(element.duration) ? element.duration : Infinity,
        element.currentTime + Number(details.seekOffset || 10),
      )
    }
    const seekTo = (details = {}) => {
      if (details.seekTime != null) element.currentTime = details.seekTime
    }
    const actions = {
      play,
      pause,
      seekbackward: seekBackward,
      seekforward: seekForward,
      seekto: seekTo,
      previoustrack: descriptor.onPrevious
        ? userPlayback(descriptor.onPrevious)
        : seekBackward,
      nexttrack: descriptor.onNext
        ? userPlayback(descriptor.onNext)
        : seekForward,
    }
    MEDIA_SESSION_ACTIONS.forEach((action) =>
      setActionHandler(mediaSession, action, actions[action] || null),
    )

    const updatePlaybackState = () => {
      mediaSession.playbackState = element.paused ? 'paused' : 'playing'
      updatePositionState(mediaSession, element)
    }
    const events = ['play', 'playing', 'pause', 'durationchange', 'timeupdate']
    events.forEach((event) =>
      element.addEventListener(event, updatePlaybackState),
    )
    updatePlaybackState()

    return () => {
      events.forEach((event) =>
        element.removeEventListener(event, updatePlaybackState),
      )
      MEDIA_SESSION_ACTIONS.forEach((action) =>
        setActionHandler(mediaSession, action, null),
      )
      mediaSession.metadata = null
      mediaSession.playbackState = 'none'
    }
  }, [revision, allowPlayback])

  return (
    <MediaSessionContext.Provider value={value}>
      {children}
    </MediaSessionContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useMediaSessionSource = ({
  source,
  element,
  metadata,
  active = false,
  audioContext,
  onPrevious,
  onNext,
  onSeekBackward,
  onSeekForward,
  onSleepExpire,
  onUserPlayback,
}) => {
  const coordinator = useContext(MediaSessionContext)
  const idRef = useRef(null)
  if (!idRef.current) idRef.current = Symbol(source || 'media')

  const descriptor = useMemo(
    () => ({
      element,
      metadata,
      audioContext,
      onPrevious,
      onNext,
      onSeekBackward,
      onSeekForward,
      onSleepExpire,
      onUserPlayback,
    }),
    [
      audioContext,
      element,
      metadata,
      onNext,
      onPrevious,
      onSeekBackward,
      onSeekForward,
      onSleepExpire,
      onUserPlayback,
    ],
  )

  useEffect(() => {
    if (!coordinator || !element) return undefined
    return coordinator.register(idRef.current, descriptor)
    // Descriptor updates are handled below. Re-registering here would clear
    // active ownership every time metadata or an inline command callback
    // changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [coordinator, element])

  useEffect(() => {
    if (!coordinator || !element) return
    coordinator.update(idRef.current, descriptor)
  }, [coordinator, descriptor, element])

  useEffect(() => {
    if (!coordinator || !element) return
    if (active) coordinator.activate(idRef.current)
    else coordinator.deactivate(idRef.current)
  }, [active, coordinator, element])

  return coordinator
}

// eslint-disable-next-line react-refresh/only-export-components
export const useMediaSessionCoordinator = () => useContext(MediaSessionContext)

export default MediaSessionCoordinator
