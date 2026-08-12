import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import MusicSearch from './MusicSearch'

const mocks = vi.hoisted(() => ({
  search: vi.fn(),
  push: vi.fn(),
}))

vi.mock('react-router-dom', () => ({
  useHistory: () => ({ push: mocks.push }),
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

describe('<MusicSearch />', () => {
  beforeEach(() => {
    mocks.search.mockReset()
    mocks.push.mockReset()
    localStorage.clear()
  })

  it('renders a matching song before unrelated artist results', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        artists: [{ id: 'unrelated', name: 'The Boy That Got Away' }],
        songs: [
          {
            id: 'the-one-that-got-away',
            title: 'The One That Got Away',
            artistName: 'Katy Perry',
          },
        ],
      }),
    )

    render(<MusicSearch />)
    submitSearch('The one that got away')

    const songsHeading = await screen.findByRole('heading', { name: 'Songs' })
    const artistsHeading = await screen.findByRole('heading', {
      name: 'Artists',
    })

    expect(
      songsHeading.compareDocumentPosition(artistsHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(screen.getByText('Katy Perry')).toBeInTheDocument()
  })

  it('keeps artists before songs for an exact artist search', async () => {
    mocks.search.mockResolvedValue(
      searchResults({
        artists: [{ id: 'katy-perry', name: 'Katy Perry' }],
        songs: [{ id: 'roar', title: 'Roar', artistName: 'Katy Perry' }],
      }),
    )

    render(<MusicSearch />)
    submitSearch('Katy Perry')

    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'Songs' }),
      ).toBeInTheDocument(),
    )
    const artistsHeading = screen.getByRole('heading', { name: 'Artists' })
    const songsHeading = screen.getByRole('heading', { name: 'Songs' })

    expect(
      artistsHeading.compareDocumentPosition(songsHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })
})
