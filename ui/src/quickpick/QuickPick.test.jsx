import React from 'react'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import QuickPick from './QuickPick'

const mocks = vi.hoisted(() => ({
  getQuickPick: vi.fn(),
  recordQuickPickImpressions: vi.fn(),
  recordQuickPickClick: vi.fn(),
  startRelatedRadio: vi.fn(),
  notify: vi.fn(),
  dispatch: vi.fn(),
}))

vi.mock('react-admin', () => ({ useNotify: () => mocks.notify }))
vi.mock('react-redux', () => ({ useDispatch: () => mocks.dispatch }))
vi.mock('./QuickPickPlaylists', () => ({
  default: () => <div data-testid="playlists" />,
}))
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

describe('QuickPick', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.getQuickPick.mockResolvedValue(grid())
    mocks.recordQuickPickImpressions.mockResolvedValue({})
    mocks.recordQuickPickClick.mockResolvedValue({})
    mocks.startRelatedRadio.mockResolvedValue()
  })
  afterEach(cleanup)

  it('renders only nine discovery songs without headings or menus, reporting those songs once', async () => {
    const { rerender } = render(<QuickPick />)
    await screen.findByRole('button', { name: 'Play Song 3 radio' })
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    expect(screen.queryByText('Song 0')).not.toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Discover' })).toBeInTheDocument()
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledWith(
      'view-1',
      grid()
        .items.slice(3)
        .map((item) => item.itemKey),
    )
    rerender(<QuickPick />)
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledTimes(1)
    expect(mocks.getQuickPick).toHaveBeenCalledTimes(1)
    expect(screen.getAllByRole('button')).toHaveLength(9)
    expect(screen.getByTestId('playlists')).toBeInTheDocument()
  })

  it('plays immediately but waits for impressions before reporting a click', async () => {
    let finishImpressions
    mocks.recordQuickPickImpressions.mockReturnValue(
      new Promise((resolve) => {
        finishImpressions = resolve
      }),
    )
    render(<QuickPick />)
    fireEvent.click(
      await screen.findByRole('button', { name: 'Play Song 6 radio' }),
    )
    expect(mocks.startRelatedRadio).toHaveBeenCalledWith(
      mocks.dispatch,
      mocks.notify,
      grid().items[6].song,
    )
    expect(mocks.recordQuickPickClick).not.toHaveBeenCalled()
    finishImpressions({})
    await waitFor(() =>
      expect(mocks.recordQuickPickClick).toHaveBeenCalledWith(
        'view-1',
        'track:song-6',
      ),
    )
    expect(mocks.getQuickPick).toHaveBeenCalledTimes(1)
  })

  it.each(['impression', 'click'])(
    'keeps playback working after a %s tracking failure',
    async (failure) => {
      if (failure === 'impression')
        mocks.recordQuickPickImpressions.mockRejectedValue(new Error('offline'))
      else mocks.recordQuickPickClick.mockRejectedValue(new Error('offline'))
      render(<QuickPick />)
      fireEvent.click(
        await screen.findByRole('button', { name: 'Play Song 6 radio' }),
      )
      await waitFor(() =>
        expect(mocks.startRelatedRadio).toHaveBeenCalledTimes(1),
      )
      expect(screen.getAllByRole('button')).toHaveLength(9)
      expect(mocks.notify).not.toHaveBeenCalled()
    },
  )

  it('loads a new view on the next visit and reports clicks with that view', async () => {
    const first = render(<QuickPick />)
    await screen.findByRole('button', { name: 'Play Song 3 radio' })
    first.unmount()
    mocks.getQuickPick.mockResolvedValue(grid('view-2'))
    render(<QuickPick />)
    fireEvent.click(
      await screen.findByRole('button', { name: 'Play Song 6 radio' }),
    )
    await waitFor(() =>
      expect(mocks.recordQuickPickClick).toHaveBeenCalledWith(
        'view-2',
        'track:song-6',
      ),
    )
    expect(mocks.getQuickPick).toHaveBeenCalledTimes(2)
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledTimes(2)
  })

  it.each([{ items: [] }, { items: grid().items.slice(0, 3) }])(
    'does not report empty discovery grids',
    async ({ items }) => {
      mocks.getQuickPick.mockResolvedValue({ viewId: 'empty', items })
      render(<QuickPick />)
      await screen.findByText('No songs to discover yet.')
      expect(mocks.recordQuickPickImpressions).not.toHaveBeenCalled()
      expect(screen.getByTestId('playlists')).toBeInTheDocument()
    },
  )

  it('keeps playlists mounted while discoveries load', () => {
    mocks.getQuickPick.mockReturnValue(new Promise(() => {}))
    render(<QuickPick />)
    expect(
      screen.getByRole('progressbar', { name: 'Loading discoveries' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('playlists')).toBeInTheDocument()
  })

  it('recovers from a discovery failure without blocking playlists', async () => {
    mocks.getQuickPick.mockRejectedValueOnce(new Error('offline'))
    render(<QuickPick />)
    await screen.findByText('Unable to load discoveries.')
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(screen.getByTestId('playlists')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry discoveries' }))
    await screen.findByRole('button', { name: 'Play Song 3 radio' })
    expect(mocks.getQuickPick).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledTimes(1)
  })

  it('does not render or record a discovery item without a playable song', async () => {
    const response = grid()
    response.items[3] = { ...response.items[3], song: null }
    mocks.getQuickPick.mockResolvedValue(response)
    render(<QuickPick />)
    await screen.findByRole('button', { name: 'Play Song 4 radio' })
    expect(screen.getAllByRole('button')).toHaveLength(8)
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledWith(
      'view-1',
      response.items.slice(4).map((item) => item.itemKey),
    )
  })
})
