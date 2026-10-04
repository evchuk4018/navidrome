import React from 'react'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import { Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import { EVENT_REFRESH_RESOURCE } from '../actions'
import { activityReducer } from '../reducers/activityReducer'
import QuickPickPlaylists from './QuickPickPlaylists'
import { clearQuickPickCache } from './cache'

const mocks = vi.hoisted(() => ({
  artwork: vi.fn(),
  locale: 'en',
  dataProvider: { getList: vi.fn() },
}))

vi.mock('react-admin', () => ({
  useDataProvider: () => mocks.dataProvider,
  useLocale: () => mocks.locale,
  useTranslate: () => (key, options) => options._,
}))
vi.mock('../common/Artwork', () => ({
  Artwork: (props) => {
    mocks.artwork(props)
    return (
      <div data-testid={`art-${props.record.id}`} className={props.className} />
    )
  },
}))

const playlists = [
  { id: 'pl-z', name: 'Zulu', ownerId: 'user-1' },
  { id: 'pl-other', name: 'Other user', ownerId: 'user-2', public: true },
  { id: 'pl-liked', name: 'Liked Music', ownerId: 'user-1' },
  { id: 'pl-a', name: 'Alpha', ownerId: 'user-1', rules: { expression: {} } },
]

const renderPlaylists = (theme = createTheme()) => {
  const store = createStore(
    combineReducers({
      admin: (state = { resources: { playlist: { data: {} } } }, action) =>
        action.type === 'TEST/PLAYLIST_DATA'
          ? { resources: { playlist: { data: action.data } } }
          : state,
      activity: activityReducer,
    }),
  )
  const history = createMemoryHistory({ initialEntries: ['/quick-pick'] })
  const tree = () => (
    <Provider store={store}>
      <ThemeProvider theme={theme}>
        <Router history={history}>
          <QuickPickPlaylists />
        </Router>
      </ThemeProvider>
    </Provider>
  )
  const result = render(tree())
  return {
    ...result,
    store,
    history,
    rerenderList: () => result.rerender(tree()),
  }
}

beforeEach(() => {
  clearQuickPickCache()
  vi.restoreAllMocks()
  vi.clearAllMocks()
  mocks.locale = 'en'
  mocks.dataProvider.getList.mockResolvedValue({ data: playlists })
  localStorage.setItem('userId', 'user-1')
  localStorage.setItem('role', 'regular')
})

afterEach(cleanup)

describe('QuickPickPlaylists', () => {
  it.each(['regular', 'admin'])(
    'lists only owned playlists with the liked playlist first for a %s user',
    async (role) => {
      localStorage.setItem('role', role)
      renderPlaylists()
      await screen.findByRole('link', { name: 'Alpha' })
      expect(mocks.dataProvider.getList).toHaveBeenCalledWith('playlist', {
        pagination: { page: 1, perPage: 0 },
        sort: { field: 'name', order: 'ASC' },
        filter: { owner_id: 'user-1' },
      })
      expect(
        screen.getAllByRole('link').map((link) => link.getAttribute('href')),
      ).toEqual([
        '/playlist/pl-liked/show',
        '/playlist/pl-a/show',
        '/playlist/pl-z/show',
      ])
      expect(screen.getAllByText('Playlist')).toHaveLength(3)
      expect(screen.queryByText('Other user')).not.toBeInTheDocument()
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
      expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    },
  )

  it('opens the selected playlist page from the whole row', async () => {
    const { history } = renderPlaylists()
    fireEvent.click(await screen.findByRole('link', { name: 'Liked Music' }))
    expect(history.location.pathname).toBe('/playlist/pl-liked/show')
  })

  it('renders every owned playlist beyond the sidebar limit', async () => {
    const many = Array.from({ length: 125 }, (_, index) => ({
      id: `playlist-${index}`,
      name: `Playlist ${String(index).padStart(3, '0')}`,
      ownerId: 'user-1',
    }))
    mocks.dataProvider.getList.mockResolvedValue({ data: many })
    renderPlaylists()
    await screen.findByRole('link', { name: 'Playlist 000' })
    expect(screen.getAllByRole('link')).toHaveLength(125)
    expect(screen.getAllByRole('link').at(-1)).toHaveAttribute(
      'href',
      '/playlist/playlist-124/show',
    )
  })

  it('uses locale-aware ordering and playlist IDs to break matching names', async () => {
    mocks.locale = 'sv'
    mocks.dataProvider.getList.mockResolvedValue({
      data: [
        { id: 'b', name: 'Alpha', ownerId: 'user-1' },
        { id: 'a', name: 'alpha', ownerId: 'user-1' },
        { id: 'z', name: 'Zulu', ownerId: 'user-1' },
        { id: 'swedish', name: 'Älskade', ownerId: 'user-1' },
      ],
    })
    renderPlaylists()
    await screen.findByRole('link', { name: 'alpha' })
    expect(
      screen.getAllByRole('link').map((link) => link.getAttribute('href')),
    ).toEqual([
      '/playlist/a/show',
      '/playlist/b/show',
      '/playlist/z/show',
      '/playlist/swedish/show',
    ])
  })

  it('uses shared artwork and keeps a neutral placeholder when no image is available', async () => {
    renderPlaylists()
    const artwork = await screen.findByTestId('art-pl-z')
    expect(getComputedStyle(artwork.parentElement).backgroundColor).toBe(
      'rgb(36, 36, 36)',
    )
    expect(artwork.previousSibling).toHaveAttribute('aria-hidden', 'true')
    expect(mocks.artwork).toHaveBeenCalledWith(
      expect.objectContaining({
        record: playlists[0],
        size: 192,
        title: '',
      }),
    )
  })

  it.each(['light', 'dark'])(
    'uses navigation colors in a %s theme',
    async (type) => {
      renderPlaylists(
        createTheme({ palette: { type, primary: { main: '#0000ff' } } }),
      )
      const row = await screen.findByRole('link', { name: 'Zulu' })
      expect(getComputedStyle(row).color).toBe('rgb(245, 245, 245)')
      expect(getComputedStyle(screen.getByText('Zulu')).color).toBe(
        'rgb(245, 245, 245)',
      )
      expect(getComputedStyle(screen.getAllByText('Playlist')[0]).color).toBe(
        'rgb(146, 146, 146)',
      )
    },
  )

  it('shows a compact empty state without fabricated playlists', async () => {
    mocks.dataProvider.getList.mockResolvedValue({ data: [] })
    renderPlaylists()
    expect(await screen.findByText('No playlists yet.')).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('shows loading independently of discovery data', () => {
    mocks.dataProvider.getList.mockReturnValue(new Promise(() => {}))
    renderPlaylists()
    expect(
      screen.getByRole('progressbar', { name: 'Loading playlists' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('allows retrying a failed playlist query', async () => {
    mocks.dataProvider.getList.mockRejectedValueOnce(new Error('offline'))
    renderPlaylists()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Unable to load playlists.',
    )
    mocks.dataProvider.getList.mockResolvedValueOnce({ data: playlists })
    fireEvent.click(screen.getByRole('button', { name: 'Retry playlists' }))
    expect(
      await screen.findByRole('link', { name: 'Alpha' }),
    ).toBeInTheDocument()
    expect(mocks.dataProvider.getList).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('does not query when there is no authenticated user ID', () => {
    localStorage.clear()
    renderPlaylists()
    expect(mocks.dataProvider.getList).not.toHaveBeenCalled()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('freezes the records and ordering after the first successful response', async () => {
    const mutable = playlists.map((playlist) => ({ ...playlist }))
    mocks.dataProvider.getList.mockResolvedValue({ data: mutable })
    const first = renderPlaylists()
    await screen.findByRole('link', { name: 'Alpha' })
    mutable[0].name = 'Aardvark'
    mutable.push({ id: 'new', name: 'New playlist', ownerId: 'user-1' })
    first.unmount()
    renderPlaylists()
    expect(screen.getByRole('link', { name: 'Alpha' })).toBeInTheDocument()
    expect(
      screen.queryByRole('progressbar', { name: 'Loading playlists' }),
    ).not.toBeInTheDocument()
    expect(
      await screen.findByRole('link', { name: 'Alpha' }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'Aardvark' }),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'New playlist' }),
    ).not.toBeInTheDocument()
    expect(mocks.dataProvider.getList).toHaveBeenCalledOnce()
  })

  it('keeps the cached ordering after a locale change and remount', async () => {
    const localeSensitivePlaylists = [
      { id: 'pl-alpha', name: 'Alpha', ownerId: 'user-1' },
      { id: 'pl-swedish', name: 'Älskade', ownerId: 'user-1' },
      { id: 'pl-zulu', name: 'Zulu', ownerId: 'user-1' },
    ]
    mocks.dataProvider.getList.mockResolvedValue({
      data: localeSensitivePlaylists,
    })

    const expectedHrefs = [
      '/playlist/pl-alpha/show',
      '/playlist/pl-swedish/show',
      '/playlist/pl-zulu/show',
    ]
    const first = renderPlaylists()
    await screen.findByRole('link', { name: 'Alpha' })
    expect(
      screen.getAllByRole('link').map((link) => link.getAttribute('href')),
    ).toEqual(expectedHrefs)

    mocks.locale = 'sv'
    first.rerenderList()
    await vi.waitFor(() =>
      expect(
        screen.getAllByRole('link').map((link) => link.getAttribute('href')),
      ).toEqual(expectedHrefs),
    )

    first.unmount()
    renderPlaylists()
    expect(
      screen.getAllByRole('link').map((link) => link.getAttribute('href')),
    ).toEqual(expectedHrefs)
    expect(mocks.dataProvider.getList).toHaveBeenCalledOnce()
  })

  it('ignores resource-store mutations after the snapshot is cached', async () => {
    const { store } = renderPlaylists()
    await screen.findByRole('link', { name: 'Alpha' })
    act(
      () =>
        void store.dispatch({
          type: 'TEST/PLAYLIST_DATA',
          data: {
            'pl-a': { ...playlists[3], name: 'Aardvark' },
            unrelated: {
              id: 'unrelated',
              name: 'Unrelated',
              ownerId: 'user-1',
            },
          },
        }),
    )
    expect(screen.getByRole('link', { name: 'Alpha' })).toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'Aardvark' }),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'Unrelated' }),
    ).not.toBeInTheDocument()
    expect(mocks.dataProvider.getList).toHaveBeenCalledOnce()
  })

  it.each([{ playlist: ['pl-z'] }, { '*': '*' }])(
    'ignores playlist refresh events after the snapshot is cached: %j',
    async (resources) => {
      const { store } = renderPlaylists()
      await screen.findByRole('link', { name: 'Alpha' })
      act(
        () =>
          void store.dispatch({
            type: EVENT_REFRESH_RESOURCE,
            data: resources,
          }),
      )
      expect(screen.getByRole('link', { name: 'Alpha' })).toBeInTheDocument()
      expect(mocks.dataProvider.getList).toHaveBeenCalledOnce()
    },
  )

  it('ignores unrelated refresh events as well', async () => {
    const { store } = renderPlaylists()
    await screen.findByRole('link', { name: 'Alpha' })
    act(
      () =>
        void store.dispatch({
          type: EVENT_REFRESH_RESOURCE,
          data: { song: ['song-1'] },
        }),
    )
    expect(screen.getByRole('link', { name: 'Alpha' })).toBeInTheDocument()
    expect(mocks.dataProvider.getList).toHaveBeenCalledOnce()
  })

  it('shares a pending request when remounted before it resolves', async () => {
    let resolve
    mocks.dataProvider.getList.mockReturnValue(
      new Promise((result) => {
        resolve = result
      }),
    )
    const first = renderPlaylists()
    first.unmount()
    renderPlaylists()
    await vi.waitFor(() =>
      expect(mocks.dataProvider.getList).toHaveBeenCalledOnce(),
    )
    resolve({ data: playlists })
    expect(
      await screen.findByRole('link', { name: 'Alpha' }),
    ).toBeInTheDocument()
  })

  it('keeps an empty successful response cached until the cache is cleared', async () => {
    mocks.dataProvider.getList.mockResolvedValueOnce({ data: [] })
    renderPlaylists()
    expect(await screen.findByText('No playlists yet.')).toBeInTheDocument()
    mocks.dataProvider.getList.mockResolvedValueOnce({ data: playlists })
    cleanup()
    renderPlaylists()
    expect(await screen.findByText('No playlists yet.')).toBeInTheDocument()
    expect(mocks.dataProvider.getList).toHaveBeenCalledOnce()
  })

  it('uses a separate cache entry after the authenticated user changes', async () => {
    mocks.dataProvider.getList.mockImplementation((resource, { filter }) =>
      Promise.resolve({
        data:
          filter.owner_id === 'user-2'
            ? [{ id: 'other', name: 'Other playlist', ownerId: 'user-2' }]
            : playlists,
      }),
    )
    const rendered = renderPlaylists()
    await screen.findByRole('link', { name: 'Alpha' })
    localStorage.setItem('userId', 'user-2')
    rendered.rerenderList()
    expect(
      await screen.findByRole('link', { name: 'Other playlist' }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'Alpha' }),
    ).not.toBeInTheDocument()
    expect(mocks.dataProvider.getList).toHaveBeenCalledTimes(2)
  })
})
