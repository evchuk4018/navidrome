import React from 'react'
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { Provider } from 'react-redux'
import { createStore } from 'redux'
import { Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { setSidebarVisibility } from 'react-admin'
import config from '../config'
import Menu from './Menu'

vi.mock('../config', () => ({ default: { devSidebarPlaylists: true } }))
vi.mock('./SidebarPlaylists', () => ({
  default: ({ visibleCount }) => (
    <div data-testid="preview" data-visible-count={visibleCount} />
  ),
}))
vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
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

let observers
let previewHeight
let mobile

const renderMenu = ({
  open = true,
  path = '/quick-pick',
  themeOptions = {},
} = {}) => {
  const store = createStore(
    (state = { admin: { ui: { sidebarOpen: open } } }, action) =>
      action.type === setSidebarVisibility(false).type
        ? { admin: { ui: { sidebarOpen: action.payload } } }
        : state,
  )
  const history = createMemoryHistory({ initialEntries: [path] })
  const theme = createTheme({
    props: { MuiUseMediaQuery: { noSsr: true } },
    ...themeOptions,
  })
  render(
    <Provider store={store}>
      <ThemeProvider theme={theme}>
        <Router history={history}>
          <Menu />
        </Router>
      </ThemeProvider>
    </Provider>,
  )
  return { store, history }
}

const resizePreview = (height) =>
  act(() => {
    observers[observers.length - 1].callback([{ contentRect: { height } }])
  })

beforeEach(() => {
  vi.restoreAllMocks()
  config.devSidebarPlaylists = true
  previewHeight = 0
  observers = []
  mobile = false
  window.matchMedia = (query) => ({
    matches: mobile && query.includes('max-width'),
    media: query,
    addListener: vi.fn(),
    removeListener: vi.fn(),
  })
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback) {
        this.callback = callback
        observers.push(this)
      }
      observe = vi.fn()
      disconnect = vi.fn()
    },
  )
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
    () => ({
      height: previewHeight,
      width: 280,
      top: 0,
      bottom: previewHeight,
      left: 0,
      right: 280,
      x: 0,
      y: 0,
    }),
  )
})

describe('sidebar navigation', () => {
  it('contains exactly the four reference links in order and no extra buttons', () => {
    renderMenu()
    expect(
      screen
        .getAllByRole('link')
        .map((link) => [link.textContent, link.getAttribute('href')]),
    ).toEqual([
      ['Quick Pick', '/quick-pick'],
      ['Search', '/search'],
      ['Songs', '/song'],
      ['Playlists', '/playlist'],
    ])
    expect(screen.queryAllByRole('button')).toHaveLength(0)
    for (const label of [
      'Albums',
      'Artists',
      'Radio',
      'Shares',
      'All Libraries',
      'Shared Playlists',
      'Only favourites',
    ]) {
      expect(screen.queryByText(label)).not.toBeInTheDocument()
    }
  })

  it.each([
    ['/quick-pick', 'Quick Pick'],
    ['/search', 'Search'],
    ['/song/song-1/show', 'Songs'],
    ['/playlist/pl-1/show', 'Playlists'],
  ])('marks the current destination at %s', (path, label) => {
    renderMenu({ path })
    expect(screen.getByRole('link', { name: label })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(
      screen
        .getAllByRole('link')
        .filter((link) => link.hasAttribute('aria-current')),
    ).toHaveLength(1)
  })

  it('navigates directly to the full playlist page', () => {
    const { history } = renderMenu()
    fireEvent.click(screen.getByRole('link', { name: 'Playlists' }))
    expect(history.location.pathname).toBe('/playlist')
  })

  it('closes the mobile drawer after navigation', () => {
    mobile = true
    const { history, store } = renderMenu()
    fireEvent.click(screen.getByRole('link', { name: 'Songs' }))
    expect(history.location.pathname).toBe('/song')
    expect(store.getState().admin.ui.sidebarOpen).toBe(false)
    expect(screen.queryByTestId('preview')).not.toBeInTheDocument()
  })

  it('keeps four accessible icons and working tooltips when collapsed', async () => {
    renderMenu({ open: false })
    expect(screen.getAllByRole('link')).toHaveLength(4)
    const link = screen.getByRole('link', { name: 'Quick Pick' })
    expect(link.querySelector('svg')).toBeInTheDocument()
    expect(link.querySelector('span')).not.toBeVisible()
    expect(screen.queryByTestId('preview')).not.toBeInTheDocument()
    fireEvent.mouseOver(link)
    await waitFor(() =>
      expect(
        within(screen.getByRole('tooltip')).getByText('Quick Pick'),
      ).toBeVisible(),
    )
  })

  it('uses the dark reference styling even under a light theme with menu overrides', () => {
    renderMenu({
      themeOptions: {
        palette: { type: 'light' },
        overrides: {
          RaMenuItemLink: { root: { color: '#232323 !important' } },
        },
      },
    })
    expect(screen.getByRole('navigation')).toHaveStyle({
      backgroundColor: '#0d0d0d',
      overflow: 'hidden',
    })
    const active = screen.getByRole('link', { name: 'Quick Pick' })
    expect(active).toHaveStyle({ height: '54px' })
    // jsdom does not resolve the cascade for these nested !important rules.
    // Inspect the emitted rule matching the active link and its CSS priority.
    const activeRule = Array.from(document.styleSheets)
      .flatMap((sheet) => Array.from(sheet.cssRules))
      .find(
        (rule) =>
          rule.selectorText?.includes('[aria-current="page"]') &&
          !rule.selectorText.includes('::') &&
          active.matches(rule.selectorText),
      )
    expect(activeRule.style.getPropertyValue('color')).toBe('#ff91be')
    expect(activeRule.style.getPropertyPriority('color')).toBe('important')
    expect(activeRule.style.getPropertyValue('background-color')).toBe(
      '#29141f',
    )
    expect(activeRule.style.getPropertyPriority('background-color')).toBe(
      'important',
    )
  })
})

describe('playlist preview capacity', () => {
  it.each([
    [0, 0],
    [71.9, 0],
    [72, 1],
    [143.9, 1],
    [144, 2],
    [720, 10],
  ])('fits %s pixels into %s complete rows', (height, rows) => {
    previewHeight = height
    renderMenu()
    expect(screen.getByTestId('preview')).toHaveAttribute(
      'data-visible-count',
      String(rows),
    )
  })

  it('responds to viewport and player changes measured by ResizeObserver', () => {
    previewHeight = 300
    renderMenu()
    expect(screen.getByTestId('preview')).toHaveAttribute(
      'data-visible-count',
      '4',
    )
    resizePreview(220)
    expect(screen.getByTestId('preview')).toHaveAttribute(
      'data-visible-count',
      '3',
    )
    resizePreview(200)
    expect(screen.getByTestId('preview')).toHaveAttribute(
      'data-visible-count',
      '2',
    )
    resizePreview(300)
    expect(screen.getByTestId('preview')).toHaveAttribute(
      'data-visible-count',
      '4',
    )
  })

  it('remeasures after reopening and disconnects the old observer', () => {
    previewHeight = 144
    const { store } = renderMenu()
    const first = observers[0]
    act(() => void store.dispatch(setSidebarVisibility(false)))
    expect(first.disconnect).toHaveBeenCalledOnce()
    previewHeight = 72
    act(() => void store.dispatch(setSidebarVisibility(true)))
    expect(screen.getByTestId('preview')).toHaveAttribute(
      'data-visible-count',
      '1',
    )
    expect(observers).toHaveLength(2)
  })

  it('retains the four main links when the preview feature is disabled', () => {
    config.devSidebarPlaylists = false
    renderMenu()
    expect(screen.getAllByRole('link')).toHaveLength(4)
    expect(screen.queryByTestId('preview')).not.toBeInTheDocument()
    expect(observers).toHaveLength(0)
  })
})
