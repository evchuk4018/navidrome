import React from 'react'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { Route, Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import {
  HomeTubeChannel,
  HomeTubeChannels,
  HomeTubeFeed,
} from './HomeTubeViews'

const mocks = vi.hoisted(() => ({
  addChannel: vi.fn(),
  getChannel: vi.fn(),
  getFeed: vi.fn(),
  getJob: vi.fn(),
  isConfigured: vi.fn(),
  listChannels: vi.fn(),
  refreshChannel: vi.fn(),
  refreshFeed: vi.fn(),
  reportImpressions: vi.fn(),
  requestDownload: vi.fn(),
  updateSubscription: vi.fn(),
  playVideo: vi.fn(),
}))

vi.mock('./api', () => ({
  addHomeTubeChannel: mocks.addChannel,
  getHomeTubeChannel: mocks.getChannel,
  getHomeTubeFeed: mocks.getFeed,
  getHomeTubeJob: mocks.getJob,
  isHomeTubeConfigured: mocks.isConfigured,
  listHomeTubeChannels: mocks.listChannels,
  refreshHomeTubeChannel: mocks.refreshChannel,
  refreshHomeTubeFeed: mocks.refreshFeed,
  reportHomeTubeImpressions: mocks.reportImpressions,
  requestHomeTubeDownload: mocks.requestDownload,
  updateHomeTubeSubscription: mocks.updateSubscription,
}))

vi.mock('./HomeTubePlaybackContext', () => ({
  useHomeTubePlayback: () => ({ playVideo: mocks.playVideo }),
}))

const video = (id, overrides = {}) => ({
  id,
  channelId: 'channel-1',
  channelName: 'Example Channel',
  title: `Video ${id}`,
  durationSeconds: 90,
  uploadDate: '2026-01-02',
  viewCount: 12,
  thumbnailUrl: null,
  webUrl: `https://youtube.com/watch?v=${id}`,
  availability: 'available',
  liveStatus: null,
  mediaStatus: 'ready',
  mediaError: null,
  hasBackgroundAudio: false,
  downloadable: true,
  watchState: 'unwatched',
  playbackPositionSeconds: 0,
  playbackDurationSeconds: 90,
  watchPercentage: 0,
  ...overrides,
})

const channel = (overrides = {}) => ({
  id: 'channel-1',
  youtubeChannelId: 'youtube-1',
  sourceUrl: 'https://youtube.com/@example',
  name: 'Example Channel',
  handle: '@example',
  thumbnailUrl: null,
  importStatus: 'ready',
  importError: null,
  videoCount: 2,
  readyCount: 1,
  source: 'user_added',
  subscribed: true,
  trialStatus: 'none',
  ...overrides,
})

const renderWithRouter = (component, initialEntries = ['/hometube']) => {
  const history = createMemoryHistory({ initialEntries })
  const result = render(
    <ThemeProvider theme={createTheme()}>
      <Router history={history}>{component}</Router>
    </ThemeProvider>,
  )
  return { ...result, history }
}

const renderChannel = (initialEntries = ['/hometube/channels/channel-1']) => {
  const history = createMemoryHistory({ initialEntries })
  const result = render(
    <ThemeProvider theme={createTheme()}>
      <Router history={history}>
        <Route path="/hometube/channels/:id" component={HomeTubeChannel} />
      </Router>
    </ThemeProvider>,
  )
  return { ...result, history }
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.isConfigured.mockReturnValue(true)
  mocks.reportImpressions.mockResolvedValue({})
  mocks.refreshFeed.mockResolvedValue({ videos: [video('refresh')] })
  mocks.getFeed.mockResolvedValue({
    videos: [video('one'), video('two'), video('three')],
  })
  mocks.listChannels.mockResolvedValue({ channels: [channel()] })
  mocks.addChannel.mockResolvedValue({ channelId: 'channel-2', jobId: 'job-2' })
  mocks.getChannel.mockResolvedValue({
    channel: channel(),
    videos: [video('one')],
    total: 1,
    activeJob: null,
  })
  mocks.updateSubscription.mockImplementation((id, subscribed) =>
    Promise.resolve(channel({ id, subscribed })),
  )
  mocks.requestDownload.mockResolvedValue({
    job: {
      id: 'job-1',
      status: 'queued',
      progress: 0,
      stage: 'Queued',
      error: null,
    },
  })
  mocks.getJob.mockResolvedValue({
    id: 'job-1',
    status: 'ready',
    progress: 100,
    stage: 'Ready',
    error: null,
  })
})

afterEach(cleanup)

describe('HomeTubeFeed', () => {
  it('reports the visible feed, plays cards through the shared context, and applies refresh penalty IDs', async () => {
    renderWithRouter(<HomeTubeFeed />)
    await screen.findByText('Video one')
    await waitFor(() =>
      expect(mocks.reportImpressions).toHaveBeenCalledWith([
        'one',
        'two',
        'three',
      ]),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Play Video one' }))
    expect(mocks.playVideo).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'one' }),
    )
    fireEvent.click(
      screen.getByRole('button', { name: 'Refresh HomeTube feed' }),
    )
    await waitFor(() =>
      expect(mocks.refreshFeed).toHaveBeenCalledWith(['one', 'two'], 40),
    )
    expect(await screen.findByText('Video refresh')).toBeInTheDocument()
  })

  it('falls back to a GET when refresh POST is unavailable and ignores an old response after unmount', async () => {
    let resolveOld
    mocks.getFeed
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveOld = resolve
          }),
      )
      .mockResolvedValueOnce({ videos: [video('new')] })
    const first = renderWithRouter(<HomeTubeFeed />)
    await waitFor(() => expect(mocks.getFeed).toHaveBeenCalledTimes(1))
    first.unmount()
    renderWithRouter(<HomeTubeFeed />)
    await screen.findByText('Video new')
    resolveOld({ videos: [video('old')] })
    expect(screen.queryByText('Video old')).not.toBeInTheDocument()

    mocks.getFeed.mockResolvedValue({ videos: [video('new')] })
    mocks.refreshFeed.mockRejectedValueOnce(new Error('service unavailable'))
    fireEvent.click(
      screen.getByRole('button', { name: 'Refresh HomeTube feed' }),
    )
    await waitFor(() => expect(mocks.getFeed).toHaveBeenCalledTimes(3))
    expect(await screen.findByText('Video new')).toBeInTheDocument()
  })
})

describe('HomeTubeChannels', () => {
  it('adds channels and navigates to the returned detail route', async () => {
    const { history } = renderWithRouter(<HomeTubeChannels />, [
      '/hometube/channels',
    ])
    await screen.findByText('Example Channel')
    fireEvent.change(screen.getByLabelText('YouTube channel URL'), {
      target: { value: 'https://youtube.com/@new' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Open channel' }))
    await waitFor(() =>
      expect(mocks.addChannel).toHaveBeenCalledWith('https://youtube.com/@new'),
    )
    expect(history.location.pathname).toBe('/hometube/channels/channel-2')
  })
})

describe('HomeTubeChannel', () => {
  it('updates subscriptions and starts downloads with retryable actions', async () => {
    mocks.getChannel.mockResolvedValueOnce({
      channel: channel(),
      videos: [video('one', { mediaStatus: 'not_downloaded' })],
      total: 1,
      activeJob: null,
    })
    renderChannel()
    await screen.findByText('Video one')
    fireEvent.click(screen.getByRole('button', { name: 'Subscribed' }))
    await waitFor(() =>
      expect(mocks.updateSubscription).toHaveBeenCalledWith('channel-1', false),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Download' }))
    await waitFor(() =>
      expect(mocks.requestDownload).toHaveBeenCalledWith('one'),
    )
  })

  it('opens downloaded videos through the shared playback context', async () => {
    renderChannel()
    await screen.findByText('Video one')
    fireEvent.click(screen.getByRole('button', { name: 'Play' }))
    expect(mocks.playVideo).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'one' }),
    )
  })
})
