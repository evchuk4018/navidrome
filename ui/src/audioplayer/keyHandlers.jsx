const keyHandlers = (
  audioInstance,
  playerState,
  onUserPlayback = () => {},
  queueController,
) => {
  const nextSong = () => {
    const idx = playerState.queue.findIndex(
      (item) => item.uuid === playerState.current.uuid,
    )
    return idx !== null ? playerState.queue[idx + 1] : null
  }

  const prevSong = () => {
    const idx = playerState.queue.findIndex(
      (item) => item.uuid === playerState.current.uuid,
    )
    return idx !== null ? playerState.queue[idx - 1] : null
  }

  return {
    TOGGLE_PLAY: (e) => {
      e.preventDefault()
      onUserPlayback()
      audioInstance && audioInstance.togglePlay()
    },
    VOL_UP: () =>
      (audioInstance.volume = Math.min(1, audioInstance.volume + 0.1)),
    VOL_DOWN: () =>
      (audioInstance.volume = Math.max(0, audioInstance.volume - 0.1)),
    PREV_SONG: (e) => {
      if (!e.metaKey && (queueController || prevSong())) {
        onUserPlayback()
        queueController
          ? queueController.previous()
          : audioInstance && audioInstance.playPrev()
      }
    },
    CURRENT_SONG: () => {
      window.location.href = `#/album/${playerState.current?.song.albumId}/show`
    },
    NEXT_SONG: (e) => {
      if (!e.metaKey && (queueController || nextSong())) {
        onUserPlayback()
        queueController
          ? queueController.next()
          : audioInstance && audioInstance.playNext()
      }
    },
  }
}

export default keyHandlers
