import React, {
  createContext,
  useContext,
  useCallback,
  useEffect,
  useMemo,
  useRef,
} from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { clearQueue } from '../actions'
import { useMediaSessionCoordinator } from './MediaSessionCoordinator'

const QueueContext = createContext(null)

// The dependency's shuffle chooses a random row, then advances one when it picks
// the current row. Keep that behavior for a queue containing either source.
// eslint-disable-next-line react-refresh/only-export-components
export const navigationIndex = (
  queue,
  index,
  mode,
  next = true,
  automatic = false,
  random = Math.random,
) => {
  if (!queue.length) return -1
  if (automatic && mode === 'singleLoop') return index
  if (mode === 'shufflePlay') {
    const pick = Math.floor(random() * queue.length)
    return pick === index ? Math.min(pick + 1, queue.length - 1) : pick
  }
  const target = index + (next ? 1 : -1)
  if (automatic && mode !== 'orderLoop' && target >= queue.length) return -1
  return (target + queue.length) % queue.length
}

export const PlaybackQueueProvider = ({ children }) => {
  const state = useSelector((store) => store.player)
  const dispatch = useDispatch()
  const coordinator = useMediaSessionCoordinator()
  const stateRef = useRef(state)
  stateRef.current = state
  const engines = useRef({})
  const intent = useRef(null)
  const register = useCallback((source, engine) => {
    engines.current[source] = engine
    return () => {
      if (engines.current[source] === engine) delete engines.current[source]
    }
  }, [])
  const index = state.playIndex ?? Math.max(0, state.savedPlayIndex || 0)
  const selected = state.queue[index]
  const select = useCallback(
    (target, automatic = false) => {
      const current = stateRef.current
      let row =
        typeof target === 'number'
          ? target
          : current.queue.findIndex((item) => item.uuid === target)
      if (current.queue[row]?.radioPending) {
        const ready = current.queue.findIndex(
          (item, index) => index > row && !item.radioPending,
        )
        if (ready < 0) return
        row = ready
      }
      if (
        row < 0 ||
        !current.queue[row] ||
        (automatic && coordinator?.isPlaybackBlocked?.())
      )
        return
      if (!automatic) coordinator?.allowPlayback?.()
      dispatch({ type: 'PLAYER_QUEUE_SELECT', index: row, automatic })
    },
    [coordinator, dispatch],
  )
  useEffect(() => {
    if (intent.current === state.musicIntent) return
    intent.current = state.musicIntent
    if (!state.musicIntent || !selected || selected.source !== 'hometube')
      return
    if (state.selectionAutomatic && coordinator?.isPlaybackBlocked?.()) return
    engines.current.hometube?.select(selected, state.selectionAutomatic)
  }, [coordinator, selected, state.musicIntent, state.selectionAutomatic])
  const step = useCallback(
    (next, automatic = false) => {
      const current = stateRef.current
      const currentIndex =
        current.playIndex ?? Math.max(0, current.savedPlayIndex || 0)
      const target = navigationIndex(
        current.queue,
        currentIndex,
        current.mode || 'order',
        next,
        automatic,
      )
      if (target >= 0) {
        if (target === currentIndex) {
          if (automatic && coordinator?.isPlaybackBlocked?.()) return
          if (!automatic) coordinator?.allowPlayback?.()
          engines.current[
            current.queue[currentIndex]?.source || 'music'
          ]?.restart(automatic)
        } else select(target, automatic)
      }
    },
    [coordinator, select],
  )
  const previous = useCallback(() => {
    const current = stateRef.current
    const item =
      current.queue[
        current.playIndex ?? Math.max(0, current.savedPlayIndex || 0)
      ]
    const engine = engines.current[item?.source || 'music']
    if ((engine?.element?.currentTime || 0) > 1) engine.element.currentTime = 0
    else step(false)
  }, [step])
  const ended = useCallback(
    (uuid) => {
      const current = stateRef.current
      const item =
        current.queue[
          current.playIndex ?? Math.max(0, current.savedPlayIndex || 0)
        ]
      if (item?.uuid === uuid && !coordinator?.isPlaybackBlocked?.())
        step(true, true)
    },
    [coordinator, step],
  )
  const clear = useCallback(() => {
    Object.values(engines.current).forEach((engine) => engine.stop?.())
    dispatch(clearQueue())
  }, [dispatch])
  const remove = useCallback(
    (uuid) => {
      if (!uuid) return clear()
      const current = stateRef.current
      if (current.queue.length === 1) return clear()
      const selectedItem =
        current.queue[
          current.playIndex ?? Math.max(0, current.savedPlayIndex || 0)
        ]
      engines.current[selectedItem.source || 'music']?.dismiss?.(
        current.queue.find((row) => row.uuid === uuid),
      )
      dispatch({ type: 'PLAYER_QUEUE_REMOVE', uuid })
      if (selectedItem?.uuid === uuid)
        engines.current[selectedItem.source || 'music']?.stop?.()
    },
    [clear, dispatch],
  )
  const reorder = useCallback(
    (from, to) => {
      dispatch({ type: 'PLAYER_QUEUE_REORDER', from, to })
    },
    [dispatch],
  )
  const value = useMemo(
    () => ({
      dispatch,
      queue: state.queue,
      selected,
      index,
      mode: state.mode || 'order',
      register,
      select,
      previous,
      next: () => step(true),
      ended,
      clear,
      remove,
      reorder,
    }),
    [
      dispatch,
      state.queue,
      state.mode,
      selected,
      index,
      register,
      select,
      previous,
      step,
      ended,
      clear,
      remove,
      reorder,
    ],
  )
  return <QueueContext.Provider value={value}>{children}</QueueContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export const usePlaybackQueue = () => useContext(QueueContext)
