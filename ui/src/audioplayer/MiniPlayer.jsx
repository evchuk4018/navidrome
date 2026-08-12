import React, { useCallback, useEffect, useRef, useState } from 'react'
import PropTypes from 'prop-types'
import { makeStyles } from '@material-ui/core/styles'
import PauseIcon from '@material-ui/icons/Pause'
import PlayArrowIcon from '@material-ui/icons/PlayArrow'
import KeyboardArrowUpIcon from '@material-ui/icons/KeyboardArrowUp'
import MusicNoteIcon from '@material-ui/icons/MusicNote'
import { formatDuration } from '../utils'

const useStyles = makeStyles((theme) => ({
  root: {
    position: 'fixed',
    right: 0,
    bottom: 0,
    left: 0,
    zIndex: 1001,
    display: 'flex',
    alignItems: 'center',
    boxSizing: 'border-box',
    height: 'calc(72px + env(safe-area-inset-bottom, 0px))',
    padding: theme.spacing(1),
    paddingBottom: 'calc(8px + env(safe-area-inset-bottom, 0px))',
    color: theme.palette.text.primary,
    backgroundColor: theme.palette.background.paper,
    borderTop: `1px solid ${theme.palette.divider}`,
    boxShadow: '0 -2px 12px rgba(0, 0, 0, 0.18)',
    touchAction: 'pan-y',
    '@media (max-width: 480px)': {
      height: 'calc(68px + env(safe-area-inset-bottom, 0px))',
    },
  },
  details: {
    display: 'flex',
    alignItems: 'center',
    flex: 1,
    minWidth: 0,
    height: '100%',
    padding: 0,
    margin: 0,
    overflow: 'hidden',
    color: 'inherit',
    fontFamily: 'inherit',
    textAlign: 'left',
    background: 'transparent',
    border: 0,
    cursor: 'pointer',
  },
  artwork: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    width: 52,
    height: 52,
    overflow: 'hidden',
    color: theme.palette.text.secondary,
    backgroundColor: theme.palette.action.hover,
    borderRadius: theme.shape.borderRadius,
  },
  artworkImage: {
    width: '100%',
    height: '100%',
    objectFit: 'cover',
  },
  metadata: {
    minWidth: 0,
    padding: `0 ${theme.spacing(1)}px`,
  },
  title: {
    display: 'block',
    overflow: 'hidden',
    fontSize: '0.9rem',
    fontWeight: 600,
    lineHeight: 1.25,
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  artist: {
    display: 'block',
    marginTop: 2,
    overflow: 'hidden',
    color: theme.palette.text.secondary,
    fontSize: '0.78rem',
    lineHeight: 1.25,
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  controls: {
    display: 'flex',
    alignItems: 'center',
    flexShrink: 0,
    gap: theme.spacing(0.5),
  },
  control: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 42,
    height: 42,
    padding: 0,
    color: 'inherit',
    background: 'transparent',
    border: 0,
    borderRadius: '50%',
    cursor: 'pointer',
    '&:hover': {
      backgroundColor: theme.palette.action.hover,
    },
    '&:focus-visible': {
      outline: `2px solid ${theme.palette.primary.main}`,
      outlineOffset: 2,
    },
    '&:disabled': {
      cursor: 'default',
      opacity: 0.55,
    },
  },
  toggle: {
    backgroundColor: theme.palette.action.hover,
  },
  icon: {
    fontSize: 28,
  },
  progressTrack: {
    position: 'absolute',
    right: 0,
    bottom: 0,
    left: 0,
    height: 2,
    overflow: 'hidden',
    backgroundColor: theme.palette.action.disabledBackground,
    pointerEvents: 'none',
  },
  progress: {
    height: '100%',
    backgroundColor: theme.palette.primary.main,
    transition: 'width 150ms linear',
  },
  duration: {
    position: 'absolute',
    width: 1,
    height: 1,
    padding: 0,
    margin: -1,
    overflow: 'hidden',
    clip: 'rect(0, 0, 0, 0)',
    whiteSpace: 'nowrap',
    border: 0,
  },
}))

const getTrackText = (track) => ({
  title: track.name || track.song?.title || track.title || 'Unknown track',
  artist: track.singer || track.song?.artist || track.artist || '',
  cover: track.cover || track.song?.cover || '',
})

const clamp = (value, min, max) => Math.min(Math.max(value, min), max)

const MiniPlayer = ({
  track,
  audioInstance,
  currentTime = 0,
  duration = 0,
  isPlaying = false,
  onExpand,
  openLabel = 'Open',
  playLabel = 'Click to play',
  pauseLabel = 'Click to pause',
}) => {
  const classes = useStyles()
  const [imageFailed, setImageFailed] = useState(false)
  const touchStartY = useRef(null)

  const { title, artist, cover } = track ? getTrackText(track) : {}
  const totalDuration = Math.max(Number(duration || track?.duration || 0), 0)
  const position = clamp(Number(currentTime) || 0, 0, totalDuration || Infinity)
  const progress = totalDuration
    ? clamp((position / totalDuration) * 100, 0, 100)
    : 0

  useEffect(() => {
    setImageFailed(false)
  }, [cover])

  const handleTogglePlay = useCallback(
    (event) => {
      event.stopPropagation()
      if (!audioInstance) return

      if (typeof audioInstance.togglePlay === 'function') {
        audioInstance.togglePlay()
      } else if (audioInstance.paused) {
        audioInstance.play()
      } else {
        audioInstance.pause()
      }
    },
    [audioInstance],
  )

  const handleTouchStart = useCallback((event) => {
    touchStartY.current = event.touches[0]?.clientY ?? null
  }, [])

  const handleTouchEnd = useCallback(
    (event) => {
      const startY = touchStartY.current
      const endY = event.changedTouches[0]?.clientY
      touchStartY.current = null

      if (startY != null && endY != null && startY - endY >= 40) {
        onExpand()
      }
    },
    [onExpand],
  )

  if (!track) return null

  return (
    <div
      className={classes.root}
      data-testid="mini-player"
      onTouchStart={handleTouchStart}
      onTouchEnd={handleTouchEnd}
    >
      <button
        type="button"
        className={classes.details}
        data-testid="mini-player-details"
        aria-label={`${openLabel}: ${title}`}
        onClick={onExpand}
      >
        <span className={classes.artwork}>
          {cover && !imageFailed ? (
            <img
              className={classes.artworkImage}
              src={cover}
              alt=""
              onError={() => setImageFailed(true)}
            />
          ) : (
            <MusicNoteIcon aria-hidden="true" />
          )}
        </span>
        <span className={classes.metadata}>
          <span className={classes.title}>{title}</span>
          {artist && <span className={classes.artist}>{artist}</span>}
        </span>
      </button>
      <div className={classes.controls}>
        <button
          type="button"
          className={`${classes.control} ${classes.toggle}`}
          data-testid="mini-player-toggle"
          aria-label={isPlaying ? pauseLabel : playLabel}
          title={isPlaying ? pauseLabel : playLabel}
          disabled={!audioInstance}
          onClick={handleTogglePlay}
        >
          {isPlaying ? (
            <PauseIcon className={classes.icon} aria-hidden="true" />
          ) : (
            <PlayArrowIcon className={classes.icon} aria-hidden="true" />
          )}
        </button>
        <button
          type="button"
          className={classes.control}
          data-testid="mini-player-expand"
          aria-label={openLabel}
          title={openLabel}
          onClick={onExpand}
        >
          <KeyboardArrowUpIcon className={classes.icon} aria-hidden="true" />
        </button>
      </div>
      <div className={classes.progressTrack} aria-hidden="true">
        <div
          className={classes.progress}
          data-testid="mini-player-progress"
          style={{ width: `${progress}%` }}
        />
      </div>
      <span className={classes.duration} aria-live="off">
        {formatDuration(position)} / {formatDuration(totalDuration)}
      </span>
    </div>
  )
}

MiniPlayer.propTypes = {
  track: PropTypes.object,
  audioInstance: PropTypes.object,
  currentTime: PropTypes.number,
  duration: PropTypes.number,
  isPlaying: PropTypes.bool,
  onExpand: PropTypes.func.isRequired,
  openLabel: PropTypes.string,
  playLabel: PropTypes.string,
  pauseLabel: PropTypes.string,
}

export default MiniPlayer
