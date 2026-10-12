import { v4 as uuidv4 } from 'uuid'
import httpClient, { clientUniqueId } from '../dataProvider/httpClient'
import { REST_URL } from '../consts'
import {
  networkSnapshot,
  readPlaybackCache,
  snapshotPlayer,
  structureKey,
  writePlaybackCache,
} from './codec'

const readUser = () => localStorage.getItem('userId') || ''
const deviceLabel = () => {
  const ua = navigator.userAgent || ''
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /Firefox/.test(ua)
      ? 'Firefox'
      : /Chrome/.test(ua)
        ? 'Chrome'
        : /Safari/.test(ua)
          ? 'Safari'
          : 'Browser'
  const device = /iPhone/.test(ua)
    ? 'iPhone'
    : /iPad/.test(ua)
      ? 'iPad'
      : /Android/.test(ua)
        ? 'Android'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Mac/.test(ua)
            ? 'Mac'
            : 'this device'
  return `${browser} on ${device}`
}

export const createPlaybackSessionController = ({
  send = (path, options) =>
    httpClient(`${REST_URL}/playback-session${path}`, options).then(
      ({ json }) => json,
    ),
  clientId = clientUniqueId,
  getUser = readUser,
  label = deviceLabel,
} = {}) => {
  let store,
    user = '',
    remote = null,
    epoch = 0,
    sequence = 0
  let allowed = false,
    lost = false,
    online = true,
    error = '',
    claiming = null
  let generation = 0,
    claimIntent = 0,
    pending = null,
    saving = false
  let savedStructure = '',
    progress = {},
    lastProgressSave = 0
  let pendingRestore = null
  let restoring = false,
    wanted = false,
    timer,
    unsubscribe
  const listeners = new Set()
  let hooks = {}
  const emit = () => listeners.forEach((listener) => listener())
  const snapshot = () =>
    snapshotPlayer(store?.getState().player || {}, progress)
  const view = () => ({
    remote,
    allowed: !store || allowed,
    wantsPlayback: !store || (wanted && allowed),
    lost,
    online,
    error,
    claiming: Boolean(claiming),
    epoch,
  })
  const valid = (identity, version) =>
    user === identity && generation === version && getUser() === identity
  const owner = () =>
    Boolean(user && epoch && remote?.ownerClientId === clientId && !lost)
  const request = async (path, options = {}) => {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 8000)
    try {
      return await send(path, { ...options, signal: controller.signal })
    } finally {
      clearTimeout(timeout)
    }
  }
  const stop = () => {
    allowed = false
    wanted = false
    lost = true
    pending = null
    progress = {}
    pendingRestore = null
    restoring = false
    hooks.pause?.()
    emit()
  }
  const accept = (session) => {
    if (remote && session && session.revision < remote.revision) return
    remote = session
    if (
      epoch &&
      (!session ||
        session.ownerClientId !== clientId ||
        session.epoch !== epoch)
    )
      stop()
    emit()
  }
  const reset = () => {
    generation++
    claimIntent++
    const incomingUser = getUser()
    user = ''
    epoch = 0
    sequence = 0
    remote = null
    allowed = false
    wanted = false
    lost = false
    claiming = null
    pending = null
    progress = {}
    savedStructure = ''
    pendingRestore = null
    restoring = false
    hooks.pause?.()
    store?.dispatch({ type: 'PLAYER_RESET_PLAYBACK_USER' })
    user = incomingUser
    const cached = readPlaybackCache(user)
    if (cached)
      store?.dispatch({
        type: 'PLAYER_RESTORE_SESSION',
        snapshot: cached,
        play: false,
      })
    emit()
  }
  const refresh = async () => {
    if (!store) return
    if (getUser() !== user) {
      reset()
    }
    if (!user) return
    const identity = user,
      version = generation
    try {
      const data = await request('?summary=true')
      if (!valid(identity, version)) return
      online = true
      accept(data.session)
      if (owner() && allowed) void checkpoint()
      else if (allowed && !claiming) stop()
    } catch {
      if (valid(identity, version)) {
        online = false
        emit()
      }
    }
  }
  const drain = async (keepalive = false) => {
    if (saving || !pending || !owner() || !allowed || restoring) return
    const next = pending
    pending = null
    saving = true
    const identity = user,
      version = generation,
      ownEpoch = epoch
    const shape = structureKey(next)
    const structural = shape !== savedStructure
    const body = {
      epoch: ownEpoch,
      sequence: ++sequence,
      ...(structural
        ? { snapshot: networkSnapshot(next) }
        : {
            selectedEntryId: next.selectedEntryId,
            positionMs: next.positionMs,
            state: next.state,
          }),
    }
    try {
      const encoded = JSON.stringify(body)
      const data = await request('', {
        method: 'PATCH',
        body: encoded,
        keepalive: keepalive && new Blob([encoded]).size < 60000,
      })
      if (!valid(identity, version) || epoch !== ownEpoch) return
      online = true
      if (
        data.session?.ownerClientId !== clientId ||
        data.session?.epoch !== ownEpoch
      ) {
        accept(data.session)
        return
      }
      savedStructure = shape
      accept(data.session)
    } catch (failure) {
      if (!valid(identity, version) || epoch !== ownEpoch) return
      if (failure.status === 409) {
        stop()
        await refresh()
      } else {
        online = false
        if (!pending) pending = next
      }
    } finally {
      if (valid(identity, version)) {
        saving = false
        emit()
        if (online && pending && owner() && allowed) void drain()
      } else saving = false
    }
  }
  const checkpoint = async (keepalive = false) => {
    if (!store || !user || getUser() !== user) return
    const current = snapshot()
    writePlaybackCache(user, current)
    if (!owner() || !allowed || restoring) return
    pending = current
    await drain(keepalive)
  }
  const claim = async (intent, actionVersion) => {
    const identity = user,
      version = generation
    const requestId = uuidv4()
    try {
      const latest = await request('?summary=true')
      if (!valid(identity, version) || actionVersion !== claimIntent)
        return false
      const before = latest.session
      const body = {
        intent,
        requestId,
        expectedEpoch: before?.epoch || 0,
        deviceLabel: label(),
        ...(intent === 'play' && { snapshot: networkSnapshot(snapshot()) }),
      }
      let result
      try {
        result = await request('/claim', {
          method: 'POST',
          body: JSON.stringify(body),
        })
      } catch (failure) {
        // A timed-out request may have committed. Read the receipt; never resend
        // a takeover that could reclaim ownership from a newer explicit action.
        if (failure.status) throw failure
        result = await request('')
        if (
          result.session?.lastClaimRequestId !== requestId ||
          result.session?.ownerClientId !== clientId
        )
          throw Object.assign(new Error('Playback ownership changed'), {
            status: 409,
          })
      }
      if (!valid(identity, version) || actionVersion !== claimIntent)
        return false
      const session = result.session
      epoch = session.epoch
      sequence = session.sequence || 0
      wanted = true
      remote = session
      allowed = true
      lost = false
      online = true
      error = ''
      if (intent === 'resume') {
        restoring = true
        pendingRestore = {
          entryId: session.selectedEntryId,
          token: session.epoch,
        }
        progress = {
          entryId: session.selectedEntryId,
          positionMs: session.positionMs,
          state: 'paused',
        }
        savedStructure = structureKey(session)
        store.dispatch({
          type: 'PLAYER_RESTORE_SESSION',
          snapshot: session,
          play: true,
        })
      }
      emit()
      hooks.resume?.()
      void checkpoint()
      return true
    } catch (failure) {
      if (!valid(identity, version) || actionVersion !== claimIntent)
        return false
      if (failure.status) {
        error =
          failure.status === 409
            ? 'Playback moved. Select Resume here or Play again.'
            : 'Unable to save this playback session. Please try again.'
        stop()
        await refresh()
      } else {
        // A fresh explicit play can continue locally through a network outage.
        // It is never uploaded on reconnect without checking existing ownership.
        online = false
        allowed = intent === 'play'
        error =
          intent === 'resume' ? 'Connect to your server to resume here.' : ''
        emit()
        if (allowed) hooks.resume?.()
      }
      return false
    }
  }
  const explicitPlay = () => {
    if (!store || !getUser()) return Promise.resolve(true)
    if (getUser() !== user) reset()
    if (owner()) {
      wanted = true
      allowed = true
      lost = false
      error = ''
      emit()
      void checkpoint()
      return Promise.resolve(true)
    }
    if (claiming) return claiming
    allowed = false
    wanted = true
    lost = false
    error = ''
    const actionVersion = ++claimIntent
    claiming = Promise.resolve()
      .then(() => claim('play', actionVersion))
      .finally(() => {
        if (actionVersion === claimIntent) {
          claiming = null
          emit()
        }
      })
    emit()
    return claiming
  }
  const resumeHere = () => {
    if (claiming || !store || !user) return Promise.resolve(false)
    hooks.pause?.()
    allowed = false
    wanted = true
    lost = false
    error = ''
    const actionVersion = ++claimIntent
    claiming = claim('resume', actionVersion).finally(() => {
      if (actionVersion === claimIntent) {
        claiming = null
        emit()
      }
    })
    emit()
    return claiming
  }
  const updateProgress = (entryId, positionMs, state, immediate = false) => {
    const player = store?.getState().player
    const selected =
      player?.queue?.[
        player.playIndex ?? Math.max(0, player.savedPlayIndex || 0)
      ]
    if (store && selected?.uuid !== entryId) return
    if (restoring && pendingRestore?.entryId === entryId) return
    progress = {
      entryId,
      positionMs: Math.max(0, Math.floor(positionMs || 0)),
      state,
    }
    if (allowed && state === 'paused') wanted = false
    if (!store || !user) return
    writePlaybackCache(user, snapshot())
    if (
      immediate ||
      state !== 'playing' ||
      Date.now() - lastProgressSave >= 5000
    ) {
      lastProgressSave = Date.now()
      void checkpoint()
    }
  }
  const flush = () => {
    void checkpoint(true)
  }
  const start = (nextStore) => {
    store = nextStore
    user = getUser()
    let previous = structureKey(snapshot())
    unsubscribe = store.subscribe(() => {
      const state = store.getState()
      const event = state.playbackSessionEvent
      if (event && event !== lastEvent) {
        lastEvent = event
        if (
          (!event.userId || event.userId === user) &&
          epoch &&
          (event.ownerClientId !== clientId || event.epoch !== epoch)
        )
          stop()
        if (!event.userId || event.userId === user) void refresh()
      }
      const current = snapshot(),
        shape = structureKey(current)
      if (
        pendingRestore &&
        current.selectedEntryId !== pendingRestore.entryId
      ) {
        pendingRestore = null
        restoring = false
      }
      if (shape !== previous) {
        previous = shape
        void checkpoint()
      }
    })
    timer = setInterval(() => {
      void refresh()
    }, 15000)
    window.addEventListener('focus', refresh)
    window.addEventListener('online', refresh)
    window.addEventListener('pageshow', refresh)
    window.addEventListener('pagehide', flush)
    document.addEventListener('visibilitychange', visibility)
    void refresh()
  }
  let lastEvent
  const visibility = () => (document.hidden ? flush() : void refresh())
  return {
    start,
    refresh,
    checkpoint,
    explicitPlay,
    resumeHere,
    updateProgress,
    flush,
    isAllowed: () => !store || allowed,
    canWrite: () => !store || (owner() && allowed),
    wantsPlayback: () => !store || (wanted && allowed),
    isRestoring: () => restoring,
    restoreComplete: (entryId, positionMs, token) => {
      if (
        !pendingRestore ||
        pendingRestore.entryId !== entryId ||
        pendingRestore.token !== token ||
        !allowed ||
        !owner()
      )
        return
      pendingRestore = null
      restoring = false
      progress = { entryId, positionMs, state: 'paused' }
      void checkpoint()
    },
    playRejected: () => {
      wanted = false
      emit()
    },
    identityChanged: () => {
      reset()
    },
    epoch: () => (owner() ? epoch : 0),
    view,
    snapshot,
    selected: () => {
      const player = store?.getState().player
      return player?.queue?.[
        player.playIndex ?? Math.max(0, player.savedPlayIndex || 0)
      ]
    },
    setHooks: (next) => {
      hooks = next
      return () => {
        if (hooks === next) hooks = {}
      }
    },
    subscribe: (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    stop: () => {
      generation++
      claimIntent++
      allowed = false
      pending = null
      hooks.pause?.()
      clearInterval(timer)
      unsubscribe?.()
      window.removeEventListener('focus', refresh)
      window.removeEventListener('online', refresh)
      window.removeEventListener('pageshow', refresh)
      window.removeEventListener('pagehide', flush)
      document.removeEventListener('visibilitychange', visibility)
      store = null
      emit()
    },
  }
}

export const playbackSession = createPlaybackSessionController()

export const playbackSessionMiddleware = () => (next) => (action) => {
  const explicit =
    [
      'PLAYER_PLAY_TRACKS',
      'PLAYER_SET_TRACK',
      'PLAYER_REQUEST_SEARCH_PLAY',
    ].includes(action.type) ||
    (action.type === 'PLAYER_QUEUE_SELECT' && !action.automatic)
  if (explicit) void playbackSession.explicitPlay()
  return next(action)
}
