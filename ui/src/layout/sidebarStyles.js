export const SIDEBAR_WIDTH = 280
export const CLOSED_SIDEBAR_WIDTH = 55
export const PLAYLIST_ROW_HEIGHT = 72

export const sidebarColors = {
  background: '#0d0d0d',
  accent: '#ff2a7f',
  activeText: '#ff91be',
  selection: '#29141f',
  text: '#f5f5f5',
  navigation: '#d0d0d0',
  secondary: '#929292',
  divider: '#242424',
}

// Scope these rules to sidebar links: several selectable themes use !important
// menu colors, and their top-bar menus must retain those theme colors.
export const sidebarLinkStates = {
  '&&': {
    color: `${sidebarColors.navigation} !important`,
    backgroundColor: 'transparent',
    textDecoration: 'none',
    fontFamily: "system-ui, 'Helvetica Neue', Helvetica, Arial, sans-serif",
  },
  '&&:hover': {
    backgroundColor: `${sidebarColors.selection} !important`,
  },
  '&&[aria-current="page"]': {
    color: `${sidebarColors.activeText} !important`,
    backgroundColor: `${sidebarColors.selection} !important`,
    fontWeight: 600,
    '&::before': {
      content: '""',
      position: 'absolute',
      top: 0,
      bottom: 0,
      left: 0,
      width: 4,
      borderRadius: 4,
      backgroundColor: sidebarColors.accent,
    },
  },
  '&&.Mui-focusVisible, &&:focus-visible': {
    outline: `2px solid ${sidebarColors.accent}`,
    outlineOffset: -2,
  },
}
