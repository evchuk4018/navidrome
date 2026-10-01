import React from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import BottomNavigation from './BottomNavigation'
import { emittedRules } from './testMediaQuery'

vi.mock('react-admin', () => ({
  useTranslate:
    () =>
    (key, options = {}) =>
      ({
        'menu.search': 'Search',
        'resources.song.name': 'Songs',
        'resources.playlist.name': 'Playlists',
        'ra.action.menu': 'Menu',
      })[key] ||
      options._ ||
      key,
}))

const renderNavigation = (path = '/quick-pick') => {
  const history = createMemoryHistory({ initialEntries: [path] })
  render(
    <ThemeProvider theme={createTheme({ palette: { type: 'light' } })}>
      <Router history={history}>
        <BottomNavigation />
      </Router>
    </ThemeProvider>,
  )
  return history
}

const selectedLinks = () =>
  screen
    .getAllByRole('link')
    .filter((link) => link.hasAttribute('aria-current'))

describe('bottom navigation', () => {
  it('renders exactly four accessible icons in order with no visible labels or tooltips', () => {
    renderNavigation()
    const links = screen.getAllByRole('link')
    expect(
      links.map((link) => [
        link.getAttribute('aria-label'),
        link.getAttribute('href'),
      ]),
    ).toEqual([
      ['Quick Pick', '/quick-pick'],
      ['Search', '/search'],
      ['Songs', '/song'],
      ['Playlists', '/playlist'],
    ])
    links.forEach((link) => {
      expect(link.textContent).toBe('')
      expect(link.querySelector('svg')).toHaveAttribute('aria-hidden', 'true')
      expect(link).not.toHaveAttribute('title')
      fireEvent.mouseOver(link)
    })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })

  it.each([
    ['/quick-pick', 'Quick Pick', 0],
    ['/search', 'Search', 1],
    ['/search/artist/artist-1', 'Search', 1],
    ['/search/album/album-1', 'Search', 1],
    ['/song', 'Songs', 2],
    ['/song/song-1/show', 'Songs', 2],
    ['/playlist', 'Playlists', 3],
    ['/playlist/pl-1/show', 'Playlists', 3],
    ['/playlist/pl-1', 'Playlists', 3],
  ])(
    'selects the destination for direct navigation to %s',
    (path, label, index) => {
      renderNavigation(path)
      expect(selectedLinks()).toEqual([
        screen.getByRole('link', { name: label }),
      ])
      expect(screen.getByTestId('bottom-navigation-indicator')).toHaveStyle({
        transform: `translateX(${index * 100}%)`,
        opacity: '1',
      })
    },
  )

  it.each([
    '/album/album-1/show',
    '/personal',
    '/songbird',
    '/quick-pick/other',
  ])('does not retain a stale selection on unrelated route %s', (path) => {
    renderNavigation(path)
    expect(selectedLinks()).toHaveLength(0)
    expect(screen.getByTestId('bottom-navigation-indicator')).toHaveStyle({
      opacity: '0',
    })
  })

  it('updates the route and sliding highlight on clicks and browser back/forward', () => {
    const history = renderNavigation()
    fireEvent.click(screen.getByRole('link', { name: 'Search' }))
    fireEvent.click(screen.getByRole('link', { name: 'Playlists' }))
    expect(history.location.pathname).toBe('/playlist')
    expect(selectedLinks()).toEqual([
      screen.getByRole('link', { name: 'Playlists' }),
    ])
    act(() => history.goBack())
    expect(selectedLinks()).toEqual([
      screen.getByRole('link', { name: 'Search' }),
    ])
    expect(screen.getByTestId('bottom-navigation-indicator')).toHaveStyle({
      transform: 'translateX(100%)',
    })
    act(() => history.goForward())
    expect(selectedLinks()).toEqual([
      screen.getByRole('link', { name: 'Playlists' }),
    ])
    expect(screen.getByTestId('bottom-navigation-indicator')).toHaveStyle({
      transform: 'translateX(300%)',
    })
  })

  it('keeps the black and pink colors, 200ms animation, and keyboard focus under a light theme', () => {
    renderNavigation()
    expect(screen.getByRole('navigation')).toHaveStyle({
      backgroundColor: '#0d0d0d',
    })
    const link = screen.getByRole('link', { name: 'Quick Pick' })
    const activeRule = emittedRules().find(
      ({ rule, media }) =>
        media.length === 0 &&
        rule.selectorText?.includes('[aria-current="page"]') &&
        link.matches(rule.selectorText),
    ).rule
    expect(activeRule.style.getPropertyValue('color')).toBe('#ff91be')
    expect(activeRule.style.getPropertyPriority('color')).toBe('important')
    const indicator = screen.getByTestId('bottom-navigation-indicator')
    expect(indicator).toHaveStyle({
      transition: 'transform 200ms ease-out, opacity 200ms ease-out',
    })
    const pill = emittedRules().find(
      ({ rule }) => rule.selectorText === `.${indicator.className}::before`,
    ).rule
    expect(pill.style.getPropertyValue('background-color')).toBe('#29141f')
    const focus = emittedRules().find(
      ({ rule }) =>
        rule.selectorText?.includes(link.className) &&
        rule.selectorText.includes(':focus-visible'),
    ).rule
    expect(focus.style.getPropertyValue('outline')).toContain('#ff2a7f')
  })

  it('disables the highlight and icon transitions and scaling for reduced motion', () => {
    renderNavigation()
    const indicator = screen.getByTestId('bottom-navigation-indicator')
    const icon = screen
      .getByRole('link', { name: 'Quick Pick' })
      .querySelector('svg')
    const reduced = emittedRules().filter(({ media }) =>
      media.includes('(prefers-reduced-motion: reduce)'),
    )
    expect(
      reduced.some(
        ({ rule }) =>
          indicator.matches(rule.selectorText) &&
          rule.style.getPropertyValue('transition') === 'none',
      ),
    ).toBe(true)
    expect(
      reduced.some(
        ({ rule }) =>
          icon.matches(rule.selectorText) &&
          rule.style.getPropertyValue('transition') === 'none',
      ),
    ).toBe(true)
    expect(
      reduced.some(
        ({ rule }) =>
          icon.matches(rule.selectorText) &&
          rule.style.getPropertyValue('transform') === 'none',
      ),
    ).toBe(true)
  })
})
