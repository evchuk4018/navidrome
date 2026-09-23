import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createHashHistory } from 'history'
import { Route, Router, Switch } from 'react-router-dom'
import { vi } from 'vitest'
import MusicSearch from './MusicSearch'

const mocks = vi.hoisted(() => ({
  search: vi.fn(),
}))

vi.mock('./provider', () => ({
  search: mocks.search,
}))

vi.mock('./useDownloadJobs', () => ({
  useDownloadJobs: () => ({ jobs: [], refreshJobs: vi.fn() }),
}))

vi.mock('./DownloadStatus', () => ({
  DownloadButton: () => null,
  DownloadStatus: () => null,
}))

const searchResults = (overrides = {}) => ({
  artists: [],
  albums: [],
  songs: [],
  genres: [],
  ...overrides,
})

const submitSearch = (value) => {
  const input = screen.getByPlaceholderText(
    'Search artists, albums, songs, or genres',
  )
  fireEvent.change(input, { target: { value } })
  fireEvent.submit(input.closest('form'))
}

const renderSearch = (route = '/search') => {
  window.history.replaceState(null, '', `/navidrome/#${route}`)
  const history = createHashHistory()
  const view = render(
    <Router history={history}>
      <Switch>
        <Route exact path="/search" component={MusicSearch} />
        <Route
          exact
          path="/search/artist/:id"
          render={() => <p>Artist details</p>}
        />
        <Route render={() => <p>Not Found</p>} />
      </Switch>
    </Router>,
  )
  return { history, ...view }
}

describe('<MusicSearch />', () => {
  beforeEach(() => {
    mocks.search.mockReset()
    localStorage.clear()
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
})
