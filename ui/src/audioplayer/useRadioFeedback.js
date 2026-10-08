import { useCallback, useEffect, useRef } from 'react'
import { createRadioFeedbackQueue } from '../quickpick/radioFeedback'

export const useRadioFeedback = (authenticated) => {
  const queueRef = useRef(null)
  useEffect(() => {
    if (!authenticated) return
    const queue = createRadioFeedbackQueue()
    queueRef.current = queue
    return () => {
      queue.dispose()
      if (queueRef.current === queue) queueRef.current = null
    }
  }, [authenticated])

  return useCallback((playback, event) => {
    if (!playback?.sessionId || !playback?.itemId) return
    queueRef.current?.enqueue(playback.sessionId, {
      itemId: playback.itemId,
      event,
      listenedMs: Math.floor(playback.listenedMs || 0),
      durationMs: Math.floor(playback.durationMs || 0),
    })
  }, [])
}
