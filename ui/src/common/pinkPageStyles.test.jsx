import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { createTheme, ThemeProvider, useTheme } from '@material-ui/core/styles'
import { PinkPageTheme, sidebarColors } from './pinkPageStyles'

const ThemeProbe = () => {
  const theme = useTheme()
  return (
    <output
      data-testid="theme-probe"
      data-palette-type={theme.palette.type}
      data-primary-main={theme.palette.primary.main}
      data-primary-light={theme.palette.primary.light}
      data-primary-dark={theme.palette.primary.dark}
      data-action-hover={theme.palette.action.hover}
      data-action-selected={theme.palette.action.selected}
      data-action-disabled={theme.palette.action.disabled}
    />
  )
}

describe('PinkPageTheme', () => {
  it.each(['light', 'dark'])(
    'keeps the selected %s theme unchanged outside the page boundary',
    (type) => {
      const selectedTheme = createTheme({ palette: { type } })
      const expectedPinkTheme = createTheme({
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
      })
      render(
        <ThemeProvider theme={selectedTheme}>
          <PinkPageTheme>
            <ThemeProbe />
          </PinkPageTheme>
        </ThemeProvider>,
      )

      const probe = screen.getByTestId('theme-probe')
      expect(probe).toHaveAttribute('data-palette-type', 'dark')
      expect(probe).toHaveAttribute(
        'data-primary-main',
        expectedPinkTheme.palette.primary.main,
      )
      expect(probe).toHaveAttribute(
        'data-primary-light',
        expectedPinkTheme.palette.primary.light,
      )
      expect(probe).toHaveAttribute(
        'data-primary-dark',
        expectedPinkTheme.palette.primary.dark,
      )
      expect(probe).toHaveAttribute(
        'data-action-hover',
        expectedPinkTheme.palette.action.hover,
      )
      expect(probe).toHaveAttribute(
        'data-action-selected',
        expectedPinkTheme.palette.action.selected,
      )
      expect(probe).toHaveAttribute(
        'data-action-disabled',
        expectedPinkTheme.palette.action.disabled,
      )
      expect(selectedTheme.palette.type).toBe(type)
      expect(selectedTheme.palette.primary.main).not.toBe(sidebarColors.accent)
    },
  )
})
