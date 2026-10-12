import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { cancelPlaybackRecovery, pauseAudio, resumePlayback } from './playback'
import { playbackSession } from '../playbacksession/controller'

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
  const installedHandlers = useRef(new Map())
  const restoredPositions = useRef(new WeakMap())
  const resumeSelectedRef = useRef(() => {})
  const [revision, setRevision] = useState(0)
  const redraw = useCallback(() => setRevision((revision) => revision + 1), [])

  const register = useCallback(
    (id, descriptor) => {
      registrations.current.set(id, descriptor)
      const guard = (event) => {
        sleepTimerCheck.current?.()
        const selected = playbackSession.selected()
        const wrongEntry =
          selected &&
          descriptor.source &&
          ((selected.source === 'hometube' ? 'hometube' : 'music') !==
            descriptor.source ||
            (descriptor.element.ndPlaybackEntryId &&
              descriptor.element.ndPlaybackEntryId !== selected.uuid))
        if (
          !sleepBlocked.current &&
          playbackSession.isAllowed() &&
          !wrongEntry &&
          !selected?.unavailable
        ) {
          if (event?.type === 'play') resumeSelectedRef.current()
          if (!playbackSession.isRestoring()) return
        }
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
      const progress = (event) => {
        const current = registrations.current.get(id) || descriptor
        if (!playbackSession.isAllowed()) return
        if (event?.type === 'loadedmetadata' || event?.type === 'canplay')
          resumeSelectedRef.current()
        const element = current.element
        const entryId = element.ndPlaybackEntryId || current.entryId
        if (entryId)
          playbackSession.updateProgress(
            entryId,
            (element.currentTime || 0) * 1000,
            element.paused || element.ended ? 'paused' : 'playing',
            event?.type === 'seeked',
          )
      }
      const progressEvents = [
        'timeupdate',
        'pause',
        'seeked',
        'play',
        'loadedmetadata',
        'canplay',
      ]
      progressEvents.forEach((event) =>
        descriptor.element.addEventListener(event, progress),
      )
      return () => {
        events.forEach((event) =>
          descriptor.element.removeEventListener(event, guard, true),
        )
        progressEvents.forEach((event) =>
          descriptor.element.removeEventListener(event, progress),
        )
        if (cancelPlaybackRecovery(descriptor.element))
          pauseAudio(descriptor.element)
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
      if (activeId.current !== id) {
        const previous = registrations.current.get(activeId.current)
        if (cancelPlaybackRecovery(previous?.element))
          pauseAudio(previous.element)
      }
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
    void playbackSession.explicitPlay()
  }, [])

  const isPlaybackBlocked = useCallback(() => {
    sleepTimerCheck.current?.()
    return sleepBlocked.current || !playbackSession.isAllowed()
  }, [])

  const resumeSelected = useCallback(() => {
    if (!playbackSession.wantsPlayback() || sleepBlocked.current) return
    const selected = playbackSession.selected()
    if (!selected || selected.unavailable || selected.radioPending) return
    const source = selected.source === 'hometube' ? 'hometube' : 'music'
    const descriptor = [...registrations.current.values()].find(
      (item) =>
        item.source === source &&
        (item.element.ndPlaybackEntryId || item.entryId) === selected.uuid,
    )
    if (!descriptor?.element || descriptor.element.readyState === 0) return
    const element = descriptor.element
    const restoreKey = `${selected.uuid}:${selected.resumeToken}`
    if (
      selected.resumePositionMs != null &&
      !selected.isRadio &&
      restoredPositions.current.get(element) !== restoreKey &&
      element.ndResumeKey !== restoreKey
    ) {
      if (!Number.isFinite(element.duration) || element.duration <= 0) return
      try {
        element.currentTime = Math.min(
          selected.resumePositionMs / 1000,
          element.duration,
        )
        restoredPositions.current.set(element, restoreKey)
        element.ndResumeKey = restoreKey
      } catch {
        return
      }
    }
    playbackSession.restoreComplete(
      selected.uuid,
      (element.currentTime || 0) * 1000,
      selected.resumeToken,
    )
    resumePlayback(element, descriptor.audioContext, (error) => {
      if (error) playbackSession.playRejected()
    })
  }, [])
  resumeSelectedRef.current = resumeSelected

  useEffect(() => {
    const removeHooks = playbackSession.setHooks({
      pause: () =>
        registrations.current.forEach((descriptor) => {
          cancelPlaybackRecovery(descriptor.element)
          pauseAudio(descriptor.element)
          descriptor.onOwnershipLost?.()
        }),
      resume: resumeSelected,
    })
    const removeListener = playbackSession.subscribe(redraw)
    return () => {
      removeHooks()
      removeListener()
    }
  }, [redraw, resumeSelected])

  useEffect(() => {
    resumeSelected()
  }, [revision, resumeSelected])

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

  const actions = useMemo(() => {
    const selected = () => registrations.current.get(activeId.current)
    const userPlayback = (command) => () => {
      const descriptor = selected()
      if (!descriptor?.element) return
      allowPlayback()
      descriptor.onUserPlayback?.()
      command(descriptor)
    }
    return {
      play: userPlayback(({ element, audioContext }) =>
        resumePlayback(element, audioContext),
      ),
      pause: () => pauseAudio(selected()?.element),
      previoustrack: userPlayback((descriptor) => {
        cancelPlaybackRecovery(descriptor.element)
        descriptor.onPrevious?.()
      }),
      nexttrack: userPlayback((descriptor) => {
        cancelPlaybackRecovery(descriptor.element)
        descriptor.onNext?.()
      }),
      seekto: (details = {}) => {
        const element = selected()?.element
        if (element && details.seekTime != null) {
          cancelPlaybackRecovery(element)
          element.currentTime = details.seekTime
        }
      },
    }
  }, [allowPlayback])

  const updatePlaybackState = useCallback(() => {
    const mediaSession = getMediaSession()
    const element = registrations.current.get(activeId.current)?.element
    if (!mediaSession || !element) return
    mediaSession.playbackState =
      element.paused || element.ended ? 'paused' : 'playing'
    updatePositionState(mediaSession, element)
  }, [])

  // Descriptor/metadata updates must not tear down the paused OS session.
  // The installed dispatchers always resolve the latest selected source.
  useEffect(() => {
    const mediaSession = getMediaSession()
    if (!mediaSession?.setActionHandler) return
    const descriptor = registrations.current.get(activeId.current)
    const element = descriptor?.element
    MEDIA_SESSION_ACTIONS.forEach((action) => {
      const available =
        element &&
        (action !== 'previoustrack' || descriptor.onPrevious) &&
        (action !== 'nexttrack' || descriptor.onNext)
      const handler = available ? actions[action] || null : null
      if (installedHandlers.current.get(action) !== handler) {
        setActionHandler(mediaSession, action, handler)
        installedHandlers.current.set(action, handler)
      }
    })
    mediaSession.metadata = element ? makeMetadata(descriptor.metadata) : null
    if (element) updatePlaybackState()
    else {
      mediaSession.playbackState = 'none'
      try {
        mediaSession.setPositionState()
      } catch {
        // Position state is not available in every browser.
      }
    }
  }, [revision, actions, updatePlaybackState])

  const selectedElement = registrations.current.get(activeId.current)?.element
  useEffect(() => {
    if (!selectedElement) return undefined
    const update = () => {
      if (
        registrations.current.get(activeId.current)?.element === selectedElement
      )
        updatePlaybackState()
    }
    const events = [
      'play',
      'playing',
      'pause',
      'ended',
      'loadedmetadata',
      'durationchange',
      'timeupdate',
      'seeked',
      'ratechange',
    ]
    events.forEach((event) => selectedElement.addEventListener(event, update))
    return () => {
      events.forEach((event) =>
        selectedElement.removeEventListener(event, update),
      )
    }
  }, [selectedElement, updatePlaybackState])

  useEffect(
    () => () => {
      const mediaSession = getMediaSession()
      if (!mediaSession?.setActionHandler) return
      MEDIA_SESSION_ACTIONS.forEach((action) =>
        setActionHandler(mediaSession, action, null),
      )
      mediaSession.metadata = null
      mediaSession.playbackState = 'none'
    },
    [],
  )

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
  entryId,
  onOwnershipLost,
}) => {
  const coordinator = useContext(MediaSessionContext)
  const idRef = useRef(null)
  if (!idRef.current) idRef.current = Symbol(source || 'media')

  const descriptor = useMemo(
    () => ({
      source,
      entryId,
      onOwnershipLost,
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
      source,
      entryId,
      onOwnershipLost,
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
