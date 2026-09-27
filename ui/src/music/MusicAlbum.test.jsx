import { render, screen } from '@testing-library/react'
import { createMemoryHistory } from 'history'
import { Route, Router } from 'react-router-dom'
import { vi } from 'vitest'
import MusicAlbum from './MusicAlbum'

const mocks = vi.hoisted(() => ({ getAlbum: vi.fn() }))

vi.mock('./provider', () => ({ getAlbum: mocks.getAlbum }))
vi.mock('./useDownloadJobs', () => ({
  useDownloadJobs: () => ({ jobs: [], refreshJobs: vi.fn() }),
}))
vi.mock('./DownloadStatus', () => ({
  DownloadButton: ({ id }) => <button>Download {id}</button>,
  DownloadStatus: () => null,
}))

const renderAlbum = () => {
  const history = createMemoryHistory({
    initialEntries: ['/search/album/group'],
  })
  render(
    <Router history={history}>
      <Route path="/search/album/:id" component={MusicAlbum} />
    </Router>,
  )
}

describe('<MusicAlbum /> library status', () => {
  beforeEach(() => mocks.getAlbum.mockReset())

  it('marks owned tracks and leaves new tracks downloadable', async () => {
    mocks.getAlbum.mockResolvedValue({
      album: { id: 'group', title: 'Album', artistName: 'Artist' },
      tracks: [
        { id: 'owned', title: 'Owned', localMediaFileId: 'local-owned' },
        { id: 'new', title: 'New' },
      ],
    })
    renderAlbum()

    expect(await screen.findByText('1. Owned')).toBeInTheDocument()
    expect(screen.getByText('Downloaded')).toBeInTheDocument()
    expect(screen.queryByText('Download owned')).not.toBeInTheDocument()
    expect(screen.getByText('Download new')).toBeInTheDocument()
  })

  it('withholds track downloads when ownership lookup failed', async () => {
    mocks.getAlbum.mockResolvedValue({
      album: { id: 'group', title: 'Album', artistName: 'Artist' },
      tracks: [{ id: 'unknown', title: 'Unknown' }],
      libraryStatusUnavailable: true,
    })
    renderAlbum()

    expect(await screen.findByText('1. Unknown')).toBeInTheDocument()
    expect(screen.getByText('Library status unavailable')).toBeInTheDocument()
    expect(screen.queryByText('Download unknown')).not.toBeInTheDocument()
  })
})
