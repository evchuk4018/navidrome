import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createHashHistory } from 'history'
import { Route, Router, Switch } from 'react-router-dom'
import { vi } from 'vitest'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import { playerReducer } from '../reducers/playerReducer'
import MusicSearch from './MusicSearch'

const mocks = vi.hoisted(() => ({
  search: vi.fn(),
  jobs: [],
}))

vi.mock('./provider', () => ({
  search: mocks.search,
}))

vi.mock('./useDownloadJobs', () => ({
  useDownloadJobs: () => ({ jobs: mocks.jobs, refreshJobs: vi.fn() }),
}))

const recentSongsKey = (userId) =>
  `navidrome.externalMusic.recentSongs.v1:${userId}`

const searchResults = (overrides = {}) => ({
  artists: [],
  albums: [],
  songs: [],
  genres: [],
  ...overrides,
})

const submitSearch = (value) => {
  const input = screen.getByPlaceholderText('Search artists, albums, songs…')
  fireEvent.change(input, { target: { value } })
  fireEvent.submit(input.closest('form'))
}

const renderSearch = (route = '/search') => {
  window.history.replaceState(null, '', `/navidrome/#${route}`)
  const history = createHashHistory()
  const store = createStore(combineReducers({ player: playerReducer }))
  const app = () => (
    <Provider store={store}>
      <Router history={history}>
        <Switch>
          <Route exact path="/search" component={MusicSearch} />
          <Route
            exact
            path="/search/artist/:id"
            render={() => <p>Artist details</p>}
          />
          <Route
            exact
            path="/search/album/:id"
            render={() => <p>Album details</p>}
          />
          <Route render={() => <p>Not Found</p>} />
        </Switch>
      </Router>
    </Provider>
  )
  const view = render(app())
  return {
    history,
    store,
    rerenderSearch: () => view.rerender(app()),
    ...view,
  }
}

describe('<MusicSearch />', () => {
  beforeEach(() => {
    mocks.search.mockReset()
    mocks.jobs = []
    localStorage.clear()
  })

  it('requests playback for a downloaded song without changing the route', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        songs: [
          {
            id: 'catalog-song',
            localMediaFileId: 'local-song',
            title: 'Ready Song',
            artistName: 'Artist',
          },
        ],
      }),
    )
    const { history, store } = renderSearch()
    submitSearch('Ready Song')
    fireEvent.click(
      await screen.findByRole('button', { name: 'Play Ready Song' }),
    )

    expect(store.getState().player.pendingSearchPlay).toEqual(
      expect.objectContaining({
        sourceId: 'catalog-song',
        localMediaFileId: 'local-song',
      }),
    )
    expect(history.location.pathname).toBe('/search')
    history.push('/search/artist/example')
    expect(store.getState().player.pendingSearchPlay.sourceId).toBe(
      'catalog-song',
    )
  })

  it('offers Play for a queued catalog song', async () => {
    mocks.jobs = [
      { id: 'job-1', kind: 'song', sourceId: 'catalog-song', status: 'queued' },
    ]
    mocks.search.mockResolvedValue(
      searchResults({
        songs: [{ id: 'catalog-song', title: 'Waiting Song' }],
      }),
    )
    const { store } = renderSearch()
    submitSearch('Waiting Song')
    fireEvent.click(
      await screen.findByRole('button', { name: 'Play Waiting Song' }),
    )
    expect(store.getState().player.pendingSearchPlay.sourceId).toBe(
      'catalog-song',
    )
  })

  it('preserves the mixed server result order', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        results: [
          {
            kind: 'song',
            song: {
              id: 'the-one-that-got-away',
              title: 'The One That Got Away',
              artistName: 'Katy Perry',
            },
          },
          {
            kind: 'artist',
            artist: { id: 'unrelated', name: 'The Boy That Got Away' },
          },
        ],
      }),
    )

    renderSearch()
    submitSearch('The one that got away')

    const songHeading = await screen.findByRole('heading', {
      name: 'The One That Got Away',
    })
    const artistHeading = await screen.findByRole('heading', {
      name: 'The Boy That Got Away',
    })

    expect(
      songHeading.compareDocumentPosition(artistHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(screen.getByText('Katy Perry')).toBeInTheDocument()
  })

  it('keeps artists before songs for an exact artist search', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        results: [
          { kind: 'artist', artist: { id: 'katy-perry', name: 'Katy Perry' } },
          {
            kind: 'song',
            song: { id: 'roar', title: 'Roar', artistName: 'Katy Perry' },
          },
        ],
      }),
    )

    renderSearch()
    submitSearch('Katy Perry')

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Roar' })).toBeInTheDocument(),
    )
    const artistsHeading = screen.getByRole('heading', { name: 'Katy Perry' })
    const songsHeading = screen.getByRole('heading', { name: 'Roar' })

    expect(
      artistsHeading.compareDocumentPosition(songsHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it('renders the cover URL returned for a song result', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        songs: [
          {
            id: 'covered-song',
            title: 'Covered Song',
            artistName: 'Artist',
            imageUrl: 'https://coverartarchive.org/covered-song.jpg',
          },
        ],
      }),
    )

    const { container } = renderSearch()
    submitSearch('Covered Song')

    await screen.findByText('Artist')
    expect(
      container.querySelector(
        'img[src="https://coverartarchive.org/covered-song.jpg"]',
      ),
    ).toBeInTheDocument()
  })

  it('merges a downloaded catalog song with local-only songs in the mixed list', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        results: [
          {
            kind: 'song',
            song: {
              id: 'catalog-owned',
              source: 'catalog',
              localMediaFileId: 'local-owned',
              title: 'Talking Body',
              artistName: 'Tove Lo',
            },
          },
          {
            kind: 'song',
            song: {
              id: 'local-live',
              source: 'library',
              localMediaFileId: 'local-live',
              title: 'Talking Body (Live)',
              artistName: 'Tove Lo',
            },
          },
          {
            kind: 'song',
            song: {
              id: 'catalog-new',
              source: 'catalog',
              title: 'New Song',
              artistName: 'Tove Lo',
            },
          },
        ],
      }),
    )

    renderSearch()
    submitSearch('Talking Body')

    await screen.findByRole('heading', { name: 'Talking Body' })
    expect(screen.queryByText('Download catalog-owned')).not.toBeInTheDocument()
    expect(screen.queryByText('Download local-live')).not.toBeInTheDocument()
    expect(screen.queryByText('Download catalog-new')).not.toBeInTheDocument()
    expect(
      screen.getAllByRole('heading', { name: 'Talking Body' }),
    ).toHaveLength(1)
  })

  it('withholds song downloads when library status is unavailable', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        partial: true,
        degradedSources: ['library'],
        results: [
          {
            kind: 'song',
            song: {
              id: 'unknown',
              title: 'Unknown Song',
              artistName: 'Artist',
            },
          },
        ],
      }),
    )

    renderSearch()
    submitSearch('Unknown Song')

    expect(
      await screen.findByText('Library status unavailable'),
    ).toBeInTheDocument()
    expect(screen.queryByText('Download unknown')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Play Unknown Song' }),
    ).toBeDisabled()
  })

  it('skips malformed live entity IDs while retaining genre refinements', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        results: [
          { kind: 'artist', artist: { id: null, name: 'Malformed Artist' } },
          { kind: 'album', album: { id: {}, title: 'Malformed Album' } },
          { kind: 'song', song: { id: '', title: 'Malformed Song' } },
          { kind: 'genre', genre: { name: 'Rock' } },
        ],
      }),
    )

    renderSearch()
    submitSearch('malformed')

    expect(await screen.findByText('Rock')).toBeInTheDocument()
    expect(screen.queryByText('Malformed Artist')).not.toBeInTheDocument()
    expect(screen.queryByText('Malformed Album')).not.toBeInTheDocument()
    expect(screen.queryByText('Malformed Song')).not.toBeInTheDocument()
  })

  it('keeps a submitted search on the hash route under the base path', async () => {
    mocks.search.mockResolvedValue(searchResults())
    const { history } = renderSearch()

    submitSearch('Katy Perry')

    await waitFor(() => expect(mocks.search).toHaveBeenCalled())
    expect(history.location.pathname).toBe('/search')
    expect(history.location.search).toBe('?q=Katy+Perry')
    expect(window.location.pathname).toBe('/navidrome/')
    expect(window.location.hash).toBe('#/search?q=Katy+Perry')
    expect(screen.queryByText('Not Found')).not.toBeInTheDocument()
  })

  it('canonicalizes a short initial q while preserving other URL params', async () => {
    mocks.search.mockResolvedValue(searchResults())
    const { history } = renderSearch('/search?q=a&view=compact')

    await waitFor(() => expect(history.location.search).toBe('?view=compact'))
    expect(mocks.search).not.toHaveBeenCalled()
    expect(
      screen.getByPlaceholderText('Search artists, albums, songs…'),
    ).toHaveValue('')
  })

  it('loads a direct search URL and restores it after navigation', async () => {
    mocks.search.mockResolvedValue(searchResults())
    const { history } = renderSearch('/search?q=Katy+Perry')

    await waitFor(() =>
      expect(mocks.search).toHaveBeenCalledWith(
        'Katy Perry',
        expect.objectContaining({ limit: 30 }),
      ),
    )
    expect(screen.getByPlaceholderText(/Search artists/)).toHaveValue(
      'Katy Perry',
    )

    history.push('/search/artist/example')
    expect(screen.getByText('Artist details')).toBeInTheDocument()
    history.goBack()

    await waitFor(() =>
      expect(screen.getByPlaceholderText(/Search artists/)).toHaveValue(
        'Katy Perry',
      ),
    )
    history.push('/search?q=Roar')

    await waitFor(() =>
      expect(screen.getByPlaceholderText(/Search artists/)).toHaveValue('Roar'),
    )
    await waitFor(() =>
      expect(mocks.search).toHaveBeenCalledWith(
        'Roar',
        expect.objectContaining({ limit: 30 }),
      ),
    )
    expect(history.location.pathname).toBe('/search')
    expect(window.location.hash).toBe('#/search?q=Roar')
  })

  it('records the selected song and the query that rendered it', async () => {
    localStorage.setItem('userId', 'user-1')
    const song = {
      id: 'song-1',
      source: 'catalog',
      localMediaFileId: 'local-1',
      title: 'Rendered Song',
      artistName: 'Rendered Artist',
      albumTitle: 'Rendered Album',
      imageUrl: 'https://example.com/cover.jpg',
      artworkUrls: ['https://example.com/cover-small.jpg'],
    }
    mocks.search.mockResolvedValue(searchResults({ songs: [song] }))
    const view = renderSearch()
    submitSearch('first query')
    await screen.findByRole('heading', { name: 'Rendered Song' })

    const input = screen.getByPlaceholderText('Search artists, albums, songs…')
    fireEvent.change(input, { target: { value: 'second query' } })
    fireEvent.click(screen.getByRole('button', { name: 'Play Rendered Song' }))

    expect(JSON.parse(localStorage.getItem(recentSongsKey('user-1')))).toEqual([
      {
        query: 'first query',
        song: {
          id: 'song-1',
          source: 'catalog',
          localMediaFileId: 'local-1',
          title: 'Rendered Song',
          artistName: 'Rendered Artist',
          albumTitle: 'Rendered Album',
          imageUrl: 'https://example.com/cover.jpg',
          artworkUrls: ['https://example.com/cover-small.jpg'],
        },
      },
    ])
    view.unmount()
  })

  it('does not create history for submitting a query without choosing a song', async () => {
    localStorage.setItem('userId', 'user-1')
    mocks.search.mockResolvedValue(searchResults())
    renderSearch()
    submitSearch('query only')
    await waitFor(() => expect(mocks.search).toHaveBeenCalled())
    expect(localStorage.getItem(recentSongsKey('user-1'))).toBeNull()
  })

  it('replays stored history directly without searching and restores artwork', async () => {
    localStorage.setItem('userId', 'user-1')
    localStorage.setItem(
      recentSongsKey('user-1'),
      JSON.stringify([
        {
          query: 'saved query',
          song: {
            id: 'saved-song',
            source: 'library',
            localMediaFileId: 'local-saved',
            title: 'Saved Song',
            artistName: 'Saved Artist',
            albumTitle: 'Saved Album',
            imageUrl: 'https://example.com/saved.jpg',
            artworkUrls: [],
          },
        },
      ]),
    )
    const { store } = renderSearch()
    expect(await screen.findByAltText('Saved Song')).toHaveAttribute(
      'src',
      'https://example.com/saved.jpg',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Play Saved Song' }))

    expect(mocks.search).not.toHaveBeenCalled()
    expect(store.getState().player.pendingSearchPlay).toEqual(
      expect.objectContaining({
        sourceId: 'saved-song',
        localMediaFileId: 'local-saved',
        title: 'Saved Song',
        artist: 'Saved Artist',
      }),
    )
  })

  it('deduplicates repeated plays, moves them to the top, and keeps eight songs', async () => {
    localStorage.setItem('userId', 'user-1')
    const songs = Array.from({ length: 9 }, (_, index) => ({
      id: `song-${index}`,
      source: 'catalog',
      title: `Song ${index}`,
      artistName: 'Artist',
    }))
    mocks.search.mockResolvedValue(searchResults({ songs }))
    renderSearch()
    submitSearch('many songs')
    await screen.findByRole('heading', { name: 'Song 0' })

    for (const song of songs) {
      fireEvent.click(
        screen.getByRole('button', { name: `Play ${song.title}` }),
      )
    }
    fireEvent.click(screen.getByRole('button', { name: 'Play Song 0' }))

    const stored = JSON.parse(localStorage.getItem(recentSongsKey('user-1')))
    expect(stored).toHaveLength(8)
    expect(stored[0].song.id).toBe('song-0')
    expect(stored.filter((entry) => entry.song.id === 'song-0')).toHaveLength(1)
    expect(stored.some((entry) => entry.song.id === 'song-1')).toBe(false)
  })

  it('switches history when the authenticated account changes in place', async () => {
    localStorage.setItem('userId', 'user-1')
    localStorage.setItem(
      recentSongsKey('user-1'),
      JSON.stringify([
        {
          query: 'one',
          song: { id: 'one', title: 'User One Song', source: 'catalog' },
        },
      ]),
    )
    localStorage.setItem(
      recentSongsKey('user-2'),
      JSON.stringify([
        {
          query: 'two',
          song: { id: 'two', title: 'User Two Song', source: 'catalog' },
        },
      ]),
    )
    const { rerenderSearch } = renderSearch()
    expect(await screen.findByText('User One Song')).toBeInTheDocument()
    localStorage.setItem('userId', 'user-2')
    rerenderSearch()

    await waitFor(() => {
      expect(screen.getByText('User Two Song')).toBeInTheDocument()
      expect(screen.queryByText('User One Song')).not.toBeInTheDocument()
    })
  })

  it('ignores legacy and malformed history and still plays with unavailable storage', async () => {
    localStorage.setItem('userId', 'user-1')
    localStorage.setItem(
      recentSongsKey('user-1'),
      JSON.stringify([
        'old query',
        { song: { id: { invalid: true }, title: 'Bad' } },
        { song: { id: 'valid', title: 'Valid Song', source: 'catalog' } },
      ]),
    )
    mocks.search.mockResolvedValue(
      searchResults({
        songs: [{ id: 'new', title: 'New Song', artistName: 'Artist' }],
      }),
    )
    const { store } = renderSearch()
    expect(await screen.findByText('Valid Song')).toBeInTheDocument()

    const setItem = vi
      .spyOn(window.localStorage, 'setItem')
      .mockImplementation(() => {
        throw new Error('quota')
      })
    submitSearch('New Song')
    fireEvent.click(
      await screen.findByRole('button', { name: 'Play New Song' }),
    )
    expect(store.getState().player.pendingSearchPlay).toEqual(
      expect.objectContaining({ sourceId: 'new' }),
    )
    expect(setItem).toHaveBeenCalled()
    setItem.mockRestore()
  })

  it('keeps the history menu separate from playback and removes only its row', async () => {
    localStorage.setItem('userId', 'user-1')
    localStorage.setItem(
      recentSongsKey('user-1'),
      JSON.stringify([
        {
          query: 'query',
          song: { id: 'saved', title: 'Saved Song', source: 'catalog' },
        },
        {
          query: 'query',
          song: { id: 'other', title: 'Other Song', source: 'catalog' },
        },
      ]),
    )
    const { store } = renderSearch()
    const menuButton = await screen.findByRole('button', {
      name: 'More options for Saved Song',
    })
    fireEvent.click(menuButton)
    expect(store.getState().player.pendingSearchPlay).toBeNull()
    expect(screen.getAllByRole('menuitem')).toHaveLength(1)
    fireEvent.click(
      screen.getByRole('menuitem', { name: 'Remove from history' }),
    )

    expect(screen.queryByText('Saved Song')).not.toBeInTheDocument()
    expect(screen.getByText('Other Song')).toBeInTheDocument()
    expect(
      JSON.parse(localStorage.getItem(recentSongsKey('user-1'))),
    ).toHaveLength(1)
    expect(store.getState().player.pendingSearchPlay).toBeNull()
  })

  it('clears stale results and aborts their late response while preserving other URL params', async () => {
    let resolveSearch
    let searchSignal
    mocks.search.mockImplementation((_query, options) => {
      searchSignal = options.signal
      return new Promise((resolve) => {
        resolveSearch = resolve
      })
    })
    const { history } = renderSearch('/search?q=old&view=compact')
    await waitFor(() => expect(mocks.search).toHaveBeenCalled())
    const input = screen.getByPlaceholderText('Search artists, albums, songs…')
    fireEvent.change(input, { target: { value: '' } })

    expect(input).toHaveValue('')
    expect(history.location.search).toBe('?view=compact')
    expect(screen.queryByText('Old Song')).not.toBeInTheDocument()
    expect(searchSignal.aborted).toBe(true)
    await act(async () => {
      resolveSearch(
        searchResults({ songs: [{ id: 'old', title: 'Old Song' }] }),
      )
      await Promise.resolve()
    })
    expect(screen.queryByText('Old Song')).not.toBeInTheDocument()
  })

  it('uses the whole song row for artwork/text playback and keeps downloads out of search', async () => {
    mocks.jobs = [
      { id: 'job', kind: 'song', sourceId: 'queued', status: 'running' },
    ]
    mocks.search.mockResolvedValue(
      searchResults({
        results: [
          { kind: 'artist', artist: { id: 'artist', name: 'An Artist' } },
          {
            kind: 'album',
            album: { id: 'album', title: 'An Album', artistName: 'An Artist' },
          },
          {
            kind: 'song',
            song: {
              id: 'queued',
              title: 'Queued Song',
              artistName: 'An Artist',
              imageUrl: 'https://example.com/queued.jpg',
            },
          },
        ],
      }),
    )
    const { history, store } = renderSearch()
    submitSearch('An Artist')
    const artwork = await screen.findByAltText('Queued Song')
    fireEvent.click(artwork)
    expect(store.getState().player.pendingSearchPlay.sourceId).toBe('queued')
    expect(screen.queryByText(/Download/)).not.toBeInTheDocument()
    expect(screen.getByText('Starting…')).toBeInTheDocument()

    fireEvent.click(
      screen.getByRole('button', { name: 'Open artist An Artist' }),
    )
    expect(history.location.pathname).toBe('/search/artist/artist')
    history.goBack()
    await screen.findByAltText('Queued Song')
    fireEvent.click(screen.getByRole('button', { name: 'Open album An Album' }))
    expect(history.location.pathname).toBe('/search/album/album')
  })

  it('keeps same-ID catalog and library rows independently playable and historic', async () => {
    localStorage.setItem('userId', 'user-1')
    mocks.jobs = [
      { id: 'job', kind: 'song', sourceId: 'same-id', status: 'queued' },
    ]
    mocks.search.mockResolvedValue(
      searchResults({
        results: [
          {
            kind: 'song',
            song: {
              id: 'same-id',
              source: 'catalog',
              title: 'Catalog Version',
              artistName: 'Artist',
            },
          },
          {
            kind: 'song',
            song: {
              id: 'same-id',
              source: 'library',
              localMediaFileId: 'local-same-id',
              title: 'Library Version',
              artistName: 'Artist',
            },
          },
        ],
      }),
    )
    renderSearch()
    submitSearch('same song')
    await screen.findByRole('heading', { name: 'Catalog Version' })

    const catalogRow = screen.getByRole('button', {
      name: 'Play Catalog Version',
    })
    const libraryRow = screen.getByRole('button', {
      name: 'Play Library Version',
    })
    fireEvent.click(catalogRow)
    expect(libraryRow).not.toBeDisabled()
    fireEvent.click(libraryRow)

    const stored = JSON.parse(localStorage.getItem(recentSongsKey('user-1')))
    expect(
      stored.map((entry) => `${entry.song.source}:${entry.song.id}`),
    ).toEqual(['library:same-id', 'catalog:same-id'])
  })

  it('activates a history row with keyboard input', async () => {
    localStorage.setItem('userId', 'user-1')
    localStorage.setItem(
      recentSongsKey('user-1'),
      JSON.stringify([
        {
          query: 'keyboard',
          song: { id: 'keyboard', title: 'Keyboard Song', source: 'catalog' },
        },
      ]),
    )
    const { store } = renderSearch()
    const button = await screen.findByRole('button', {
      name: 'Play Keyboard Song',
    })
    button.focus()
    const user = userEvent.setup()
    await user.keyboard('{Enter}')
    expect(store.getState().player.pendingSearchPlay).toEqual(
      expect.objectContaining({ sourceId: 'keyboard' }),
    )
  })

  it('keeps selected-song history in memory when no user ID is available', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        songs: [{ id: 'memory', title: 'Memory Song', artistName: 'Artist' }],
      }),
    )
    const { store } = renderSearch()
    submitSearch('Memory Song')
    fireEvent.click(
      await screen.findByRole('button', { name: 'Play Memory Song' }),
    )
    expect(
      localStorage.getItem('navidrome.externalMusic.recentSongs.v1'),
    ).toBeNull()

    fireEvent.change(
      screen.getByPlaceholderText('Search artists, albums, songs…'),
      {
        target: { value: '' },
      },
    )
    expect(await screen.findByText('Memory Song')).toBeInTheDocument()
    expect(store.getState().player.pendingSearchPlay).toEqual(
      expect.objectContaining({ sourceId: 'memory' }),
    )
  })
})
