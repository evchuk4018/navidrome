import React from 'react'
import { act, render } from '@testing-library/react'
import { Provider } from 'react-redux'
import { createStore } from 'redux'
import { Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import themes from '../themes'
import Layout from './Layout'

const mocks = vi.hoisted(() => ({
  layout: vi.fn(),
  theme: null,
  libraries: vi.fn(),
}))
vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  Layout: (props) => {
    mocks.layout(props)
    return null
  },
}))
vi.mock('../themes/useCurrentTheme', () => ({ default: () => mocks.theme }))
vi.mock('../common/useUserLibraries', () => ({
  useUserLibraries: () => mocks.libraries(),
}))
vi.mock('../common/useSearchRefocus', () => ({ useSearchRefocus: () => {} }))
vi.mock('react-hotkeys', () => ({ HotKeys: ({ children }) => children }))
vi.mock('./Menu', () => ({ default: () => null }))
vi.mock('./AppBar', () => ({ default: () => null }))
vi.mock('./Notification', () => ({ default: () => null }))

const renderLayout = (
  queue = [],
  history = createMemoryHistory({ initialEntries: ['/song'] }),
) => {
  const store = createStore((state = { player: { queue } }, action) =>
    action.type === 'TEST/QUEUE' ? { player: { queue: action.queue } } : state,
  )
  render(
    <Provider store={store}>
      <Router history={history}>
        <Layout />
      </Router>
    </Provider>,
  )
  return store
}
const lastTheme = () => mocks.layout.mock.lastCall[0].theme

beforeEach(() => {
  vi.clearAllMocks()
  mocks.theme = themes.DarkTheme
})

describe('sidebar layout integration', () => {
  it.each(Object.entries(themes))(
    'keeps a dark sidebar in %s without altering the selected theme elsewhere',
    (name, selectedTheme) => {
      mocks.theme = selectedTheme
      renderLayout()
      const theme = lastTheme()
      expect(theme.sidebar).toMatchObject({ width: 280, closedWidth: 55 })
      expect(theme.overrides.RaSidebar.drawerPaper.backgroundColor).toBe(
        '#0d0d0d !important',
      )
      expect(theme.overrides.RaSidebar.fixed.backgroundColor).toBe('#0d0d0d')
      expect(theme.overrides.RaSidebar.fixed.overflow).toBe('hidden')
      expect(theme.palette).toBe(selectedTheme.palette)
      expect(theme.overrides.RaAppBar).toBe(selectedTheme.overrides?.RaAppBar)
      expect(theme.overrides.RaMenuItemLink).toBe(
        selectedTheme.overrides?.RaMenuItemLink,
      )
      expect(selectedTheme.sidebar?.width).not.toBe(280)
    },
  )

  it('reserves the player and safe-area height and restores the space when the queue clears', () => {
    const store = renderLayout()
    expect(lastTheme().overrides.RaSidebar.fixed.height).toBe(
      'calc(100vh - 48px - 0px - env(safe-area-inset-bottom, 0px))',
    )
    act(
      () =>
        void store.dispatch({ type: 'TEST/QUEUE', queue: [{ id: 'song-1' }] }),
    )
    expect(lastTheme().overrides.RaSidebar.fixed.height).toBe(
      'calc(100vh - 48px - 80px - env(safe-area-inset-bottom, 0px))',
    )
    act(() => void store.dispatch({ type: 'TEST/QUEUE', queue: [] }))
    expect(lastTheme().overrides.RaSidebar.fixed.height).toBe(
      'calc(100vh - 48px - 0px - env(safe-area-inset-bottom, 0px))',
    )
  })

  it('uses the full mobile drawer height and dynamic viewport support', () => {
    renderLayout([{ id: 'song-1' }])
    const sidebar = lastTheme().overrides.RaSidebar
    const mobile = sidebar.drawerPaper['@media (max-width:599.95px)']
    expect(mobile.height).toBe(
      'calc(100vh - 0px - 80px - env(safe-area-inset-bottom, 0px))',
    )
    expect(mobile['@supports (height: 100dvh)'].height).toBe(
      'calc(100dvh - 0px - 80px - env(safe-area-inset-bottom, 0px))',
    )
    expect(sidebar.fixed['@supports (height: 100dvh)'].height).toBe(
      'calc(100dvh - 48px - 80px - env(safe-area-inset-bottom, 0px))',
    )
  })

  it('initializes libraries independently of the removed selector', () => {
    renderLayout()
    expect(mocks.libraries).toHaveBeenCalledOnce()
  })
})

describe('Quick Pick layout integration', () => {
  it.each(Object.entries(themes))(
    'uses the navigation black in %s and restores the theme when leaving the page',
    (name, selectedTheme) => {
      mocks.theme = selectedTheme
      const history = createMemoryHistory({ initialEntries: ['/quick-pick'] })
      renderLayout([], history)
      const theme = lastTheme()
      expect(theme.overrides.RaLayout.root.background).toBe(
        '#0d0d0d !important',
      )
      expect(theme.overrides.RaLayout.content.background).toBe(
        '#0d0d0d !important',
      )
      expect(theme.overrides.RaLayout.contentWithSidebar.background).toBe(
        '#0d0d0d !important',
      )
      expect(theme.overrides.RaLayout.content.padding).toBe('0 !important')
      expect(theme.palette).toBe(selectedTheme.palette)
      expect(theme.overrides.RaAppBar).toBe(selectedTheme.overrides?.RaAppBar)
      act(() => history.push('/playlist/pl-1/show'))
      expect(lastTheme().overrides.RaLayout).toBe(
        selectedTheme.overrides?.RaLayout,
      )
    },
  )

  it('preserves the player and safe-area spacing on the redesigned page', () => {
    const history = createMemoryHistory({ initialEntries: ['/quick-pick'] })
    const store = renderLayout([], history)
    const node = document.createElement('div')
    node.className = mocks.layout.mock.lastCall[0].className
    document.body.appendChild(node)
    expect(getComputedStyle(node).paddingBottom).toBe('0px')
    act(
      () =>
        void store.dispatch({ type: 'TEST/QUEUE', queue: [{ id: 'song-1' }] }),
    )
    node.className = mocks.layout.mock.lastCall[0].className
    // jsdom rearranges env() inside calc(); assert the reserved space and inset.
    expect(getComputedStyle(node).paddingBottom).toContain('80px')
    expect(getComputedStyle(node).paddingBottom).toContain(
      'safe-area-inset-bottom',
    )
    expect(lastTheme().overrides.RaLayout.root.background).toBe(
      '#0d0d0d !important',
    )
    act(() => void store.dispatch({ type: 'TEST/QUEUE', queue: [] }))
    node.className = mocks.layout.mock.lastCall[0].className
    expect(getComputedStyle(node).paddingBottom).toBe('0px')
    node.remove()
  })
})
