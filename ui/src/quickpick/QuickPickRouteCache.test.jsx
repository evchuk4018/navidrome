import React from 'react'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { Route, Router, Switch } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import QuickPick from './QuickPick'
import { clearQuickPickCache } from './cache'

const mocks = vi.hoisted(() => ({
  getQuickPick: vi.fn(),
  getList: vi.fn(),
  dataProvider: null,
  recordQuickPickImpressions: vi.fn(),
  recordQuickPickClick: vi.fn(),
  startRelatedRadio: vi.fn(),
  notify: vi.fn(),
  dispatch: vi.fn(),
}))

vi.mock('react-admin', () => ({
  useDataProvider: () => mocks.dataProvider,
  useLocale: () => 'en',
  useNotify: () => mocks.notify,
  useTranslate: () => (key, options) => options._,
}))
vi.mock('react-redux', () => ({ useDispatch: () => mocks.dispatch }))
vi.mock('../common/Artwork', () => ({ Artwork: () => null }))
vi.mock('./provider', () => ({
  getQuickPick: mocks.getQuickPick,
  recordQuickPickImpressions: mocks.recordQuickPickImpressions,
  recordQuickPickClick: mocks.recordQuickPickClick,
}))
vi.mock('./startRelatedRadio', () => ({
  startRelatedRadio: mocks.startRelatedRadio,
}))

const grid = (viewId = 'view-1') => ({
  viewId,
  items: Array.from({ length: 12 }, (_, index) => ({
    kind: 'song',
    section: index < 3 ? 'listen_again' : 'start_radio',
    viewId,
    itemKey: `track:song-${index}`,
    song: {
      id: `song-${index}`,
      title: `Song ${index}`,
      artist: `Artist ${index}`,
    },
  })),
})

const renderQuickPickRoute = () => {
  const history = createMemoryHistory({ initialEntries: ['/quick-pick'] })
  const result = render(
    <ThemeProvider theme={createTheme()}>
      <Router history={history}>
        <Switch>
          <Route path="/quick-pick" component={QuickPick} />
          <Route path="/search">
            <div data-testid="search-route" />
          </Route>
        </Switch>
      </Router>
    </ThemeProvider>,
  )
  return { ...result, history }
}

describe('Quick Pick route cache', () => {
  beforeEach(() => {
    clearQuickPickCache()
    vi.resetAllMocks()
    mocks.dataProvider = { getList: mocks.getList }
    mocks.getQuickPick.mockResolvedValue(grid())
    mocks.getList.mockResolvedValue({
      data: [{ id: 'playlist-1', name: 'Alpha', ownerId: 'user-1' }],
    })
    mocks.recordQuickPickImpressions.mockResolvedValue({})
    mocks.recordQuickPickClick.mockResolvedValue({})
    localStorage.setItem('userId', 'user-1')
  })

  afterEach(cleanup)

  it('returns from another route with the same discovery and playlist snapshots', async () => {
    const first = renderQuickPickRoute()
    await screen.findByRole('button', { name: 'Play Song 3 radio' })
    await screen.findByRole('link', { name: 'Alpha' })
    const songs = screen
      .getAllByRole('button')
      .map((button) => button.getAttribute('aria-label'))
    const playlists = screen
      .getAllByRole('link')
      .map((link) => link.getAttribute('href'))

    act(() => first.history.push('/search'))
    expect(screen.getByTestId('search-route')).toBeInTheDocument()
    act(() => first.history.push('/quick-pick'))

    expect(
      screen.getByRole('button', { name: 'Play Song 3 radio' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Alpha' })).toBeInTheDocument()
    expect(
      screen.queryByRole('progressbar', { name: 'Loading discoveries' }),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('progressbar', { name: 'Loading playlists' }),
    ).not.toBeInTheDocument()
    expect(
      screen
        .getAllByRole('button')
        .map((button) => button.getAttribute('aria-label')),
    ).toEqual(songs)
    expect(
      screen.getAllByRole('link').map((link) => link.getAttribute('href')),
    ).toEqual(playlists)
    expect(mocks.getQuickPick).toHaveBeenCalledOnce()
    expect(mocks.getList).toHaveBeenCalledOnce()
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledOnce()
  })

  it('refreshes both sections only after a full page cache reset', async () => {
    const first = renderQuickPickRoute()
    await screen.findByRole('button', { name: 'Play Song 3 radio' })
    await screen.findByRole('link', { name: 'Alpha' })
    clearQuickPickCache()
    first.unmount()
    mocks.getQuickPick.mockResolvedValueOnce(grid('view-2'))
    mocks.getList.mockResolvedValueOnce({
      data: [{ id: 'playlist-2', name: 'Beta', ownerId: 'user-1' }],
    })
    renderQuickPickRoute()
    expect(
      await screen.findByRole('button', { name: 'Play Song 3 radio' }),
    ).toBeInTheDocument()
    expect(
      await screen.findByRole('link', { name: 'Beta' }),
    ).toBeInTheDocument()
    expect(mocks.getQuickPick).toHaveBeenCalledTimes(2)
    expect(mocks.getList).toHaveBeenCalledTimes(2)
  })

  it('retries only a failed playlist section while keeping discovery cached', async () => {
    mocks.getList.mockRejectedValueOnce(new Error('offline'))
    renderQuickPickRoute()
    await screen.findByRole('button', { name: 'Play Song 3 radio' })
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Unable to load playlists.',
    )
    mocks.getList.mockResolvedValueOnce({
      data: [{ id: 'playlist-1', name: 'Alpha', ownerId: 'user-1' }],
    })
    fireEvent.click(screen.getByRole('button', { name: 'Retry playlists' }))
    expect(
      await screen.findByRole('link', { name: 'Alpha' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Play Song 3 radio' }),
    ).toBeInTheDocument()
    expect(mocks.getQuickPick).toHaveBeenCalledOnce()
    expect(mocks.getList).toHaveBeenCalledTimes(2)
  })
})
