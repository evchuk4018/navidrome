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
  resumeContext(context)
  const playPromise = audio.play()
  if (playPromise && typeof playPromise.catch === 'function') {
    playPromise.catch(() => {
      audio.pause()
    })
  }
}

export const pauseAudio = (audio) => {
  if (audio && !audio.paused) {
    audio.pause()
  }
}

export const togglePlayback = (audio, context) => {
  if (!audio) return
  if (audio.paused) {
    playAudio(audio, context)
  } else {
    pauseAudio(audio)
  }
}
