import React, { useCallback, useEffect, useState } from 'react'
import { useDispatch } from 'react-redux'
import { useGetOne, useTranslate } from 'react-admin'
import { GlobalHotKeys } from 'react-hotkeys'
import IconButton from '@material-ui/core/IconButton'
import { useMediaQuery } from '@material-ui/core'
import { RiSaveLine } from 'react-icons/ri'
import PlaylistAddIcon from '@material-ui/icons/PlaylistAdd'
import { LoveButton, useToggleLove } from '../common'
import { openAddToPlaylist, openSaveQueueDialog } from '../actions'
import { keyMap } from '../hotkeys'
import { makeStyles } from '@material-ui/core/styles'
import SleepTimerButton from './SleepTimerButton'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import { entryIdentity, playlistEntry } from './playlistEntries'

const useStyles = makeStyles(() => ({
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
    padding: 0,
    margin: 0,
    height: 44,
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
    width: 44,
    height: 44,
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
}))

const PlayerToolbar = ({
  id,
  isRadio,
  video,
  layout,
  extraActions,
  active = true,
}) => {
  const dispatch = useDispatch()
  const translate = useTranslate()
  const [catalogId, setCatalogId] = useState(null)
  const metadata =
    video &&
    JSON.stringify({
      id: video.id,
      title: video.title,
      channelId: video.channelId || '',
      channelName: video.channelName || '',
      thumbnailUrl: video.thumbnailUrl || '',
      durationSeconds: video.durationSeconds || 0,
    })
  useEffect(() => {
    if (!metadata) return undefined
    let active = true
    const saved = JSON.parse(metadata)
    httpClient(`${REST_URL}/hometubeVideo/${encodeURIComponent(saved.id)}`, {
      method: 'PUT',
      body: metadata,
    })
      .then(() => {
        if (active) setCatalogId(saved.id)
      })
      .catch(() => {
        /* Add to Playlist and favorite can retry catalog storage. */
      })
    return () => {
      active = false
    }
  }, [metadata])
  const { data, loading } = useGetOne(
    video ? 'hometubeVideo' : 'song',
    video?.id || id,
    { enabled: !!(video ? catalogId === video.id : id) && !isRadio },
  )
  const record = video
    ? { ...video, ...data, source: 'hometube', videoId: video.id }
    : data
  const [toggleLove, toggling] = useToggleLove(
    video ? 'hometubeVideo' : 'song',
    record,
  )
  const matchesDesktop = !useMediaQuery(
    '(max-width:768px) and (orientation:portrait)',
  )
  const isDesktop = layout ? layout === 'desktop' : matchesDesktop
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
      if (!id && !video) return
      const entry = video
        ? playlistEntry({ source: 'hometube', video })
        : undefined
      dispatch(
        openAddToPlaylist(
          entry
            ? { selectedIds: [entryIdentity(entry)], selectedEntries: [entry] }
            : { selectedIds: [id] },
        ),
      )
      e.stopPropagation()
    },
    [dispatch, id, video],
  )

  const buttonClass = isDesktop ? classes.button : classes.mobileButton
  const listItemClass = isDesktop ? classes.toolbar : classes.mobileListItem

  const saveQueueButton = (
    <IconButton
      size={isDesktop ? 'small' : undefined}
      onClick={handleSaveQueue}
      disabled={isRadio}
      data-testid="save-queue-button"
      aria-label="Save queue"
      className={buttonClass}
    >
      <RiSaveLine className={!isDesktop ? classes.mobileIcon : undefined} />
    </IconButton>
  )

  const addToPlaylistButton = (
    <IconButton
      size={isDesktop ? 'small' : undefined}
      onClick={handleAddToPlaylist}
      disabled={(!id && !video) || isRadio}
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
      record={record}
      resource={video ? 'hometubeVideo' : 'song'}
      size={isDesktop ? undefined : 'inherit'}
      disabled={(!video && loading) || toggling || (!id && !video) || isRadio}
      className={buttonClass}
    />
  )

  return (
    <>
      <GlobalHotKeys
        keyMap={keyMap}
        handlers={active ? handlers : {}}
        allowChanges
      />
      {isDesktop ? (
        <li className={`${listItemClass} item`}>
          {extraActions}
          {saveQueueButton}
          {addToPlaylistButton}
          {loveButton}
          <SleepTimerButton className={buttonClass} />
        </li>
      ) : (
        <>
          {extraActions && (
            <li className={`${listItemClass} item`}>{extraActions}</li>
          )}
          <li className={`${listItemClass} item`}>{saveQueueButton}</li>
          <li className={`${listItemClass} item`}>{addToPlaylistButton}</li>
          <li className={`${listItemClass} item`}>{loveButton}</li>
          <li className={`${listItemClass} item`}>
            <SleepTimerButton
              className={buttonClass}
              iconClassName={classes.mobileIcon}
            />
          </li>
        </>
      )}
    </>
  )
}

export default PlayerToolbar
