const mediaSessionActions = [
  'seekbackward',
  'seekforward',
  'previoustrack',
  'nexttrack',
]

const getMediaSession = () => {
  if (typeof navigator === 'undefined') return null
  return navigator.mediaSession
}

const setMediaSessionActionHandler = (mediaSession, action, handler) => {
  try {
    mediaSession.setActionHandler(action, handler)
  } catch {
    // Browsers may reject actions they do not support.
  }
}

const configureMediaSessionTrackNavigation = (
  audioInstance,
  mediaSession = getMediaSession(),
) => {
  if (!audioInstance || !mediaSession?.setActionHandler) return

  setMediaSessionActionHandler(mediaSession, 'seekbackward', null)
  setMediaSessionActionHandler(mediaSession, 'seekforward', null)
  setMediaSessionActionHandler(mediaSession, 'previoustrack', () => {
    audioInstance.playPrev?.()
  })
  setMediaSessionActionHandler(mediaSession, 'nexttrack', () => {
    audioInstance.playNext?.()
  })

  return () => {
    mediaSessionActions.forEach((action) =>
      setMediaSessionActionHandler(mediaSession, action, null),
    )
  }
}

export default configureMediaSessionTrackNavigation
