import { useEffect, useState } from 'react'
import { playbackSession } from './controller'

export const usePlaybackOwnership = () => {
  const [, update] = useState(0)
  useEffect(
    () => playbackSession.subscribe(() => update((value) => value + 1)),
    [],
  )
  return playbackSession.view()
}
