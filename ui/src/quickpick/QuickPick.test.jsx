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

  it('renders three familiar songs and nine discovery songs, reporting the grid once', async () => {
    const { rerender } = render(<QuickPick />)
    await screen.findByRole('heading', { name: 'Discover' })
    const heading = screen.getByRole('heading', { name: 'Quick Pick' })
    expect(heading.nextSibling.querySelectorAll('button')).toHaveLength(3)
    expect(
      screen
        .getByRole('heading', { name: 'Discover' })
        .nextSibling.querySelectorAll('button'),
    ).toHaveLength(9)
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledWith(
      'view-1',
      grid().items.map((item) => item.itemKey),
    )
    rerender(<QuickPick />)
    expect(mocks.recordQuickPickImpressions).toHaveBeenCalledTimes(1)
    expect(mocks.getQuickPick).toHaveBeenCalledTimes(1)
    expect(screen.getAllByRole('button')).toHaveLength(12)
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
      expect(screen.getAllByRole('button')).toHaveLength(12)
      expect(mocks.notify).not.toHaveBeenCalled()
    },
  )

  it('loads a new view on the next visit and reports clicks with that view', async () => {
    const first = render(<QuickPick />)
    await screen.findByRole('heading', { name: 'Discover' })
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

  it('does not report empty grids', async () => {
    mocks.getQuickPick.mockResolvedValue({ viewId: 'empty', items: [] })
    render(<QuickPick />)
    await screen.findByText(
      'Play a few songs and your favorites will appear here.',
    )
    expect(mocks.recordQuickPickImpressions).not.toHaveBeenCalled()
  })
})
