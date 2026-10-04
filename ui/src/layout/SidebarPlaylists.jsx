import React, { useCallback, useMemo } from 'react'
import { useSelector } from 'react-redux'
import { makeStyles, Typography } from '@material-ui/core'
import {
  MenuItemLink,
  useDataProvider,
  useLocale,
  useNotify,
  useQueryWithStore,
  useTranslate,
} from 'react-admin'
import { useDrop } from 'react-dnd'
import { Artwork } from '../common/Artwork'
import { OverflowTooltip } from '../common/OverflowTooltip'
import { PLAYLIST_DEFAULT_SORT, sortPlaylists } from '../common/playlistOrder'
import { canChangeTracks } from '../common/playlistUtils'
import { useRefreshOnEvents } from '../common/useRefreshOnEvents'
import { DraggableTypes } from '../consts'
import config from '../config'
import {
  PLAYLIST_ROW_HEIGHT,
  sidebarColors,
  sidebarLinkStates,
} from './sidebarStyles'

const useStyles = makeStyles({
  row: {
    ...sidebarLinkStates,
    '&&': {
      ...sidebarLinkStates['&&'],
      color: `${sidebarColors.text} !important`,
    },
    position: 'relative',
    display: 'flex',
    height: PLAYLIST_ROW_HEIGHT,
    minHeight: PLAYLIST_ROW_HEIGHT,
    boxSizing: 'border-box',
    padding: '9px 20px 9px 28px',
    gap: 18,
    borderRadius: 8,
  },
  artwork: {
    width: 54,
    height: 54,
    flexShrink: 0,
    borderRadius: 6,
    backgroundColor: '#242024',
  },
  details: {
    flex: 1,
    minWidth: 0,
  },
  name: {
    color: 'inherit !important',
    fontFamily: 'inherit',
    fontSize: 15,
    lineHeight: '22px',
    fontWeight: 600,
  },
  count: {
    color: `${sidebarColors.secondary} !important`,
    fontFamily: 'inherit',
    fontSize: 13,
    lineHeight: '20px',
  },
})

const SidebarPlaylist = ({ playlist, onRefresh }) => {
  const classes = useStyles()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const translate = useTranslate()
  const locale = useLocale()
  const songCount = playlist.songCount ?? 0
  const count = new Intl.NumberFormat(locale).format(songCount)
  const songs = translate('resources.song.name', {
    smart_count: songCount,
  }).toLocaleLowerCase(locale)

  const [, dropRef] = useDrop(
    () => ({
      accept: canChangeTracks(playlist) ? DraggableTypes.ALL : [],
      drop: async (item) => {
        if (!canChangeTracks(playlist)) return
        try {
          const { data } = await dataProvider.addToPlaylist(playlist.id, item)
          notify('message.songsAddedToPlaylist', 'info', {
            smart_count: data?.added,
          })
          await onRefresh()
        } catch (error) {
          notify('ra.page.error', 'warning')
        }
      },
    }),
    [playlist, dataProvider, notify, onRefresh],
  )

  return (
    <MenuItemLink
      ref={dropRef}
      role="link"
      to={`/playlist/${playlist.id}/show`}
      className={classes.row}
      primaryText={
        <>
          <Artwork
            record={playlist}
            size={108}
            className={classes.artwork}
            title=""
          />
          <div className={classes.details}>
            <OverflowTooltip title={playlist.name} placement="right">
              <Typography
                component="span"
                display="block"
                noWrap
                className={classes.name}
              >
                {playlist.name}
              </Typography>
            </OverflowTooltip>
            <Typography
              component="span"
              display="block"
              noWrap
              className={classes.count}
            >
              {count} {songs}
            </Typography>
          </div>
        </>
      }
    />
  )
}

const SidebarPlaylists = ({ visibleCount }) => {
  const locale = useLocale()
  const ownerId = localStorage.getItem('userId') || ''
  const playlistData = useSelector(
    (state) => state.admin.resources.playlist?.data,
  )
  const { data, loaded, error, refetch } = useQueryWithStore({
    type: 'getList',
    resource: 'playlist',
    payload: {
      pagination: { page: 1, perPage: config.maxSidebarPlaylists },
      sort: PLAYLIST_DEFAULT_SORT,
      filter: {},
    },
  })
  const onRefresh = useCallback(async () => refetch(), [refetch])
  useRefreshOnEvents({ events: ['playlist'], onRefresh })

  const playlists = useMemo(() => {
    if (!loaded || error || !data) return []
    // The capped query decides membership; the resource store supplies local edits.
    return sortPlaylists(
      Object.values(data).map(
        (playlist) => playlistData?.[playlist.id] || playlist,
      ),
      locale,
      ownerId,
    ).slice(0, Math.max(0, visibleCount))
  }, [data, loaded, error, playlistData, locale, ownerId, visibleCount])

  return playlists.map((playlist) => (
    <SidebarPlaylist
      key={playlist.id}
      playlist={playlist}
      onRefresh={onRefresh}
    />
  ))
}

export default SidebarPlaylists
