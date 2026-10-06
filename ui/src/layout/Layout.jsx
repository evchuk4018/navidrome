import React, { useCallback, useMemo } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { Layout as RALayout, Sidebar, toggleSidebar } from 'react-admin'
import { useMediaQuery } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { HotKeys } from 'react-hotkeys'
import { useLocation } from 'react-router-dom'
import Menu from './Menu'
import AppBar from './AppBar'
import BottomNavigation from './BottomNavigation'
import {
  BOTTOM_NAVIGATION_HEIGHT,
  COMPACT_NAVIGATION_QUERY,
} from './navigation'
import Notification from './Notification'
import useCurrentTheme from '../themes/useCurrentTheme'
import { useSearchRefocus } from '../common/useSearchRefocus'
import { useUserLibraries } from '../common/useUserLibraries'
import { useHomeTubePlayback } from '../hometube/HomeTubePlaybackContext'
import {
  CLOSED_SIDEBAR_WIDTH,
  SIDEBAR_WIDTH,
  sidebarColors,
} from './sidebarStyles'

const useStyles = makeStyles({
  root: {
    paddingBottom: (props) =>
      props.compact || props.addPadding
        ? `calc(${(props.compact ? BOTTOM_NAVIGATION_HEIGHT : 0) + (props.addPadding ? 80 : 0)}px + env(safe-area-inset-bottom, 0px))`
        : 0,
  },
})

const Layout = (props) => {
  const currentTheme = useCurrentTheme()
  const compact = useMediaQuery(COMPACT_NAVIGATION_QUERY, { noSsr: true })
  const { pathname } = useLocation()
  const isDiscoveryPage = pathname === '/quick-pick' || pathname === '/search'
  const isLibraryPage =
    pathname === '/song' ||
    pathname.startsWith('/song/') ||
    pathname === '/playlist' ||
    pathname.startsWith('/playlist/')
  const isHomeTubePage = pathname === '/hometube' || pathname.startsWith('/hometube/')
  const isPinkPage = isDiscoveryPage || isLibraryPage || isHomeTubePage
  const queue = useSelector((state) => state.player?.queue)
  const homeTubePlayback = useHomeTubePlayback()
  const hasPlayer =
    (queue?.length || 0) > 0 ||
    Boolean(homeTubePlayback.activeSource === 'hometube' && homeTubePlayback.currentVideo)
  const classes = useStyles({ addPadding: hasPlayer, compact })
  const dispatch = useDispatch()
  useSearchRefocus()
  useUserLibraries()

  const theme = useMemo(() => {
    const playerSpace = hasPlayer ? '80px' : '0px'
    const height = (viewport, top = '48px') =>
      `calc(${viewport} - ${top} - ${playerSpace} - env(safe-area-inset-bottom, 0px))`
    const mobile = '@media (max-width:599.95px)'
    const dynamicViewport = '@supports (height: 100dvh)'
    const existingSidebar = currentTheme.overrides?.RaSidebar || {}
    const existingLayout = currentTheme.overrides?.RaLayout || {}

    return {
      ...currentTheme,
      sidebar: {
        ...currentTheme.sidebar,
        width: SIDEBAR_WIDTH,
        closedWidth: CLOSED_SIDEBAR_WIDTH,
      },
      overrides: {
        ...currentTheme.overrides,
        ...((isPinkPage || compact) && {
          RaLayout: {
            ...existingLayout,
            root: {
              ...existingLayout.root,
              ...(isPinkPage && {
                background: `${sidebarColors.background} !important`,
                color: sidebarColors.text,
              }),
              minWidth: 0,
            },
            contentWithSidebar: {
              ...existingLayout.contentWithSidebar,
              ...(isPinkPage && {
                background: `${sidebarColors.background} !important`,
              }),
              gap: 0,
            },
            content: {
              ...existingLayout.content,
              ...(isPinkPage && {
                background: `${sidebarColors.background} !important`,
                padding: '0 !important',
                borderRadius: 0,
              }),
              minWidth: 0,
            },
            ...(compact && {
              appFrame: {
                ...existingLayout.appFrame,
                marginTop: '0 !important',
              },
            }),
          },
        }),
        RaSidebar: {
          ...existingSidebar,
          root: {
            ...existingSidebar.root,
            height: height('100vh'),
            borderRadius: 0,
            [dynamicViewport]: { height: height('100dvh') },
            [mobile]: {
              height: height('100vh', '0px'),
              [dynamicViewport]: { height: height('100dvh', '0px') },
            },
          },
          fixed: {
            ...existingSidebar.fixed,
            width: 'inherit',
            height: height('100vh'),
            overflow: 'hidden',
            backgroundColor: sidebarColors.background,
            [dynamicViewport]: { height: height('100dvh') },
          },
          drawerPaper: {
            ...existingSidebar.drawerPaper,
            backgroundColor: `${sidebarColors.background} !important`,
            color: sidebarColors.text,
            boxSizing: 'border-box',
            borderRadius: 0,
            borderRight: `1px solid ${sidebarColors.divider}`,
            overflow: 'hidden',
            [mobile]: {
              height: height('100vh', '0px'),
              [dynamicViewport]: { height: height('100dvh', '0px') },
            },
          },
        },
      },
    }
  }, [currentTheme, hasPlayer, isPinkPage, compact])

  const keyHandlers = {
    TOGGLE_MENU: useCallback(() => {
      if (!compact) dispatch(toggleSidebar())
    }, [dispatch, compact]),
  }

  return (
    <HotKeys handlers={keyHandlers}>
      <RALayout
        {...props}
        className={classes.root}
        menu={Menu}
        appBar={AppBar}
        sidebar={compact ? BottomNavigation : Sidebar}
        theme={theme}
        notification={Notification}
      >
        {props.children}
      </RALayout>
    </HotKeys>
  )
}

export default Layout
