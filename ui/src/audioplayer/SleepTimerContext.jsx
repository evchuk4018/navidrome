import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useMediaSessionCoordinator } from './MediaSessionCoordinator'

const SleepTimerContext = createContext(null)
const PRESETS = [15, 30, 60]
const DEFAULT_VALUE = {
  start: () => {},
  stop: () => {},
  remainingSeconds: 0,
  isActive: false,
}

export const SleepTimerProvider = ({ children }) => {
  const coordinator = useMediaSessionCoordinator()
  const deadline = useRef(0)
  const [remainingSeconds, setRemainingSeconds] = useState(0)

  const check = useCallback(() => {
    if (!deadline.current) return
    const remaining = Math.max(
      0,
      Math.ceil((deadline.current - Date.now()) / 1000),
    )
    if (!remaining) {
      // Clear synchronously: media-end events and a suspended tab's interval
      // can reach the deadline in the same turn.
      deadline.current = 0
      coordinator?.expireSleep?.()
    }
    setRemainingSeconds(remaining)
  }, [coordinator])

  const start = useCallback((minutes) => {
    if (!PRESETS.includes(minutes)) return
    deadline.current = Date.now() + minutes * 60 * 1000
    setRemainingSeconds(minutes * 60)
  }, [])

  const stop = useCallback(() => {
    deadline.current = 0
    setRemainingSeconds(0)
  }, [])

  useEffect(
    () => coordinator?.setSleepTimerCheck?.(check),
    [check, coordinator],
  )

  const isActive = remainingSeconds > 0
  useEffect(() => {
    if (!isActive) return undefined
    const timer = window.setInterval(check, 500)
    window.addEventListener('pageshow', check)
    document.addEventListener('visibilitychange', check)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('pageshow', check)
      document.removeEventListener('visibilitychange', check)
    }
  }, [check, isActive])

  const value = useMemo(
    () => ({ start, stop, remainingSeconds, isActive }),
    [isActive, remainingSeconds, start, stop],
  )
  return (
    <SleepTimerContext.Provider value={value}>
      {children}
    </SleepTimerContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useSleepTimer = () =>
  useContext(SleepTimerContext) || DEFAULT_VALUE
