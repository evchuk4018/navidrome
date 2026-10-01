import React from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import { Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import { EVENT_REFRESH_RESOURCE } from '../actions'
import { activityReducer } from '../reducers/activityReducer'
import QuickPickPlaylists from './QuickPickPlaylists'

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  refetch: vi.fn(),
  artwork: vi.fn(),
  locale: 'en',
}))

vi.mock('react-admin', () => ({
  useQueryWithStore: (...args) => mocks.query(...args),
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
  { id: 'pl-liked', name: 'Liked Songs', ownerId: 'user-1' },
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
  vi.restoreAllMocks()
  vi.clearAllMocks()
  mocks.locale = 'en'
  mocks.query.mockReturnValue({
    data: playlists,
    loaded: true,
    refetch: mocks.refetch,
  })
  localStorage.setItem('userId', 'user-1')
  localStorage.setItem('role', 'regular')
  vi.spyOn(Date, 'now').mockReturnValue(1000)
})

it.each(['regular', 'admin'])(
  'lists only owned playlists alphabetically for a %s user',
  (role) => {
    localStorage.setItem('role', role)
    renderPlaylists()
    expect(mocks.query).toHaveBeenCalledWith(
      {
        type: 'getList',
        resource: 'playlist',
        payload: {
          pagination: { page: 1, perPage: 0 },
          sort: { field: 'name', order: 'ASC' },
          filter: { owner_id: 'user-1' },
        },
      },
      { action: 'CUSTOM_QUERY', enabled: true },
    )
    expect(
      screen.getAllByRole('link').map((link) => link.getAttribute('href')),
    ).toEqual([
      '/playlist/pl-a/show',
      '/playlist/pl-liked/show',
      '/playlist/pl-z/show',
    ])
    expect(screen.getAllByText('Playlist')).toHaveLength(3)
    expect(screen.queryByText('Other user')).not.toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
  },
)

it('opens the selected playlist page from the whole row', () => {
  const { history } = renderPlaylists()
  fireEvent.click(screen.getByRole('link', { name: 'Liked Songs' }))
  expect(history.location.pathname).toBe('/playlist/pl-liked/show')
})

it('renders every owned playlist beyond the sidebar limit', () => {
  const many = Array.from({ length: 125 }, (_, index) => ({
    id: `playlist-${index}`,
    name: `Playlist ${String(index).padStart(3, '0')}`,
    ownerId: 'user-1',
  }))
  mocks.query.mockReturnValue({
    data: many,
    loaded: true,
    refetch: mocks.refetch,
  })
  renderPlaylists()
  expect(screen.getAllByRole('link')).toHaveLength(125)
  expect(screen.getAllByRole('link').at(-1)).toHaveAttribute(
    'href',
    '/playlist/playlist-124/show',
  )
})

it('uses locale-aware ordering and playlist IDs to break matching names', () => {
  mocks.locale = 'sv'
  mocks.query.mockReturnValue({
    data: [
      { id: 'b', name: 'Alpha', ownerId: 'user-1' },
      { id: 'a', name: 'alpha', ownerId: 'user-1' },
      { id: 'z', name: 'Zulu', ownerId: 'user-1' },
      { id: 'swedish', name: 'Älskade', ownerId: 'user-1' },
    ],
    loaded: true,
    refetch: mocks.refetch,
  })
  renderPlaylists()
  expect(
    screen.getAllByRole('link').map((link) => link.getAttribute('href')),
  ).toEqual([
    '/playlist/a/show',
    '/playlist/b/show',
    '/playlist/z/show',
    '/playlist/swedish/show',
  ])
})

it('uses shared artwork and keeps a neutral placeholder when no image is available', () => {
  renderPlaylists()
  const artwork = screen.getByTestId('art-pl-z')
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

it.each(['light', 'dark'])('uses navigation colors in a %s theme', (type) => {
  renderPlaylists(
    createTheme({ palette: { type, primary: { main: '#0000ff' } } }),
  )
  const row = screen.getByRole('link', { name: 'Zulu' })
  expect(getComputedStyle(row).color).toBe('rgb(245, 245, 245)')
  expect(getComputedStyle(screen.getByText('Zulu')).color).toBe(
    'rgb(245, 245, 245)',
  )
  expect(getComputedStyle(screen.getAllByText('Playlist')[0]).color).toBe(
    'rgb(146, 146, 146)',
  )
})

it('shows a compact empty state without fabricated playlists', () => {
  mocks.query.mockReturnValue({
    data: [],
    loaded: true,
    refetch: mocks.refetch,
  })
  renderPlaylists()
  expect(screen.getByText('No playlists yet.')).toBeInTheDocument()
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
})

it('shows loading independently of discovery data', () => {
  mocks.query.mockReturnValue({ loaded: false, refetch: mocks.refetch })
  renderPlaylists()
  expect(
    screen.getByRole('progressbar', { name: 'Loading playlists' }),
  ).toBeInTheDocument()
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
})

it('allows retrying a failed playlist query', () => {
  mocks.query.mockReturnValue({
    error: new Error('offline'),
    loaded: false,
    refetch: mocks.refetch,
  })
  const { rerenderList } = renderPlaylists()
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Unable to load playlists.',
  )
  fireEvent.click(screen.getByRole('button', { name: 'Retry playlists' }))
  expect(mocks.refetch).toHaveBeenCalledOnce()
  mocks.query.mockReturnValue({
    data: playlists,
    loaded: true,
    refetch: mocks.refetch,
  })
  rerenderList()
  expect(screen.getByRole('link', { name: 'Alpha' })).toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

it('disables the query if there is no authenticated user ID', () => {
  localStorage.clear()
  renderPlaylists()
  expect(mocks.query).toHaveBeenCalledWith(expect.anything(), {
    action: 'CUSTOM_QUERY',
    enabled: false,
  })
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
})

it('reflects local names and artwork without adding unrelated cached records', () => {
  const { store } = renderPlaylists()
  const updated = { ...playlists[0], name: 'Aardvark', imageUrl: 'new-cover' }
  act(
    () =>
      void store.dispatch({
        type: 'TEST/PLAYLIST_DATA',
        data: {
          'pl-z': updated,
          unrelated: { id: 'unrelated', name: 'Unrelated', ownerId: 'user-1' },
        },
      }),
  )
  expect(screen.getAllByRole('link')[0]).toHaveAccessibleName('Aardvark')
  expect(screen.queryByText('Unrelated')).not.toBeInTheDocument()
  expect(mocks.artwork).toHaveBeenCalledWith(
    expect.objectContaining({ record: updated }),
  )
})

it('removes a playlist whose ownership changes in the local store', () => {
  const { store } = renderPlaylists()
  act(
    () =>
      void store.dispatch({
        type: 'TEST/PLAYLIST_DATA',
        data: { 'pl-z': { ...playlists[0], ownerId: 'user-2' } },
      }),
  )
  expect(screen.queryByRole('link', { name: 'Zulu' })).not.toBeInTheDocument()
})

it.each([{ playlist: ['pl-z'] }, { '*': '*' }])(
  'refreshes for playlist or global events: %j',
  (resources) => {
    const { store } = renderPlaylists()
    vi.mocked(Date.now).mockReturnValue(2000)
    act(
      () =>
        void store.dispatch({ type: EVENT_REFRESH_RESOURCE, data: resources }),
    )
    expect(mocks.refetch).toHaveBeenCalledOnce()
  },
)

it('does not reload for unrelated resource events', () => {
  const { store } = renderPlaylists()
  vi.mocked(Date.now).mockReturnValue(2000)
  act(
    () =>
      void store.dispatch({
        type: EVENT_REFRESH_RESOURCE,
        data: { song: ['song-1'] },
      }),
  )
  expect(mocks.refetch).not.toHaveBeenCalled()
})
