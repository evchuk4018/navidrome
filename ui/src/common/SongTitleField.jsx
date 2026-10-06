import { makeStyles } from '@material-ui/core/styles'
import React from 'react'
import PropTypes from 'prop-types'
import clsx from 'clsx'
import { useSelector } from 'react-redux'
import { FunctionField } from 'react-admin'
import { useTheme } from '@material-ui/core/styles'
import PlayingLight from '../icons/playing-light.gif'
import PlayingDark from '../icons/playing-dark.gif'
import PausedLight from '../icons/paused-light.png'
import PausedDark from '../icons/paused-dark.png'
import { Artwork } from './Artwork'

const useStyles = makeStyles({
  icon: {
    width: '32px',
    height: '32px',
    verticalAlign: 'text-top',
    marginLeft: '-8px',
    marginTop: '-7px',
    paddingRight: '3px',
  },
  text: {
    verticalAlign: 'text-top',
    minWidth: 0,
  },
  titleCell: {
    display: 'inline-flex',
    alignItems: 'center',
    minWidth: 0,
    maxWidth: '100%',
  },
  artwork: {
    width: 38,
    height: 38,
    flexShrink: 0,
    marginRight: '12px',
    borderRadius: 7,
    backgroundColor: '#242424',
  },
  subtitle: {
    opacity: 0.5,
  },
})

export const SongTitleField = ({ showTrackNumbers, showArtwork, ...props }) => {
  const theme = useTheme()
  const classes = useStyles()
  const { record } = props
  const currentTrack = useSelector((state) => state?.player?.current || {})
  const currentId = currentTrack.trackId
  const paused = currentTrack.paused
  const isCurrent =
    currentId &&
    (currentId === record.id ||
      currentId === record.mediaFileId ||
      (currentTrack.source === 'hometube' && currentId === record.videoId))

  const subtitle = record?.tags?.['subtitle']

  const trackName = (r) => {
    const name = r.title
    if (r.trackNumber && showTrackNumbers) {
      return r.trackNumber.toString().padStart(2, '0') + ' ' + name
    }
    if (subtitle) {
      return (
        <>
          {name}
          <span className={classes.subtitle}>{' (' + subtitle + ')'}</span>
        </>
      )
    }
    return name
  }

  const Icon = () => {
    let icon
    if (paused) {
      icon = theme.palette.type === 'light' ? PausedLight : PausedDark
    } else {
      icon = theme.palette.type === 'light' ? PlayingLight : PlayingDark
    }
    return (
      <img
        src={icon}
        className={classes.icon}
        alt={paused ? 'paused' : 'playing'}
      />
    )
  }

  const content = (
    <>
      {isCurrent && <Icon />}
      {showArtwork && (
        <Artwork
          record={record}
          size={96}
          square
          className={classes.artwork}
          title=""
        />
      )}
      <FunctionField
        {...props}
        source="title"
        render={trackName}
        className={clsx(classes.text, showArtwork && classes.titleCell)}
      />
    </>
  )

  return showArtwork ? (
    <div className={classes.titleCell}>{content}</div>
  ) : (
    content
  )
}

SongTitleField.propTypes = {
  record: PropTypes.object,
  showTrackNumbers: PropTypes.bool,
  showArtwork: PropTypes.bool,
}

SongTitleField.defaultProps = {
  record: {},
  showTrackNumbers: false,
  showArtwork: false,
}
