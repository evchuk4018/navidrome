import React from 'react'
import { act, render, screen } from '@testing-library/react'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import { Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { activityReducer } from '../reducers/activityReducer'
import { settingsReducer } from '../reducers/settingsReducer'
import { EVENT_REFRESH_RESOURCE } from '../actions'
import { DraggableTypes } from '../consts'
import SidebarPlaylists from './SidebarPlaylists'

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  refetch: vi.fn(),
  addToPlaylist: vi.fn(),
  notify: vi.fn(),
  artwork: vi.fn(),
  drops: new Map(),
  locale: 'en',
}))
const dataProvider = { addToPlaylist: mocks.addToPlaylist }

vi.mock('../config', () => ({ default: { maxSidebarPlaylists: 100 } }))
vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useQueryWithStore: (query) => mocks.query(query),
  useDataProvider: () => dataProvider,
  useNotify: () => mocks.notify,
  useLocale: () => mocks.locale,
  useTranslate: () => (key, options) =>
    key === 'resources.song.name'
      ? mocks.locale === 'de'
        ? options.smart_count === 1
          ? 'Lied'
          : 'Lieder'
        : options.smart_count === 1
          ? 'Song'
          : 'Songs'
      : key,
}))
vi.mock('../common/Artwork', () => ({
  Artwork: (props) => {
    mocks.artwork(props)
    return (
      <div data-testid={`art-${props.record.id}`} className={props.className} />
    )
  },
}))
vi.mock('react-dnd', () => ({
  useDrop: (factory) => {
    const spec = factory()
    return [
      {},
      (node) => {
        if (node) mocks.drops.set(node.getAttribute('href'), spec)
      },
    ]
  },
}))

const playlists = {
  'pl-1': {
    id: 'pl-1',
    name: 'Zulu',
    ownerId: 'user-1',
    songCount: 0,
    sync: false,
  },
  'pl-2': {
    id: 'pl-2',
    name: 'Alpha',
    ownerId: 'user-2',
    songCount: 1,
    sync: false,
  },
  'pl-3': {
    id: 'pl-3',
    name: 'Baby doll',
    ownerId: 'user-1',
    songCount: 1024,
    sync: false,
    starred: true,
  },
}

const renderPreview = ({ visibleCount = 10, path = '/quick-pick' } = {}) => {
  const store = createStore(
    combineReducers({
      admin: (
        state = {
          ui: { sidebarOpen: true },
          resources: { playlist: { data: {} } },
        },
        action,
      ) =>
        action.type === 'TEST/PLAYLIST_DATA'
          ? { ...state, resources: { playlist: { data: action.data } } }
          : state,
      activity: activityReducer,
      settings: settingsReducer,
    }),
    { settings: { sidebarPlaylistsOnlyFavourites: true } },
  )
  const history = createMemoryHistory({ initialEntries: [path] })
  const theme = createTheme({ props: { MuiUseMediaQuery: { noSsr: true } } })
  const tree = (count) => (
    <Provider store={store}>
      <ThemeProvider theme={theme}>
        <Router history={history}>
          <SidebarPlaylists visibleCount={count} />
        </Router>
      </ThemeProvider>
    </Provider>
  )
  const result = render(tree(visibleCount))
  return {
    ...result,
    store,
    rerenderCount: (count) => result.rerender(tree(count)),
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  mocks.locale = 'en'
  mocks.drops.clear()
  mocks.query.mockReturnValue({
    data: playlists,
    loaded: true,
    refetch: mocks.refetch,
  })
  mocks.addToPlaylist.mockResolvedValue({ data: { added: 2 } })
  vi.spyOn(Date, 'now').mockReturnValue(1000)
  localStorage.setItem('userId', 'user-1')
  localStorage.setItem('role', 'user')
  window.matchMedia = (query) => ({
    matches: false,
    media: query,
    addListener: vi.fn(),
    removeListener: vi.fn(),
  })
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
})

describe('playlist preview', () => {
  it('combines owned and shared playlists in the default order without changing saved preferences', () => {
    const { store } = renderPreview()
    expect(
      screen.getAllByRole('link').map((link) => link.getAttribute('href')),
    ).toEqual([
      '/playlist/pl-2/show',
      '/playlist/pl-3/show',
      '/playlist/pl-1/show',
    ])
    expect(mocks.query).toHaveBeenCalledWith({
      type: 'getList',
      resource: 'playlist',
      payload: {
        pagination: { page: 1, perPage: 100 },
        sort: { field: 'liked_songs_first', order: 'ASC' },
        filter: {},
      },
    })
    expect(store.getState().settings.sidebarPlaylistsOnlyFavourites).toBe(true)
    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })

  it('keeps the current user’s liked playlist inside the visible cap', () => {
    mocks.query.mockReturnValue({
      data: {
        ...playlists,
        'pl-liked': {
          id: 'pl-liked',
          name: '  LiKeD MuSiC  ',
          ownerId: 'user-1',
          songCount: 12,
        },
      },
      loaded: true,
      refetch: mocks.refetch,
    })

    renderPreview({ visibleCount: 1 })

    expect(screen.getAllByRole('link')).toHaveLength(1)
    expect(screen.getByRole('link')).toHaveAttribute(
      'href',
      '/playlist/pl-liked/show',
    )
  })

  it('renders localized zero, singular, and thousands counts', () => {
    renderPreview()
    expect(screen.getByText('0 songs')).toBeInTheDocument()
    expect(screen.getByText('1 song')).toBeInTheDocument()
    expect(screen.getByText('1,024 songs')).toBeInTheDocument()
  })

  it('uses the selected locale for numbers and song labels', () => {
    mocks.locale = 'de'
    renderPreview()
    expect(screen.getByText('1.024 lieder')).toBeInTheDocument()
    expect(screen.getByText('1 lied')).toBeInTheDocument()
  })

  it('only mounts visible links and artwork, and unmounts rows when capacity shrinks', () => {
    const { rerenderCount } = renderPreview({ visibleCount: 2 })
    expect(screen.getAllByRole('link')).toHaveLength(2)
    expect(screen.queryByText('Zulu')).not.toBeInTheDocument()
    expect(
      mocks.artwork.mock.calls.map(([props]) => props.record.id),
    ).not.toContain('pl-1')
    rerenderCount(1)
    expect(screen.getAllByRole('link')).toHaveLength(1)
    expect(screen.queryByText('Baby doll')).not.toBeInTheDocument()
    expect(screen.queryByTestId('art-pl-3')).not.toBeInTheDocument()
  })

  it('uses the shared artwork with a fixed-size fallback box', () => {
    renderPreview()
    expect(mocks.artwork).toHaveBeenCalledWith(
      expect.objectContaining({
        record: playlists['pl-2'],
        size: 108,
        title: '',
      }),
    )
    expect(screen.getByTestId('art-pl-2')).toHaveStyle({
      width: '54px',
      height: '54px',
      borderRadius: '6px',
      backgroundColor: '#242024',
    })
  })

  it('marks the open playlist row as active', () => {
    renderPreview({ path: '/playlist/pl-3/show' })
    expect(screen.getByRole('link', { name: /Baby doll/ })).toHaveAttribute(
      'aria-current',
      'page',
    )
  })

  it.each([
    { data: {}, loaded: true },
    { data: undefined, loaded: false },
    { data: playlists, loaded: true, error: new Error('Unavailable') },
  ])(
    'does not add controls for an empty, loading, or failed query',
    (response) => {
      mocks.query.mockReturnValue({ ...response, refetch: mocks.refetch })
      const { container } = renderPreview()
      expect(container).toBeEmptyDOMElement()
    },
  )

  it('shows no off-screen links when capacity is zero', () => {
    renderPreview({ visibleCount: 0 })
    expect(screen.queryAllByRole('link')).toHaveLength(0)
    expect(mocks.artwork).not.toHaveBeenCalled()
  })

  it('uses local record updates without including unrelated cached playlists', () => {
    const { store } = renderPreview()
    act(
      () =>
        void store.dispatch({
          type: 'TEST/PLAYLIST_DATA',
          data: {
            'pl-1': { ...playlists['pl-1'], name: 'A new name', songCount: 44 },
            'pl-99': { id: 'pl-99', name: 'Outside the query', songCount: 7 },
          },
        }),
    )
    expect(screen.getByText('A new name')).toBeInTheDocument()
    expect(screen.getByText('44 songs')).toBeInTheDocument()
    expect(screen.getAllByRole('link')[0]).toHaveAttribute(
      'href',
      '/playlist/pl-1/show',
    )
    expect(screen.queryByText('Outside the query')).not.toBeInTheDocument()
  })

  it.each([{ playlist: ['pl-2'] }, { '*': '*' }])(
    'refetches for playlist and global events: %j',
    (resources) => {
      const { store } = renderPreview()
      vi.mocked(Date.now).mockReturnValue(2000)
      act(
        () =>
          void store.dispatch({
            type: EVENT_REFRESH_RESOURCE,
            data: resources,
          }),
      )
      expect(mocks.refetch).toHaveBeenCalledOnce()
    },
  )

  it('does not refetch for unrelated events', () => {
    const { store } = renderPreview()
    vi.mocked(Date.now).mockReturnValue(2000)
    act(
      () =>
        void store.dispatch({
          type: EVENT_REFRESH_RESOURCE,
          data: { album: ['al-1'] },
        }),
    )
    expect(mocks.refetch).not.toHaveBeenCalled()
  })
})

describe('playlist drop targets', () => {
  it('keeps writable playlists accepting tracks and refreshes after a successful drop', async () => {
    renderPreview()
    const spec = mocks.drops.get('/playlist/pl-1/show')
    expect(spec.accept).toEqual(DraggableTypes.ALL)
    const item = { ids: ['song-1', 'song-2'] }
    await act(() => spec.drop(item))
    expect(mocks.addToPlaylist).toHaveBeenCalledWith('pl-1', item)
    expect(mocks.notify).toHaveBeenCalledWith(
      'message.songsAddedToPlaylist',
      'info',
      { smart_count: 2 },
    )
    expect(mocks.refetch).toHaveBeenCalledOnce()
  })

  it('blocks writes to read-only shared playlists', async () => {
    renderPreview()
    const spec = mocks.drops.get('/playlist/pl-2/show')
    expect(spec.accept).toEqual([])
    await act(() => spec.drop({ ids: ['song-1'] }))
    expect(mocks.addToPlaylist).not.toHaveBeenCalled()
  })

  it('blocks writes to smart playlists', async () => {
    mocks.query.mockReturnValue({
      data: [{ ...playlists['pl-1'], rules: { expression: {} } }],
      loaded: true,
      refetch: mocks.refetch,
    })
    renderPreview()
    const spec = mocks.drops.get('/playlist/pl-1/show')
    expect(spec.accept).toEqual([])
    await act(() => spec.drop({ ids: ['song-1'] }))
    expect(mocks.addToPlaylist).not.toHaveBeenCalled()
  })

  it('retains administrator write access to shared playlists', () => {
    localStorage.setItem('role', 'admin')
    renderPreview()
    expect(mocks.drops.get('/playlist/pl-2/show').accept).toEqual(
      DraggableTypes.ALL,
    )
  })

  it('keeps the existing warning on a failed drop and does not refresh', async () => {
    mocks.addToPlaylist.mockRejectedValue(new Error('Failed'))
    renderPreview()
    await act(() =>
      mocks.drops.get('/playlist/pl-1/show').drop({ ids: ['song-1'] }),
    )
    expect(mocks.notify).toHaveBeenCalledWith('ra.page.error', 'warning')
    expect(mocks.refetch).not.toHaveBeenCalled()
  })
})
