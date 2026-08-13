import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useInterval } from '../common'
import { useDispatch, useSelector } from 'react-redux'
import { ThemeProvider } from '@material-ui/core/styles'
import {
  createMuiTheme,
  useAuthState,
  useDataProvider,
  useTranslate,
} from 'react-admin'
import ReactGA from 'react-ga'
import { GlobalHotKeys } from 'react-hotkeys'
import ReactJkMusicPlayer from 'navidrome-music-player'
import 'navidrome-music-player/assets/index.css'
import useCurrentTheme from '../themes/useCurrentTheme'
import config from '../config'
import useStyle from './styles'
import AudioTitle from './AudioTitle'
import MiniPlayer from './MiniPlayer'
import {
  clearQueue,
  addTracks,
  currentPlaying,
  refreshQueue,
  setPlayMode,
  setTranscodingProfile,
  setVolume,
  syncQueue,
} from '../actions'
import PlayerToolbar from './PlayerToolbar'
import { sendNotification } from '../utils'
import subsonic from '../subsonic'
import locale from './locale'
import { keyMap } from '../hotkeys'
import keyHandlers from './keyHandlers'
import { calculateGain } from '../utils/calculateReplayGain'
import { detectBrowserProfile, decisionService } from '../transcode'
import configureMediaSessionTrackNavigation from './mediaSession'
import {
  radioSongs,
  refillPersonalRadio,
  sendRadioFeedback,
} from '../quickpick/provider'

const MINI_MODE = 'mini'
const FULL_MODE = 'full'

const Player = () => {
  const theme = useCurrentTheme()
  const translate = useTranslate()
  const playerTheme = theme.player?.theme || 'dark'
  const dataProvider = useDataProvider()
  const playerState = useSelector((state) => state.player)
  const dispatch = useDispatch()
  const [currentTrackId, setCurrentTrackId] = useState(null)
  const [heartbeatTrackId, setHeartbeatTrackId] = useState(null)
  const lastPositionMsRef = useRef(0)
  const currentTrackIdRef = useRef(null)
  const stoppedRef = useRef(false)
  const radioPlaybackRef = useRef(null)
  const [audioInstance, setAudioInstance] = useState(null)
  const [displayMode, setDisplayMode] = useState(MINI_MODE)
  const [miniProgress, setMiniProgress] = useState({
    currentTime: 0,
    duration: 0,
  })
  const isMobilePlayer =
    /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(
      navigator.userAgent,
    )

  const { authenticated } = useAuthState()

  // Keep a ref to playerState so the mount effect can read the latest value
  // without re-triggering on every queue/position change
  const playerStateRef = useRef(playerState)
  playerStateRef.current = playerState

  currentTrackIdRef.current = currentTrackId

  const appendRadioItems = useCallback(
    (response) => {
      const queued = new Set(
        playerStateRef.current.queue
          .map((item) => item.radioItemId)
          .filter(Boolean),
      )
      const songs = radioSongs(response)
      const ids = songs.ids.filter(
        (key) => !queued.has(songs.data[key].radioItemId),
      )
      if (ids.length) {
        dispatch(
          addTracks(
            Object.fromEntries(ids.map((key) => [key, songs.data[key]])),
            ids,
          ),
        )
      }
    },
    [dispatch],
  )

  const reportRadioFeedback = useCallback((playback, event) => {
    if (!playback?.sessionId || !playback?.itemId) return
    sendRadioFeedback(playback.sessionId, {
      itemId: playback.itemId,
      event,
      listenedMs: Math.floor(playback.listenedMs || 0),
      durationMs: Math.floor(playback.durationMs || 0),
    }).catch(() => {})
  }, [])

  useEffect(() => {
    if (playerState.queue.length === 0) {
      setDisplayMode(MINI_MODE)
      setMiniProgress({ currentTime: 0, duration: 0 })
    }
  }, [playerState.queue.length])

  useInterval(
    () => {
      if (heartbeatTrackId && !stoppedRef.current) {
        subsonic.reportPlayback(
          heartbeatTrackId,
          lastPositionMsRef.current,
          'playing',
        )
      }
    },
    heartbeatTrackId ? config.playbackReportIntervalMs : null,
  )

  // Detect browser codec profile and eagerly resolve transcode URLs for the
  // persisted queue once on mount (e.g. after a browser refresh)
  useEffect(() => {
    const profile = detectBrowserProfile()
    decisionService.setProfile(profile)
    dispatch(setTranscodingProfile(profile))

    const state = playerStateRef.current
    const currentIdx = state.savedPlayIndex || 0
    const trackIds = state.queue
      .slice(currentIdx, currentIdx + 4)
      .filter((item) => !item.isRadio && item.trackId)
      .map((item) => item.trackId)

    if (trackIds.length === 0) {
      dispatch(refreshQueue())
      return
    }

    Promise.allSettled(
      trackIds.map((id) =>
        decisionService.resolveStreamUrl(id).then((url) => [id, url]),
      ),
    ).then((results) => {
      const resolvedUrls = {}
      results.forEach((r) => {
        if (r.status === 'fulfilled') {
          resolvedUrls[r.value[0]] = r.value[1]
        }
      })
      dispatch(refreshQueue(resolvedUrls))
    })
  }, [dispatch])

  // Pre-fetch transcode decisions for next 2-3 songs when queue or position changes
  useEffect(() => {
    if (!playerState.queue.length) return

    const currentIdx = playerState.savedPlayIndex || 0
    const nextSongIds = playerState.queue
      .slice(currentIdx + 1, currentIdx + 4)
      .filter((item) => !item.isRadio)
      .map((item) => item.trackId)

    if (nextSongIds.length > 0) {
      decisionService.prefetchDecisions(nextSongIds)
    }
  }, [playerState.queue, playerState.savedPlayIndex])

  const visible = authenticated && playerState.queue.length > 0
  const isRadio = playerState.current?.isRadio || false
  const classes = useStyle({
    isRadio,
    visible,
    enableCoverAnimation: config.enableCoverAnimation,
  })
  const showNotifications = useSelector(
    (state) => state.settings.notifications || false,
  )
  const gainInfo = useSelector((state) => state.replayGain)
  const [context, setContext] = useState(null)
  const [gainNode, setGainNode] = useState(null)

  useEffect(() => {
    if (
      context === null &&
      audioInstance &&
      config.enableReplayGain &&
      'AudioContext' in window &&
      (gainInfo.gainMode === 'album' || gainInfo.gainMode === 'track')
    ) {
      const ctx = new AudioContext()
      // we need this to support radios in firefox
      audioInstance.crossOrigin = 'anonymous'
      const source = ctx.createMediaElementSource(audioInstance)
      const gain = ctx.createGain()

      source.connect(gain)
      gain.connect(ctx.destination)

      setContext(ctx)
      setGainNode(gain)
    }
  }, [audioInstance, context, gainInfo.gainMode])

  useEffect(() => {
    if (gainNode) {
      const current = playerState.current || {}
      const song = current.song || {}

      const numericGain = calculateGain(gainInfo, song)
      gainNode.gain.setValueAtTime(numericGain, context.currentTime)
    }
  }, [audioInstance, context, gainNode, playerState, gainInfo])

  useEffect(() => {
    const handleBeforeUnload = (e) => {
      if (playerState.current?.uuid && audioInstance && !audioInstance.paused) {
        e.preventDefault()
        e.returnValue = ''
      }
    }

    const handlePageHide = () => {
      if (currentTrackIdRef.current && !playerState.current?.isRadio) {
        stoppedRef.current = true
        try {
          subsonic.reportPlaybackKeepalive(
            currentTrackIdRef.current,
            lastPositionMsRef.current,
            'stopped',
          )
        } catch {
          // fetch/sendBeacon may throw; ignore
        }
      }
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    window.addEventListener('pagehide', handlePageHide)
    return () => {
      window.removeEventListener('beforeunload', handleBeforeUnload)
      window.removeEventListener('pagehide', handlePageHide)
    }
  }, [playerState, audioInstance])

  const handleModeChange = useCallback((mode) => {
    if (mode === MINI_MODE || mode === FULL_MODE) {
      setDisplayMode(mode)
    }
  }, [])

  const openFullPlayer = useCallback(() => {
    setDisplayMode(FULL_MODE)
  }, [])

  const defaultOptions = useMemo(
    () => ({
      theme: playerTheme,
      bounds: 'body',
      playMode: playerState.mode,
      mode: displayMode,
      loadAudioErrorPlayNext: false,
      autoPlayInitLoadPlayList: true,
      clearPriorAudioLists: false,
      showDestroy: true,
      showDownload: false,
      showLyric: true,
      showReload: false,
      toggleMode: true,
      glassBg: false,
      showThemeSwitch: false,
      showMediaSession: true,
      restartCurrentOnPrev: true,
      quietUpdate: true,
      defaultPosition: {
        top: 300,
        left: 120,
      },
      volumeFade: { fadeIn: 200, fadeOut: 200 },
      renderAudioTitle: (audioInfo, isMobile) => (
        <AudioTitle
          audioInfo={audioInfo}
          gainInfo={gainInfo}
          isMobile={isMobile}
        />
      ),
      locale: locale(translate),
      sortableOptions: { delay: 200, delayOnTouchOnly: true },
    }),
    [displayMode, gainInfo, playerTheme, translate, playerState.mode],
  )

  const options = useMemo(() => {
    const current = playerState.current || {}
    return {
      ...defaultOptions,
      audioLists: playerState.queue.map((item) => item),
      playIndex: playerState.playIndex,
      autoPlay:
        playerState.queue.length > 0 &&
        playerState.autoPlay !== false &&
        (playerState.clear || playerState.playIndex === 0),
      clearPriorAudioLists: playerState.clear,
      extendsContent: (
        <PlayerToolbar id={current.trackId} isRadio={current.isRadio} />
      ),
      defaultVolume: isMobilePlayer ? 1 : playerState.volume,
      showMediaSession: !current.isRadio,
    }
  }, [playerState, defaultOptions, isMobilePlayer])

  const onAudioListsChange = useCallback(
    (_, audioLists, audioInfo) => dispatch(syncQueue(audioInfo, audioLists)),
    [dispatch],
  )

  const onAudioProgress = useCallback(
    (info) => {
      if (info.ended) {
        document.title = 'Navidrome'
      }
      if (info.currentTime != null || info.duration != null) {
        setMiniProgress({
          currentTime: Number(info.currentTime) || 0,
          duration: Number(info.duration) || 0,
        })
      }
      if (!info.isRadio && info.currentTime != null) {
        lastPositionMsRef.current = Math.floor(info.currentTime * 1000)
      }
      const playback = radioPlaybackRef.current
      if (playback && playback.itemId === info.radioItemId) {
        const currentMS = Math.max(0, Number(info.currentTime || 0) * 1000)
        const delta = currentMS - playback.lastPositionMS
        if (delta > 0 && delta < 2500) playback.listenedMs += delta
        playback.lastPositionMS = currentMS
        playback.durationMs = Math.max(
          playback.durationMs,
          Number(info.duration || info.song?.duration || 0) * 1000,
        )
        const threshold = Math.min(30000, playback.durationMs / 5)
        if (
          info.radioItemType === 'discovery' &&
          !playback.thresholdSent &&
          threshold > 0 &&
          playback.listenedMs >= threshold
        ) {
          playback.thresholdSent = true
          reportRadioFeedback(playback, 'threshold_reached')
        }
      }
    },
    [reportRadioFeedback],
  )

  const onAudioVolumeChange = useCallback(
    // sqrt to compensate for the logarithmic volume
    (volume) => dispatch(setVolume(Math.sqrt(volume))),
    [dispatch],
  )

  const onAudioPlay = useCallback(
    (info) => {
      if (context && context.state !== 'running') {
        context.resume()
      }

      dispatch(currentPlaying(info))
      setMiniProgress({
        currentTime: Number(info.currentTime) || 0,
        duration: Number(info.duration) || 0,
      })
      if (info.duration) {
        const song = info.song
        document.title = `${song.title} - ${song.artist} - Navidrome`
        if (!info.isRadio) {
          const posMs = Math.floor(info.currentTime * 1000)
          lastPositionMsRef.current = posMs
          const isNewTrack = info.trackId !== currentTrackId
          if (isNewTrack) {
            subsonic
              .reportPlayback(info.trackId, posMs, 'starting')
              .then(() =>
                subsonic.reportPlayback(info.trackId, posMs, 'playing'),
              )
            setCurrentTrackId(info.trackId)
          } else {
            subsonic.reportPlayback(info.trackId, posMs, 'playing')
          }
          setHeartbeatTrackId(info.trackId)
        }
        if (config.gaTrackingId) {
          ReactGA.event({
            category: 'Player',
            action: 'Play song',
            label: `${song.title} - ${song.artist}`,
          })
        }
        if (showNotifications) {
          sendNotification(
            song.title,
            `${song.artist} - ${song.album}`,
            info.cover,
          )
        }
      }

      if (
        info.radioSessionId &&
        info.radioItemId &&
        radioPlaybackRef.current?.itemId !== info.radioItemId
      ) {
        const playback = {
          sessionId: info.radioSessionId,
          itemId: info.radioItemId,
          itemType: info.radioItemType,
          listenedMs: 0,
          lastPositionMS: Number(info.currentTime || 0) * 1000,
          durationMs: Number(info.duration || info.song?.duration || 0) * 1000,
          thresholdSent: false,
          completed: false,
        }
        radioPlaybackRef.current = playback
        reportRadioFeedback(playback, 'started')
        refillPersonalRadio(playback.sessionId)
          .then(appendRadioItems)
          .catch(() => {})
      }
    },
    [
      context,
      dispatch,
      showNotifications,
      currentTrackId,
      appendRadioItems,
      reportRadioFeedback,
    ],
  )

  const onAudioPlayTrackChange = useCallback(() => {
    const playback = radioPlaybackRef.current
    if (playback && !playback.completed) {
      reportRadioFeedback(playback, 'manual_skip')
    }
    radioPlaybackRef.current = null
    if (currentTrackId) {
      subsonic.reportPlayback(
        currentTrackId,
        lastPositionMsRef.current,
        'stopped',
      )
    }
    setHeartbeatTrackId(null)
    setCurrentTrackId(null)
  }, [currentTrackId, reportRadioFeedback])

  const onAudioPause = useCallback(
    (info) => {
      dispatch(currentPlaying(info))
      setMiniProgress({
        currentTime: Number(info.currentTime) || 0,
        duration: Number(info.duration) || 0,
      })
      if (!info.isRadio && currentTrackId) {
        const posMs = Math.floor(info.currentTime * 1000)
        lastPositionMsRef.current = posMs
        subsonic.reportPlayback(currentTrackId, posMs, 'paused')
      }
      setHeartbeatTrackId(null)
    },
    [dispatch, currentTrackId],
  )

  const onAudioEnded = useCallback(
    (currentPlayId, audioLists, info) => {
      if (currentTrackId && !info.isRadio) {
        const posMs = Math.floor((info.duration || 0) * 1000)
        subsonic.reportPlayback(currentTrackId, posMs, 'stopped')
      }
      setHeartbeatTrackId(null)
      setCurrentTrackId(null)
      const playback = radioPlaybackRef.current
      if (playback && playback.itemId === info.radioItemId) {
        playback.completed = true
        playback.listenedMs = Math.max(
          playback.listenedMs,
          Number(info.duration || 0) * 1000,
        )
        reportRadioFeedback(playback, 'completed')
      }
      dispatch(currentPlaying(info))
      setMiniProgress({
        currentTime: Number(info.currentTime) || 0,
        duration: Number(info.duration) || 0,
      })
      dataProvider
        .getOne('keepalive', { id: info.trackId })
        // eslint-disable-next-line no-console
        .catch((e) => console.log('Keepalive error:', e))
    },
    [dispatch, dataProvider, currentTrackId, reportRadioFeedback],
  )

  const onCoverClick = useCallback((mode, audioLists, audioInfo) => {
    if (mode === 'full' && audioInfo?.song?.albumId) {
      window.location.href = `#/album/${audioInfo.song.albumId}/show`
    }
  }, [])

  const onAudioError = useCallback(
    (error, currentPlayId, audioLists, audioInfo) => {
      // Invalidate all cached decisions — token may be stale
      decisionService.invalidateAll()

      // Pre-fetch decisions for upcoming songs with fresh tokens
      const currentIdx = playerState.queue.findIndex(
        (item) => item.uuid === currentPlayId,
      )
      if (currentIdx >= 0) {
        const nextSongIds = playerState.queue
          .slice(currentIdx + 1, currentIdx + 4)
          .filter((item) => !item.isRadio)
          .map((item) => item.trackId)
        if (nextSongIds.length > 0) {
          decisionService.prefetchDecisions(nextSongIds)
        }
      }
    },
    [playerState.queue],
  )

  const onBeforeDestroy = useCallback(() => {
    return new Promise((resolve, reject) => {
      if (currentTrackId && !playerStateRef.current?.current?.isRadio) {
        subsonic.reportPlayback(
          currentTrackId,
          lastPositionMsRef.current,
          'stopped',
        )
      }
      setHeartbeatTrackId(null)
      setCurrentTrackId(null)
      dispatch(clearQueue())
      reject()
    })
  }, [dispatch, currentTrackId])

  const queuedMiniTrack =
    playerState.queue[
      playerState.playIndex ?? playerState.savedPlayIndex ?? 0
    ] || playerState.queue[0]
  const miniTrack = playerState.current?.uuid
    ? playerState.current
    : queuedMiniTrack
  const miniPlayerVisible = visible && displayMode === MINI_MODE
  const miniIsPlaying =
    audioInstance?.paused != null
      ? !audioInstance.paused
      : playerState.current?.paused === false

  if (!visible) {
    document.title = 'Navidrome'
  }

  const handlers = useMemo(
    () => keyHandlers(audioInstance, playerState),
    [audioInstance, playerState],
  )

  useEffect(() => {
    if (isMobilePlayer && audioInstance) {
      audioInstance.volume = 1
    }
  }, [isMobilePlayer, audioInstance])

  useEffect(() => {
    if (!audioInstance || isRadio) return

    return configureMediaSessionTrackNavigation(audioInstance)
  }, [audioInstance, isRadio, playerState.queue])

  // Report every seek (including programmatic ones the library does not surface
  // via onAudioSeeked, e.g. restartCurrentOnPrev). Debounce coalesces drag
  // bursts into one report at the final position.
  useEffect(() => {
    if (!audioInstance) return
    let timer = null
    const flush = () => {
      timer = null
      if (
        !currentTrackIdRef.current ||
        playerStateRef.current?.current?.isRadio
      ) {
        return
      }
      const posMs = Math.floor((audioInstance.currentTime || 0) * 1000)
      const state = audioInstance.paused ? 'paused' : 'playing'
      subsonic.reportPlayback(currentTrackIdRef.current, posMs, state)
    }
    const handleSeeked = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(flush, 250)
    }
    audioInstance.addEventListener('seeked', handleSeeked)
    return () => {
      if (timer) clearTimeout(timer)
      audioInstance.removeEventListener('seeked', handleSeeked)
    }
  }, [audioInstance])

  return (
    <ThemeProvider theme={createMuiTheme(theme)}>
      {miniPlayerVisible && (
        <MiniPlayer
          track={miniTrack}
          audioInstance={audioInstance}
          currentTime={miniProgress.currentTime}
          duration={miniProgress.duration || miniTrack?.duration || 0}
          isPlaying={miniIsPlaying}
          onExpand={openFullPlayer}
          openLabel={translate('player.openText')}
          playLabel={translate('player.clickToPlayText')}
          pauseLabel={translate('player.clickToPauseText')}
        />
      )}
      <ReactJkMusicPlayer
        {...options}
        className={classes.player}
        onAudioListsChange={onAudioListsChange}
        onAudioVolumeChange={onAudioVolumeChange}
        onAudioProgress={onAudioProgress}
        onAudioPlay={onAudioPlay}
        onAudioPlayTrackChange={onAudioPlayTrackChange}
        onAudioPause={onAudioPause}
        onModeChange={handleModeChange}
        onPlayModeChange={(mode) => dispatch(setPlayMode(mode))}
        onAudioEnded={onAudioEnded}
        onCoverClick={onCoverClick}
        onAudioError={onAudioError}
        onBeforeDestroy={onBeforeDestroy}
        getAudioInstance={setAudioInstance}
      />
      <GlobalHotKeys handlers={handlers} keyMap={keyMap} allowChanges />
    </ThemeProvider>
  )
}

export { Player }
