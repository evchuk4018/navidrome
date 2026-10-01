import React, { useLayoutEffect, useRef, useState } from 'react'
import { useSelector } from 'react-redux'
import { Divider, makeStyles } from '@material-ui/core'
import clsx from 'clsx'
import { useTranslate, MenuItemLink } from 'react-admin'
import SearchIcon from '@material-ui/icons/Search'
import FlashOnIcon from '@material-ui/icons/FlashOn'
import MusicNoteOutlinedIcon from '@material-ui/icons/MusicNoteOutlined'
import PlaylistPlayIcon from '@material-ui/icons/PlaylistPlay'
import SidebarPlaylists from './SidebarPlaylists'
import {
  PLAYLIST_ROW_HEIGHT,
  sidebarColors,
  sidebarLinkStates,
} from './sidebarStyles'
import config from '../config'

const useStyles = makeStyles({
  root: {
    display: 'flex',
    flexDirection: 'column',
    width: '100%',
    height: '100%',
    minHeight: 0,
    boxSizing: 'border-box',
    padding: '26px 0 12px',
    overflow: 'hidden',
    backgroundColor: sidebarColors.background,
    '@media (max-height:420px)': { padding: '8px 0' },
    '@media (max-height:360px)': { padding: '4px 0' },
  },
  navigation: {
    flexShrink: 0,
  },
  link: {
    ...sidebarLinkStates,
    position: 'relative',
    height: 54,
    minHeight: 54,
    boxSizing: 'border-box',
    margin: '0 6px 0 5px',
    padding: '0 24px',
    borderRadius: 16,
    fontSize: 17,
    lineHeight: '24px',
    fontWeight: 500,
    overflow: 'hidden',
    '@media (max-height:420px)': { height: 44, minHeight: 44 },
    '@media (max-height:360px)': { height: 32, minHeight: 32, fontSize: 15 },
  },
  icon: {
    '&&': {
      minWidth: 44,
      color: 'inherit !important',
      opacity: 1,
      padding: 0,
    },
    '& svg': {
      fontSize: 26,
      color: 'inherit !important',
    },
  },
  closedIcon: {
    '&&': { minWidth: 0 },
  },
  closedLink: {
    padding: 0,
    justifyContent: 'center',
    '& $label': { display: 'none' },
  },
  label: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  divider: {
    flexShrink: 0,
    margin: '4px 14px 12px',
    backgroundColor: sidebarColors.divider,
  },
  playlists: {
    flex: '1 1 0%',
    minHeight: 0,
    overflow: 'hidden',
  },
})

// MenuItemLink passes its entire label as titleAccess. Keep that label on the
// link and tooltip, rather than passing a React element to the SVG title.
const NavigationIcon = ({ icon: Icon }) => <Icon />

const Menu = () => {
  const open = useSelector((state) => state.admin.ui.sidebarOpen)
  const translate = useTranslate()
  const classes = useStyles()
  const previewRef = useRef(null)
  const [visibleCount, setVisibleCount] = useState(0)
  const showPlaylists = open && config.devSidebarPlaylists

  useLayoutEffect(() => {
    const preview = previewRef.current
    if (!preview) return undefined

    const measure = (height) => {
      setVisibleCount(Math.max(0, Math.floor(height / PLAYLIST_ROW_HEIGHT)))
    }
    measure(preview.getBoundingClientRect().height)
    const observer = new ResizeObserver(([entry]) => {
      measure(entry.contentRect.height)
    })
    observer.observe(preview)
    return () => observer.disconnect()
  }, [showPlaylists])

  const links = [
    {
      to: '/quick-pick',
      label: 'Quick Pick',
      icon: <NavigationIcon icon={FlashOnIcon} />,
      exact: true,
    },
    {
      to: '/search',
      label: translate('menu.search', { _: 'Search' }),
      icon: <NavigationIcon icon={SearchIcon} />,
      exact: true,
    },
    {
      to: '/song',
      label: translate('resources.song.name', { smart_count: 2, _: 'Songs' }),
      icon: <NavigationIcon icon={MusicNoteOutlinedIcon} />,
    },
    {
      to: '/playlist',
      label: translate('resources.playlist.name', {
        smart_count: 2,
        _: 'Playlists',
      }),
      icon: <NavigationIcon icon={PlaylistPlayIcon} />,
    },
  ]

  return (
    <nav className={classes.root} aria-label={translate('ra.action.menu')}>
      <div className={classes.navigation}>
        {links.map(({ to, label, icon, exact }) => (
          <MenuItemLink
            key={to}
            role="link"
            to={to}
            exact={exact}
            aria-label={label}
            className={clsx(classes.link, !open && classes.closedLink)}
            classes={{ icon: clsx(classes.icon, !open && classes.closedIcon) }}
            primaryText={<span className={classes.label}>{label}</span>}
            leftIcon={icon}
          />
        ))}
      </div>
      {showPlaylists && (
        <>
          <Divider className={classes.divider} />
          <div ref={previewRef} className={classes.playlists}>
            <SidebarPlaylists visibleCount={visibleCount} />
          </div>
        </>
      )}
    </nav>
  )
}

export default Menu
