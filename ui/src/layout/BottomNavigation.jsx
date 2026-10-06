import React from 'react'
import { makeStyles } from '@material-ui/core/styles'
import { NavLink, matchPath, useLocation } from 'react-router-dom'
import { useTranslate } from 'react-admin'
import {
  BOTTOM_NAVIGATION_HEIGHT,
  BOTTOM_NAVIGATION_SPACE,
  useNavigationLinks,
} from './navigation'
import { sidebarColors } from './sidebarStyles'

const useStyles = makeStyles({
  root: {
    position: 'fixed',
    bottom: 0,
    left: 0,
    right: 0,
    zIndex: 1000,
    height: BOTTOM_NAVIGATION_SPACE,
    paddingBottom: 'env(safe-area-inset-bottom, 0px)',
    boxSizing: 'border-box',
    backgroundColor: sidebarColors.background,
    borderTop: `1px solid ${sidebarColors.divider}`,
  },
  items: {
    position: 'relative',
    display: 'flex',
    height: '100%',
    paddingLeft: 'env(safe-area-inset-left, 0px)',
    paddingRight: 'env(safe-area-inset-right, 0px)',
  },
  track: {
    position: 'relative',
    display: 'flex',
    flex: 1,
    minWidth: 0,
  },
  indicator: {
    position: 'absolute',
    top: 0,
    bottom: 0,
    left: 0,
    width: '25%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    pointerEvents: 'none',
    transition: 'transform 200ms ease-out, opacity 200ms ease-out',
    '&::before': {
      content: '""',
      width: 64,
      maxWidth: 'calc(100% - 8px)',
      height: 44,
      borderRadius: 16,
      backgroundColor: sidebarColors.selection,
    },
    '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
  },
  link: {
    position: 'relative',
    display: 'flex',
    flex: 1,
    minWidth: 0,
    minHeight: BOTTOM_NAVIGATION_HEIGHT - 1,
    alignItems: 'center',
    justifyContent: 'center',
    color: `${sidebarColors.navigation} !important`,
    textDecoration: 'none',
    transition: 'color 200ms ease-out',
    WebkitTapHighlightColor: 'transparent',
    '& svg': {
      fontSize: 26,
      color: 'inherit !important',
      transition: 'transform 200ms ease-out',
    },
    '&[aria-current="page"]': {
      color: `${sidebarColors.activeText} !important`,
      '& svg': { transform: 'scale(1.08)' },
    },
    '&:focus-visible': {
      outline: `2px solid ${sidebarColors.accent}`,
      outlineOffset: -4,
      borderRadius: 16,
    },
    '@media (prefers-reduced-motion: reduce)': {
      transition: 'none',
      '& svg': { transition: 'none' },
      '&[aria-current="page"] svg': { transform: 'none' },
    },
  },
})

const BottomNavigation = () => {
  const classes = useStyles()
  const links = useNavigationLinks()
  const translate = useTranslate()
  const { pathname } = useLocation()
  const activeIndex = links.findIndex(({ to }) =>
    matchPath(pathname, { path: to, exact: to === '/quick-pick' }),
  )

  return (
    <nav className={classes.root} aria-label={translate('ra.action.menu')}>
      <div className={classes.items}>
        <div className={classes.track}>
          <div
            className={classes.indicator}
            aria-hidden="true"
            data-testid="bottom-navigation-indicator"
            style={{
              width: `${100 / Math.max(1, links.length)}%`,
              transform: `translateX(${Math.max(0, activeIndex) * 100}%)`,
              opacity: activeIndex < 0 ? 0 : 1,
            }}
          />
          {links.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              exact={to === '/quick-pick'}
              aria-label={label}
              className={classes.link}
              activeClassName=""
            >
              <Icon aria-hidden="true" />
            </NavLink>
          ))}
        </div>
      </div>
    </nav>
  )
}

export default BottomNavigation
