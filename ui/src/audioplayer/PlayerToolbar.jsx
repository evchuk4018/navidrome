import React, { useCallback } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useGetOne, useNotify, useTranslate } from 'react-admin'
import { GlobalHotKeys } from 'react-hotkeys'
import IconButton from '@material-ui/core/IconButton'
import { useMediaQuery } from '@material-ui/core'
import { RiSaveLine } from 'react-icons/ri'
import PlaylistAddIcon from '@material-ui/icons/PlaylistAdd'
import { LoveButton, useToggleLove } from '../common'
import { openAddToPlaylist, openSaveQueueDialog } from '../actions'
import {
  endRadioSession,
  removeRadioItem,
  setRadioAutoplay,
  setRadioMode,
} from '../actions'
import { keyMap } from '../hotkeys'
import { makeStyles } from '@material-ui/core/styles'
import { endPersonalRadio, sendRadioFeedback } from '../quickpick/provider'

const useStyles = makeStyles((theme) => ({
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    flexGrow: 1,
    justifyContent: 'flex-end',
    gap: '0.5rem',
    listStyle: 'none',
    padding: 0,
    margin: 0,
  },
  mobileListItem: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    listStyle: 'none',
    padding: theme.spacing(0.5),
    margin: 0,
    height: 24,
  },
  button: {
    width: '2.5rem',
    height: '2.5rem',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 0,
  },
  mobileButton: {
    width: 24,
    height: 24,
    padding: 0,
    margin: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: '18px',
  },
  mobileIcon: {
    fontSize: '18px',
    display: 'flex',
    alignItems: 'center',
  },
  radioControls: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    color: 'inherit',
    fontSize: '0.75rem',
  },
  radioSelect: {
    maxWidth: 120,
    color: 'inherit',
    background: 'transparent',
    border: `1px solid ${theme.palette.divider}`,
    borderRadius: theme.shape.borderRadius,
  },
  radioButton: {
    padding: theme.spacing(0.5, 0.75),
    color: 'inherit',
    background: 'transparent',
    border: `1px solid ${theme.palette.divider}`,
    borderRadius: theme.shape.borderRadius,
    cursor: 'pointer',
  },
}))

const PlayerToolbar = ({ id, isRadio }) => {
  const dispatch = useDispatch()
  const translate = useTranslate()
  const notify = useNotify()
  const radioSession = useSelector((state) => state.player?.radioSession)
  const current = useSelector((state) => state.player?.current)
  const { data, loading } = useGetOne('song', id, { enabled: !!id })
  const [toggleLove, toggling] = useToggleLove('song', data)
  const isDesktop = useMediaQuery('(min-width:810px)')
  const classes = useStyles()

  const handlers = {
    TOGGLE_LOVE: useCallback(() => toggleLove(), [toggleLove]),
  }

  const handleSaveQueue = useCallback(
    (e) => {
      dispatch(openSaveQueueDialog())
      e.stopPropagation()
    },
    [dispatch],
  )

  const handleAddToPlaylist = useCallback(
    (e) => {
      if (!id) return
      dispatch(openAddToPlaylist({ selectedIds: [id] }))
      e.stopPropagation()
    },
    [dispatch, id],
  )

  const buttonClass = isDesktop ? classes.button : classes.mobileButton
  const listItemClass = isDesktop ? classes.toolbar : classes.mobileListItem
  const activeRadio =
    isRadio &&
    radioSession?.id &&
    (!current?.radioSessionId || current.radioSessionId === radioSession.id)

  const handleModeChange = useCallback(
    (event) => dispatch(setRadioMode(event.target.value)),
    [dispatch],
  )

  const handleAutoplayChange = useCallback(
    (event) => {
      const autoplay = event.target.checked
      dispatch(setRadioAutoplay(autoplay))
      if (!autoplay && radioSession?.id) {
        dispatch(endRadioSession())
        endPersonalRadio(radioSession.id, { disableAutoplay: true }).catch(
          () => {},
        )
      }
    },
    [dispatch, radioSession?.id],
  )

  const handleNotInterested = useCallback(() => {
    if (!radioSession?.id || !current?.radioItemId) return
    sendRadioFeedback(radioSession.id, {
      itemId: current.radioItemId,
      event: 'dislike',
      trackKey: current.song?.radioTrackKey || current.radioTrackKey,
    }).catch(() => notify('Unable to update radio feedback', 'warning'))
    dispatch(removeRadioItem(current.radioItemId))
  }, [current, dispatch, notify, radioSession?.id])

  const handleLove = useCallback(
    (event) => {
      event.preventDefault()
      toggleLove()
      event.stopPropagation()
      if (activeRadio && radioSession?.id && current?.radioItemId) {
        sendRadioFeedback(radioSession.id, {
          itemId: current.radioItemId,
          event: 'keep',
          trackKey: current.song?.radioTrackKey || current.radioTrackKey,
        }).catch(() => {})
      }
    },
    [activeRadio, current, radioSession?.id, toggleLove],
  )

  const saveQueueButton = (
    <IconButton
      size={isDesktop ? 'small' : undefined}
      onClick={handleSaveQueue}
      disabled={isRadio}
      data-testid="save-queue-button"
      className={buttonClass}
    >
      <RiSaveLine className={!isDesktop ? classes.mobileIcon : undefined} />
    </IconButton>
  )

  const addToPlaylistButton = (
    <IconButton
      size={isDesktop ? 'small' : undefined}
      onClick={handleAddToPlaylist}
      disabled={!id || isRadio}
      data-testid="add-to-playlist-button"
      className={buttonClass}
      title={translate('resources.song.actions.addToPlaylist')}
      aria-label={translate('resources.song.actions.addToPlaylist')}
    >
      <PlaylistAddIcon
        className={!isDesktop ? classes.mobileIcon : undefined}
      />
    </IconButton>
  )

  const loveButton = (
    <LoveButton
      record={data}
      resource={'song'}
      size={isDesktop ? undefined : 'inherit'}
      disabled={loading || toggling || !id}
      className={buttonClass}
      onClick={handleLove}
    />
  )

  const radioControls = activeRadio ? (
    <div className={classes.radioControls} data-testid="radio-controls">
      <label>
        <span className="sr-only">Radio mode</span>
        <select
          className={classes.radioSelect}
          aria-label="Radio mode"
          data-testid="radio-tuner"
          value={radioSession.mode || 'balanced'}
          onChange={handleModeChange}
        >
          <option value="familiar">Familiar</option>
          <option value="balanced">Balanced</option>
          <option value="discover">Discover</option>
        </select>
      </label>
      <label className={classes.radioButton}>
        <input
          type="checkbox"
          checked={radioSession.autoplay !== false}
          onChange={handleAutoplayChange}
          data-testid="radio-autoplay"
        />{' '}
        Autoplay
      </label>
      <button
        type="button"
        className={classes.radioButton}
        data-testid="radio-not-interested"
        onClick={handleNotInterested}
      >
        Not interested
      </button>
    </div>
  ) : null

  return (
    <>
      <GlobalHotKeys keyMap={keyMap} handlers={handlers} allowChanges />
      {isDesktop ? (
        <li className={`${listItemClass} item`}>
          {saveQueueButton}
          {addToPlaylistButton}
          {loveButton}
          {radioControls}
        </li>
      ) : (
        <>
          <li className={`${listItemClass} item`}>{saveQueueButton}</li>
          <li className={`${listItemClass} item`}>{addToPlaylistButton}</li>
          <li className={`${listItemClass} item`}>{loveButton}</li>
          {activeRadio && (
            <li className={`${listItemClass} item`}>{radioControls}</li>
          )}
        </>
      )}
    </>
  )
}

export default PlayerToolbar
