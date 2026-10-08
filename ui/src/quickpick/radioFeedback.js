import { v4 as uuidv4 } from 'uuid'
import { radioErrorDetails, sendRadioFeedback } from './provider'

const retryDelays = [1000, 2000, 4000]
const retryableStatuses = new Set([500, 502, 503, 504])

const reportFailure = (details) => {
  // eslint-disable-next-line no-console
  console.warn('[personal-radio] feedback delivery failed', details)
}

// Each session retains its own order through track changes. The queue owns
// immutable event snapshots, so a lost response can safely resend the same ID.
export const createRadioFeedbackQueue = (
  send = sendRadioFeedback,
  warn = reportFailure,
) => {
  const sessions = new Map()
  let disposed = false

  const finish = (sessionId, session) => {
    if (disposed) return
    session.events.shift()
    session.attempts = 0
    if (session.events.length) deliver(sessionId, session)
    else sessions.delete(sessionId)
  }

  const deliver = (sessionId, session) => {
    if (disposed) return
    const feedback = session.events[0]
    const controller = new AbortController()
    session.controller = controller
    session.attempts++
    let timedOut = false
    // Abort plus a rejection keeps the queue bounded even if a transport does
    // not settle its promise after abort. Receipts cover an ambiguous commit.
    const timeout = new Promise((_, reject) => {
      session.timeout = setTimeout(() => {
        timedOut = true
        controller.abort()
        reject(new Error('Feedback request timed out'))
      }, 10000)
    })
    Promise.race([
      Promise.resolve().then(() => {
        if (disposed) return
        return send(sessionId, feedback, { signal: controller.signal })
      }),
      timeout,
    ])
      .then(() => {
        clearTimeout(session.timeout)
        session.controller = null
        finish(sessionId, session)
      })
      .catch((error) => {
        clearTimeout(session.timeout)
        session.controller = null
        if (disposed) return
        const retryable =
          timedOut || !error?.status || retryableStatuses.has(error.status)
        if (retryable && session.attempts <= retryDelays.length) {
          session.retry = setTimeout(
            () => deliver(sessionId, session),
            retryDelays[session.attempts - 1],
          )
          return
        }
        warn({
          sessionId,
          itemId: feedback.itemId,
          eventId: feedback.eventId,
          event: feedback.event,
          attempts: session.attempts,
          ...radioErrorDetails(error),
        })
        finish(sessionId, session)
      })
  }

  return {
    enqueue(sessionId, feedback) {
      if (disposed || !sessionId || !feedback.itemId) return
      const event = Object.freeze({ ...feedback, eventId: uuidv4() })
      let session = sessions.get(sessionId)
      if (!session) {
        session = { events: [], attempts: 0 }
        sessions.set(sessionId, session)
      }
      session.events.push(event)
      if (session.events.length === 1) deliver(sessionId, session)
    },
    dispose() {
      disposed = true
      for (const session of sessions.values()) {
        clearTimeout(session.timeout)
        clearTimeout(session.retry)
        session.controller?.abort()
      }
      sessions.clear()
    },
  }
}
