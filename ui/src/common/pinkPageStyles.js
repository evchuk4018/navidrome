import React, { useMemo } from 'react'
import PropTypes from 'prop-types'
import { createTheme, ThemeProvider, useTheme } from '@material-ui/core/styles'
import { sidebarColors } from '../layout/sidebarStyles'

const pinkDisabledText = 'rgba(245, 245, 245, 0.38)'
const pinkDisabledBorder = 'rgba(245, 245, 245, 0.24)'

// These styles are deliberately mounted on a page root. They keep the pink
// library treatment local to Songs and Playlists instead of changing the
// selected application theme used by the rest of Navidrome.
export const pinkPageStyles = (theme) => ({
  root: {
    boxSizing: 'border-box',
    minWidth: 0,
    minHeight: '100%',
    padding: theme.spacing(3),
    backgroundColor: sidebarColors.background,
    color: sidebarColors.text,
    fontFamily: "system-ui, 'Helvetica Neue', Helvetica, Arial, sans-serif",
    '& .RaList-root, & .RaList-main, & .RaList-content': {
      minWidth: 0,
      backgroundColor: 'transparent',
    },
    '& .RaList-main': {
      overflow: 'visible',
    },
    '& .RaList-actions, & .RaListToolbar-root, & .RaListToolbar-toolbar': {
      minHeight: 56,
      boxSizing: 'border-box',
      padding: theme.spacing(1, 1.5),
      marginBottom: theme.spacing(1.5),
      border: `1px solid ${sidebarColors.divider}`,
      borderRadius: 16,
      backgroundColor: '#171017',
      color: sidebarColors.text,
      gap: theme.spacing(1),
      flexWrap: 'wrap',
    },
    '& .MuiPaper-root': {
      color: sidebarColors.text,
      backgroundColor: '#171017',
      backgroundImage: 'none',
      border: `1px solid ${sidebarColors.divider}`,
      borderRadius: 16,
      boxShadow: '0 12px 32px rgba(0, 0, 0, .2)',
    },
    '& .MuiTableContainer-root': {
      overflowX: 'auto',
      border: `1px solid ${sidebarColors.divider}`,
      borderRadius: 16,
      backgroundColor: '#151015',
    },
    '& .MuiTable-root': {
      minWidth: 640,
      borderCollapse: 'separate',
      borderSpacing: 0,
    },
    '& .MuiTableHead-root .MuiTableCell-root': {
      color: `${sidebarColors.secondary} !important`,
      backgroundColor: '#21111a',
      fontWeight: 700,
      letterSpacing: '0.02em',
      borderBottom: `1px solid ${sidebarColors.divider}`,
      whiteSpace: 'nowrap',
    },
    '& .MuiTableCell-root': {
      color: `${sidebarColors.text} !important`,
      borderBottom: `1px solid ${sidebarColors.divider}`,
    },
    '& .MuiTableBody-root .MuiTableRow-root': {
      backgroundColor: 'rgba(255, 255, 255, .018)',
      transition: 'background-color 120ms ease-in-out',
    },
    '& .MuiTableBody-root .MuiTableRow-root:hover': {
      backgroundColor: `${sidebarColors.selection} !important`,
    },
    '& .MuiTableBody-root .MuiTableRow-root:focus-within': {
      outline: `2px solid ${sidebarColors.accent}`,
      outlineOffset: -2,
    },
    '& .MuiButton-root': {
      minHeight: 38,
      borderRadius: 10,
      color: `${sidebarColors.text} !important`,
      fontWeight: 600,
      textTransform: 'none',
      '&:hover': {
        backgroundColor: `${sidebarColors.selection} !important`,
      },
      '&:focus-visible': {
        outline: `2px solid ${sidebarColors.accent}`,
        outlineOffset: 2,
      },
      '&.Mui-disabled, &.Mui-disabled:hover': {
        color: `${pinkDisabledText} !important`,
        backgroundColor: 'transparent !important',
      },
    },
    '& .MuiIconButton-root': {
      color: `${sidebarColors.navigation} !important`,
      borderRadius: 10,
      '&:hover': {
        backgroundColor: `${sidebarColors.selection} !important`,
        color: `${sidebarColors.activeText} !important`,
      },
      '&:focus-visible': {
        outline: `2px solid ${sidebarColors.accent}`,
        outlineOffset: 2,
      },
      '&.Mui-disabled, &.Mui-disabled:hover': {
        color: `${pinkDisabledText} !important`,
        backgroundColor: 'transparent !important',
      },
    },
    '& .MuiInputBase-root': {
      color: `${sidebarColors.text} !important`,
      borderRadius: 10,
    },
    '& .MuiInputBase-root.Mui-disabled, & .MuiInputBase-input.Mui-disabled': {
      color: `${pinkDisabledText} !important`,
    },
    '& .MuiInputLabel-root': {
      color: `${sidebarColors.secondary} !important`,
    },
    '& .MuiInputLabel-root.Mui-disabled, & .MuiFormLabel-root.Mui-disabled': {
      color: `${pinkDisabledText} !important`,
    },
    '& .MuiOutlinedInput-notchedOutline': {
      borderColor: `${sidebarColors.divider} !important`,
    },
    '& .MuiOutlinedInput-root:hover .MuiOutlinedInput-notchedOutline': {
      borderColor: `${sidebarColors.activeText} !important`,
    },
    '& .MuiOutlinedInput-root.Mui-focused .MuiOutlinedInput-notchedOutline': {
      borderColor: `${sidebarColors.accent} !important`,
      borderWidth: 2,
    },
    '& .MuiOutlinedInput-root.Mui-disabled .MuiOutlinedInput-notchedOutline, & .MuiOutlinedInput-root.Mui-disabled:hover .MuiOutlinedInput-notchedOutline':
      {
        borderColor: `${pinkDisabledBorder} !important`,
      },
    '& .MuiCheckbox-root, & .MuiRadio-root': {
      color: `${sidebarColors.secondary} !important`,
      '&.Mui-checked': {
        color: `${sidebarColors.accent} !important`,
      },
      '&:focus-visible': {
        outline: `2px solid ${sidebarColors.accent}`,
        outlineOffset: 2,
      },
    },
    '& .MuiCheckbox-root.Mui-disabled, & .MuiCheckbox-root.Mui-disabled.Mui-checked, & .MuiRadio-root.Mui-disabled, & .MuiRadio-root.Mui-disabled.Mui-checked':
      {
        color: `${pinkDisabledText} !important`,
      },
    '& .MuiSwitch-colorSecondary.Mui-checked': {
      color: `${sidebarColors.accent} !important`,
    },
    '& .MuiSwitch-colorSecondary.Mui-checked + .MuiSwitch-track': {
      backgroundColor: `${sidebarColors.accent} !important`,
    },
    '& .MuiSwitch-colorSecondary.Mui-disabled, & .MuiSwitch-colorSecondary.Mui-disabled.Mui-checked':
      {
        color: `${pinkDisabledText} !important`,
      },
    '& .MuiSwitch-colorSecondary.Mui-disabled + .MuiSwitch-track, & .MuiSwitch-colorSecondary.Mui-disabled.Mui-checked + .MuiSwitch-track':
      {
        backgroundColor: `${pinkDisabledBorder} !important`,
      },
    '& .MuiTablePagination-root': {
      color: `${sidebarColors.secondary} !important`,
      borderTop: `1px solid ${sidebarColors.divider}`,
    },
    '& .MuiPaginationItem-root': {
      color: `${sidebarColors.navigation} !important`,
      '&.Mui-selected': {
        color: `${sidebarColors.text} !important`,
        backgroundColor: `${sidebarColors.selection} !important`,
      },
    },
    '& .MuiPaginationItem-root.Mui-disabled': {
      color: `${pinkDisabledText} !important`,
      backgroundColor: 'transparent !important',
      '&:hover': {
        backgroundColor: 'transparent !important',
      },
    },
    '& .MuiMenu-paper, & .MuiPopover-paper': {
      color: `${sidebarColors.text} !important`,
      backgroundColor: '#171017',
      border: `1px solid ${sidebarColors.divider}`,
      borderRadius: 12,
    },
    '& .MuiMenuItem-root': {
      color: `${sidebarColors.text} !important`,
      '&:hover, &.Mui-selected': {
        backgroundColor: `${sidebarColors.selection} !important`,
      },
      '&.Mui-disabled': {
        color: `${pinkDisabledText} !important`,
        backgroundColor: 'transparent !important',
        '&:hover': {
          backgroundColor: 'transparent !important',
        },
      },
    },
    '& .RaBulkActionsToolbar-root': {
      borderRadius: 14,
      backgroundColor: '#21111a',
      color: sidebarColors.text,
    },
    [theme.breakpoints.down('xs')]: {
      padding: theme.spacing(1.5),
      '& .RaList-actions, & .RaListToolbar-root, & .RaListToolbar-toolbar': {
        marginBottom: theme.spacing(1),
        padding: theme.spacing(0.75),
        borderRadius: 12,
      },
      '& .MuiTable-root': {
        minWidth: 560,
      },
    },
  },
})

// Material UI menus, selects, and dialogs render through portals. A nested
// theme keeps those surfaces pink as well, while the selected application
// theme continues to drive the AppBar, player, and every other route.
export const PinkPageTheme = ({ children }) => {
  const selectedTheme = useTheme()
  const theme = useMemo(() => {
    const selectedThemeWithoutPalette = { ...selectedTheme }
    delete selectedThemeWithoutPalette.palette
    return createTheme(
      {
        ...selectedThemeWithoutPalette,
        palette: {
          type: 'dark',
          primary: {
            main: sidebarColors.accent,
            contrastText: '#ffffff',
          },
          secondary: {
            main: sidebarColors.activeText,
            contrastText: '#ffffff',
          },
          background: {
            default: sidebarColors.background,
            paper: '#171017',
          },
          text: {
            primary: sidebarColors.text,
            secondary: sidebarColors.secondary,
          },
        },
      },
      {
        overrides: {
          MuiPaper: {
            root: {
              color: sidebarColors.text,
              backgroundColor: '#171017',
              backgroundImage: 'none',
            },
          },
          MuiButton: {
            root: {
              color: sidebarColors.text,
              textTransform: 'none',
              '&:hover': {
                backgroundColor: sidebarColors.selection,
              },
              '&:focus-visible': {
                outline: `2px solid ${sidebarColors.accent}`,
                outlineOffset: 2,
              },
              '&.Mui-disabled, &.Mui-disabled:hover': {
                color: `${pinkDisabledText} !important`,
                backgroundColor: 'transparent !important',
              },
            },
          },
          MuiIconButton: {
            root: {
              color: sidebarColors.navigation,
              '&:hover': {
                backgroundColor: sidebarColors.selection,
                color: sidebarColors.activeText,
              },
              '&:focus-visible': {
                outline: `2px solid ${sidebarColors.accent}`,
                outlineOffset: 2,
              },
              '&.Mui-disabled, &.Mui-disabled:hover': {
                color: `${pinkDisabledText} !important`,
                backgroundColor: 'transparent !important',
              },
            },
          },
          MuiInputBase: {
            root: {
              '&.Mui-disabled, &.Mui-disabled .MuiInputBase-input': {
                color: `${pinkDisabledText} !important`,
              },
            },
          },
          MuiInputLabel: {
            root: {
              '&.Mui-disabled': {
                color: `${pinkDisabledText} !important`,
              },
            },
          },
          MuiFormLabel: {
            root: {
              '&.Mui-disabled': {
                color: `${pinkDisabledText} !important`,
              },
            },
          },
          MuiOutlinedInput: {
            root: {
              '&.Mui-disabled .MuiOutlinedInput-notchedOutline, &.Mui-disabled:hover .MuiOutlinedInput-notchedOutline':
                {
                  borderColor: `${pinkDisabledBorder} !important`,
                },
            },
          },
          MuiMenu: {
            paper: {
              color: sidebarColors.text,
              backgroundColor: '#171017',
              border: `1px solid ${sidebarColors.divider}`,
              borderRadius: 12,
            },
          },
          MuiMenuItem: {
            root: {
              color: sidebarColors.text,
              '&:hover, &.Mui-selected': {
                backgroundColor: sidebarColors.selection,
              },
              '&.Mui-disabled': {
                color: `${pinkDisabledText} !important`,
                backgroundColor: 'transparent !important',
                '&:hover': {
                  backgroundColor: 'transparent !important',
                },
              },
            },
          },
          MuiCheckbox: {
            root: {
              color: sidebarColors.secondary,
              '&.Mui-checked': {
                color: sidebarColors.accent,
              },
              '&.Mui-disabled, &.Mui-disabled.Mui-checked': {
                color: `${pinkDisabledText} !important`,
              },
            },
          },
          MuiRadio: {
            root: {
              color: sidebarColors.secondary,
              '&.Mui-checked': {
                color: sidebarColors.accent,
              },
              '&.Mui-disabled, &.Mui-disabled.Mui-checked': {
                color: `${pinkDisabledText} !important`,
              },
            },
          },
          MuiSwitch: {
            colorSecondary: {
              color: sidebarColors.accent,
              '&.Mui-checked': {
                color: sidebarColors.accent,
              },
              '&.Mui-checked + .MuiSwitch-track': {
                backgroundColor: sidebarColors.accent,
              },
              '&.Mui-disabled, &.Mui-disabled.Mui-checked': {
                color: `${pinkDisabledText} !important`,
              },
              '&.Mui-disabled + .MuiSwitch-track, &.Mui-disabled.Mui-checked + .MuiSwitch-track':
                {
                  backgroundColor: `${pinkDisabledBorder} !important`,
                },
            },
          },
          MuiPaginationItem: {
            root: {
              '&.Mui-disabled': {
                color: `${pinkDisabledText} !important`,
                backgroundColor: 'transparent !important',
                '&:hover': {
                  backgroundColor: 'transparent !important',
                },
              },
            },
          },
        },
      },
    )
  }, [selectedTheme])

  return React.createElement(ThemeProvider, { theme }, children)
}

PinkPageTheme.propTypes = {
  children: PropTypes.node.isRequired,
}

export { sidebarColors }
