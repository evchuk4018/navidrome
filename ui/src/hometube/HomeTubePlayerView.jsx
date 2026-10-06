import React, { useEffect, useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useMediaQuery, IconButton } from '@material-ui/core'
import FullscreenExitIcon from '@material-ui/icons/FullscreenExit'
import FullscreenIcon from '@material-ui/icons/Fullscreen'
import PlayerMobile from 'navidrome-music-player/es/components/PlayerMobile'
import PlayerDesktop from 'navidrome-music-player/es/components/PlayerDesktop'
import PlayerProgress from 'navidrome-music-player/es/components/PlayerProgress'
import PlayerDestroy from 'navidrome-music-player/es/components/PlayerDestroy'
import PlayerMode from 'navidrome-music-player/es/components/PlayerMode'
import AudioListsPanel from 'navidrome-music-player/es/components/AudioListsPanel'
import playerIcons from 'navidrome-music-player/es/components/PlayerIcons'
import { formatTime } from 'navidrome-music-player/es/utils'
import { useTranslate } from 'react-admin'
import AudioTitle from '../audioplayer/AudioTitle'
import PlayerToolbar from '../audioplayer/PlayerToolbar'
import useStyle from '../audioplayer/styles'
import locale from '../audioplayer/locale'
import { usePlaybackQueue } from '../audioplayer/PlaybackQueueContext'
import { setPlayMode } from '../actions'

const modes = ['order', 'orderLoop', 'singleLoop', 'shufflePlay']

const HomeTubePlayerView = ({
  video,
  active,
  expanded,
  artwork,
  element,
  position,
  duration,
  playing,
  ready,
  queue: upcoming,
  onClose,
  onToggle,
  onNext,
  onDismiss,
  onFullscreen,
  fullscreen,
}) => {
  const dispatch = useDispatch()
  const storedMode = useSelector((state) => state.player?.mode || 'order')
  const queue = usePlaybackQueue()
  const mobile = useMediaQuery('(max-width:768px) and (orientation:portrait)')
  const translate = useTranslate()
  const text = {
    ...locale(translate),
    clickToPlayText: 'Play',
    clickToPauseText: 'Pause',
    previousTrackText: 'Previous',
    nextTrackText: 'Next',
    toggleMiniModeText: 'Minimize HomeTube player',
    closeText: 'Close queue',
    playListsText: 'Queue',
    removeAudioListsText: 'Clear queue',
    clickToDeleteText: (name) => `Remove ${name}`,
    notContentText: 'No upcoming videos.',
  }
  const classes = useStyle({
    visible: true,
    enableCoverAnimation: false,
    isRadio: false,
  })
  const [queueOpen, setQueueOpen] = useState(false)
  const [volume, setVolume] = useState(1)
  useEffect(() => {
    const sync = () => setVolume(element?.volume ?? 1)
    sync()
    element?.addEventListener('volumechange', sync)
    return () => element?.removeEventListener('volumechange', sync)
  }, [element])
  useEffect(() => {
    if (!expanded) setQueueOpen(false)
  }, [expanded])
  const closeQueue = () => {
    setQueueOpen(false)
  }
  const rows =
    queue?.queue.map((item) => ({ ...item, __PLAYER_KEY__: item.uuid })) ||
    upcoming.map((entry) => ({
      name: entry.video.title,
      singer: entry.video.channelName,
      __PLAYER_KEY__: entry.video.id,
    }))
  const mode = queue?.mode || storedMode
  const modeControl = (
    <PlayerMode
      title={text.playModeText[mode]}
      icon={
        playerIcons[
          mode === 'singleLoop'
            ? 'loop'
            : mode === 'orderLoop'
              ? 'orderLoop'
              : mode === 'shufflePlay'
                ? 'shuffle'
                : 'order'
        ]
      }
      onClick={() =>
        dispatch(setPlayMode(modes[(modes.indexOf(mode) + 1) % modes.length]))
      }
    />
  )
  const progress = (
    <PlayerProgress
      label="Video position"
      duration={duration}
      currentTime={position}
      disabled={!ready || !duration}
      onChange={(value) => {
        if (element) element.currentTime = value
      }}
    />
  )
  const openQueue = () => {
    setQueueOpen(true)
  }
  const toolbar = (
    <PlayerToolbar
      video={video}
      active={active}
      layout={mobile ? 'mobile' : 'desktop'}
      extraActions={
        <IconButton
          aria-label={fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}
          onClick={onFullscreen}
          size="small"
        >
          {fullscreen ? <FullscreenExitIcon /> : <FullscreenIcon />}
        </IconButton>
      }
    />
  )
  const previous = () => {
    if (queue) queue.previous()
    else if (element) element.currentTime = 0
  }
  const shared = {
    playing,
    loading: !ready,
    cover: video.thumbnailUrl,
    name: video.title,
    singer: video.channelName,
    renderAudioTitle: () => (
      <AudioTitle audioInfo={{ video }} gainInfo={{}} isMobile={mobile} />
    ),
    onCoverClick: () => {},
    artwork,
    glassBg: false,
    autoHiddenCover: false,
    shouldShowPlayIcon: !playing,
    icon: playerIcons,
    locale: text,
    toggleMode: true,
    onClose,
    extendsContent: toolbar,
    openAudioListsPanel: openQueue,
    previousDisabled: !ready,
    nextDisabled: !rows.length,
  }
  return (
    <div
      className={`${classes.player} react-jinke-music-player-main dark-theme hometube-shared-player`}
      style={{ display: expanded ? 'block' : 'none' }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.stopPropagation()
          queueOpen ? closeQueue() : onClose?.()
        }
      }}
    >
      {mobile ? (
        <PlayerMobile
          {...shared}
          duration={formatTime(duration)}
          currentTime={formatTime(position)}
          progressBar={progress}
          playMode={modeControl}
          onPlay={onToggle}
          audioPrevPlay={previous}
          audioNextPlay={queue?.next || onNext}
        />
      ) : (
        <PlayerDesktop
          {...shared}
          audioTitle={video.title}
          formattedCurrentTime={formatTime(position)}
          formattedAudioDuration={formatTime(duration)}
          ProgressBar={progress}
          showPlay
          onPlayPrevAudio={previous}
          onTogglePlay={onToggle}
          onPlayNextAudio={queue?.next || onNext}
          PlayModeComponent={modeControl}
          DestroyComponent={
            <PlayerDestroy
              title={text.destroyText}
              icon={playerIcons.destroy}
              onClick={() => {
                if (queue) queue.clear()
                else {
                  element?.pause?.()
                  onClose?.()
                }
              }}
            />
          }
          audioLists={rows}
          onHidePanel={onClose}
          soundValue={volume}
          onResetVolume={() => {
            if (element) element.volume = 1
          }}
          onAudioMute={() => {
            if (element) element.volume = 0
          }}
          onAudioSoundChange={(value) => {
            if (element) element.volume = value
          }}
        />
      )}
      {queueOpen && (
        <div>
          <AudioListsPanel
            audioLists={rows}
            playId={queue?.selected?.uuid || video.id}
            playing={playing}
            loading={!ready}
            visible
            panelToggleAnimate={{ show: true }}
            isMobile={mobile}
            icon={playerIcons}
            locale={text}
            remove
            onCancel={closeQueue}
            onReorder={queue?.reorder}
            onPlay={(id) => {
              if (queue) queue.select(id)
              else
                onNext?.(upcoming.find((entry) => entry.video.id === id)?.video)
              closeQueue()
            }}
            onDelete={(id) => (event) => {
              event.stopPropagation()
              if (queue) queue.remove(id)
              else if (id) onDismiss?.(id)
            }}
          />
        </div>
      )}
    </div>
  )
}

export default HomeTubePlayerView
