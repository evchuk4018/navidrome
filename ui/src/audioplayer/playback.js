const resumeAttempts = new WeakMap()
const unloadedPositions = new WeakMap()
const pausedReloads = new WeakMap()

const getSource = (audio) =>
  audio.getAttribute?.('src') || audio.src || audio.currentSrc || ''

const allowReloadPlayback = (audio) => pausedReloads.get(audio)?.()

const blockReloadPlayback = (audio, source) => {
  allowReloadPlayback(audio)
  const events = ['play', 'playing', 'canplay', 'loadstart']
  const guard = (event) => {
    if (getSource(audio) !== source) {
      allowReloadPlayback(audio)
      return
    }
    if (event.type === 'loadstart') return
    event.stopImmediatePropagation()
    if (event.type !== 'canplay' && !audio.paused) audio.pause()
  }
  events.forEach((event) => audio.addEventListener?.(event, guard, true))
  pausedReloads.set(audio, () => {
    events.forEach((event) => audio.removeEventListener?.(event, guard, true))
    pausedReloads.delete(audio)
  })
}

export const resumeContext = (context) => {
  if (!context || context.state === 'running') return
  if (typeof context.resume !== 'function') return
  try {
    const resumePromise = context.resume()
    if (resumePromise && typeof resumePromise.catch === 'function') {
      resumePromise.catch(() => {})
    }
  } catch {
    // A closed or interrupted context must not break the playback controls.
  }
}

const requestPlay = (audio, context, onSettled = () => {}) => {
  let playPromise
  try {
    // Keep play() in the user-gesture call stack on iOS. Resuming a Web Audio
    // context first can consume the activation that Safari requires here.
    playPromise = audio.play()
  } catch (error) {
    onSettled(error)
    resumeContext(context)
    return
  }
  resumeContext(context)

  if (playPromise && typeof playPromise.then === 'function') {
    // Safari may reject this promise when the player reloads a stalled source.
    // The music-player waiting/canplay handlers own that recovery; do not pause
    // the element and cancel it again from this catch handler.
    playPromise.then(() => onSettled(), onSettled)
  } else onSettled()
}

export const playAudio = (audio, context) => {
  if (!audio) return
  allowReloadPlayback(audio)
  if (!audio.paused) {
    resumeContext(context)
    return
  }
  requestPlay(audio, context)
}

export const cancelPlaybackRecovery = (audio) => {
  if (!audio) return false
  const attempt = resumeAttempts.get(audio)
  attempt?.cancel()
  unloadedPositions.delete(audio)
  allowReloadPlayback(audio)
  if (attempt && !audio.paused) audio.pause()
  return Boolean(attempt)
}

// Explicit Play is independent of the dependency's toggle and ready-state
// checks. Safari can evict a paused source while keeping its queue selected.
export const resumePlayback = (audio, context) => {
  if (!audio) return
  allowReloadPlayback(audio)
  const source = getSource(audio)
  const pending = resumeAttempts.get(audio)
  if (pending) {
    if (pending.source === source) {
      resumeContext(context)
      return
    }
    pending.cancel()
  }
  if (!audio.paused) {
    resumeContext(context)
    return
  }

  const remembered = unloadedPositions.get(audio)
  const currentTime = Number(audio.currentTime) || 0
  const position = audio.ended
    ? 0
    : Math.max(
        0,
        currentTime ||
          (remembered?.source === source ? remembered.position : 0),
      )
  const reload =
    (audio.networkState === 0 || audio.readyState === 0) &&
    typeof audio.load === 'function'
  let restorePending = reload
  let playSettled = false
  const listeners = []
  const attempt = {
    source,
    cancel: () => {
      listeners.forEach(([event, listener]) =>
        audio.removeEventListener?.(event, listener),
      )
      if (resumeAttempts.get(audio) === attempt) resumeAttempts.delete(audio)
    },
    pause: () => {
      const current = isCurrent()
      attempt.cancel()
      // The music engine's canplay handler otherwise starts a source that was
      // explicitly paused during loading. Only a fresh play intent releases it.
      if (current && reload) blockReloadPlayback(audio, source)
    },
  }
  const isCurrent = () =>
    resumeAttempts.get(audio) === attempt && getSource(audio) === source
  const restorePosition = () => {
    if (!isCurrent()) {
      attempt.cancel()
      return
    }
    if (!restorePending) return
    try {
      audio.currentTime = Number.isFinite(audio.duration)
        ? Math.min(position, Math.max(0, audio.duration))
        : position
      restorePending = false
      unloadedPositions.delete(audio)
      if (playSettled) attempt.cancel()
    } catch {
      // Some Safari sources become seekable only at canplay.
    }
  }
  const listen = (event, listener) => {
    listeners.push([event, listener])
    audio.addEventListener?.(event, listener)
  }
  resumeAttempts.set(audio, attempt)
  listen('pause', attempt.pause)
  listen('error', attempt.pause)
  listen('loadstart', () => {
    if (!isCurrent()) attempt.cancel()
  })
  if (reload) {
    unloadedPositions.set(audio, { source, position })
    listen('loadedmetadata', restorePosition)
    listen('canplay', restorePosition)
    try {
      audio.load()
    } catch {
      attempt.pause()
      return
    }
  } else if (audio.ended || remembered?.source === source) {
    try {
      audio.currentTime = position
      unloadedPositions.delete(audio)
    } catch {
      attempt.cancel()
      return
    }
  }

  requestPlay(audio, context, (error) => {
    playSettled = true
    // The dependency may reload the same source while handling waiting. Keep
    // its pending position restoration through that expected AbortError.
    if (!isCurrent()) attempt.cancel()
    else if (error && error.name !== 'AbortError') attempt.pause()
    else if (!restorePending) attempt.cancel()
  })
}

export const pauseAudio = (audio) => {
  // A pause during loading still needs its original position on the next Play.
  if (audio) resumeAttempts.get(audio)?.pause()
  if (audio && !audio.paused) {
    audio.pause()
  }
}

export const togglePlayback = (audio, context) => {
  if (!audio) return
  if (!audio.paused) {
    pauseAudio(audio)
    return
  }

  resumePlayback(audio, context)
}
