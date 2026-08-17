export const resumeContext = (context) => {
  if (!context || context.state === 'running') return
  if (typeof context.resume !== 'function') return
  const resumePromise = context.resume()
  if (resumePromise && typeof resumePromise.catch === 'function') {
    resumePromise.catch(() => {})
  }
}

export const playAudio = (audio, context) => {
  if (!audio || !audio.paused) return

  let playPromise
  try {
    // Keep play() in the user-gesture call stack on iOS. Resuming a Web Audio
    // context first can consume the activation that Safari requires here.
    playPromise = audio.play()
  } catch {
    return
  }
  resumeContext(context)

  if (playPromise && typeof playPromise.catch === 'function') {
    // Safari may reject this promise when the player reloads a stalled source.
    // The music-player waiting/canplay handlers own that recovery; do not pause
    // the element and cancel it again from this catch handler.
    playPromise.catch(() => {})
  }
}

export const pauseAudio = (audio) => {
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

  // The enhanced element's toggle keeps the dependency's loading and playing
  // state in sync. Use native playback only when iOS has evicted the source and
  // the dependency cannot restart it through its normal ready-state path.
  const sourceEvicted = audio.networkState === 0 || audio.readyState === 0
  if (!sourceEvicted && typeof audio.togglePlay === 'function') {
    try {
      audio.togglePlay()
      resumeContext(context)
      return
    } catch {
      // Fall through to the native recovery path.
    }
  }

  if (sourceEvicted && typeof audio.load === 'function') {
    audio.load()
  }
  playAudio(audio, context)
}
