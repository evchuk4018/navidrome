import React, { useCallback, useMemo } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { Layout as RALayout, toggleSidebar } from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import { HotKeys } from 'react-hotkeys'
import Menu from './Menu'
import AppBar from './AppBar'
import Notification from './Notification'
import useCurrentTheme from '../themes/useCurrentTheme'
import { useSearchRefocus } from '../common/useSearchRefocus'
import { useUserLibraries } from '../common/useUserLibraries'
import {
  CLOSED_SIDEBAR_WIDTH,
  SIDEBAR_WIDTH,
  sidebarColors,
} from './sidebarStyles'

const useStyles = makeStyles({
  root: {
    paddingBottom: (props) =>
      props.addPadding ? 'calc(80px + env(safe-area-inset-bottom, 0px))' : 0,
  },
})

const Layout = (props) => {
  const currentTheme = useCurrentTheme()
  const queue = useSelector((state) => state.player?.queue)
  const hasPlayer = (queue?.length || 0) > 0
  const classes = useStyles({ addPadding: hasPlayer })
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

    return {
      ...currentTheme,
      sidebar: {
        ...currentTheme.sidebar,
        width: SIDEBAR_WIDTH,
        closedWidth: CLOSED_SIDEBAR_WIDTH,
      },
      overrides: {
        ...currentTheme.overrides,
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
  }, [currentTheme, hasPlayer])

  const keyHandlers = {
    TOGGLE_MENU: useCallback(() => dispatch(toggleSidebar()), [dispatch]),
  }

  return (
    <HotKeys handlers={keyHandlers}>
      <RALayout
        {...props}
        className={classes.root}
        menu={Menu}
        appBar={AppBar}
        theme={theme}
        notification={Notification}
      />
    </HotKeys>
  )
}

export default Layout
