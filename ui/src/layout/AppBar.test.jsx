import React from 'react'
import { act, render, screen, waitFor } from '@testing-library/react'
import { describe, it, beforeEach, vi } from 'vitest'
import { Provider } from 'react-redux'
import { createStore, combineReducers } from 'redux'
import { activityReducer } from '../reducers'
import AppBar from './AppBar'
import config from '../config'
import { installMatchMedia } from './testMediaQuery'

let store

const streamMocks = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  startEventStream: vi.fn(),
}))

vi.mock('react-admin', () => ({
  AppBar: ({ userMenu }) => <div data-testid="appbar">{userMenu}</div>,
  useTranslate: () => (x) => x,
  usePermissions: () => ({ permissions: 'admin' }),
  getResources: () => [],
}))

vi.mock('./NowPlayingPanel', () => ({
  default: () => <div data-testid="now-playing-panel" />,
}))
vi.mock('./ActivityPanel', () => ({
  default: () => <div data-testid="activity-panel" />,
}))
vi.mock('./PersonalMenu', () => ({
  default: () => <div />,
}))
vi.mock('./UserMenu', () => ({
  default: ({ children }) => <div>{children}</div>,
}))
vi.mock('../dialogs/Dialogs', () => ({
  Dialogs: () => <div data-testid="dialogs" />,
}))
vi.mock('../dialogs', () => ({
  AboutDialog: () => <div />,
}))
vi.mock('../authProvider', () => ({
  default: { checkAuth: streamMocks.checkAuth },
}))
vi.mock('../eventStream', () => ({
  startEventStream: streamMocks.startEventStream,
}))

describe('<AppBar />', () => {
  beforeEach(() => {
    config.devActivityPanel = true
    config.enableNowPlaying = true
    streamMocks.checkAuth.mockClear()
    streamMocks.startEventStream.mockClear()
    streamMocks.checkAuth.mockResolvedValue(undefined)
    streamMocks.startEventStream.mockResolvedValue(undefined)
    installMatchMedia(1024)
    store = createStore(combineReducers({ activity: activityReducer }), {
      activity: { nowPlayingCount: 0 },
    })
  })

  afterEach(() => vi.unstubAllGlobals())

  it('initializes the event stream in compact mode without a visible header', async () => {
    installMatchMedia(390)
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    await waitFor(() => expect(streamMocks.checkAuth).toHaveBeenCalledOnce())
    expect(streamMocks.startEventStream).toHaveBeenCalledOnce()
    expect(streamMocks.startEventStream).toHaveBeenCalledWith(store.dispatch)
  })

  it('keeps event stream initialization on desktop', async () => {
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    await waitFor(() => expect(streamMocks.checkAuth).toHaveBeenCalledOnce())
    expect(streamMocks.startEventStream).toHaveBeenCalledWith(store.dispatch)
  })

  it('does not restart the event stream when crossing the compact breakpoint', async () => {
    const resize = installMatchMedia(390)
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    await waitFor(() =>
      expect(streamMocks.startEventStream).toHaveBeenCalledOnce(),
    )
    act(() => resize(960))
    act(() => resize(800))
    expect(streamMocks.checkAuth).toHaveBeenCalledOnce()
    expect(streamMocks.startEventStream).toHaveBeenCalledOnce()
  })

  it('does not initialize the event stream when the activity panel is disabled', async () => {
    config.devActivityPanel = false
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    await waitFor(() => expect(streamMocks.checkAuth).not.toHaveBeenCalled())
    expect(streamMocks.startEventStream).not.toHaveBeenCalled()
  })

  it('does not start the event stream when authentication is rejected', async () => {
    streamMocks.checkAuth.mockRejectedValue(new Error('unauthorized'))
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    await waitFor(() => expect(streamMocks.checkAuth).toHaveBeenCalledOnce())
    await act(async () => {})
    expect(streamMocks.startEventStream).not.toHaveBeenCalled()
  })

  it('removes the compact top bar and its controls while keeping shared dialogs mounted across resizes', () => {
    const resize = installMatchMedia(390)
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    expect(screen.queryByTestId('appbar')).not.toBeInTheDocument()
    expect(screen.queryByTestId('now-playing-panel')).not.toBeInTheDocument()
    expect(screen.queryByTestId('activity-panel')).not.toBeInTheDocument()
    const dialogs = screen.getByTestId('dialogs')
    expect(screen.getAllByTestId('dialogs')).toHaveLength(1)
    act(() => resize(960))
    expect(screen.getByTestId('appbar')).toBeInTheDocument()
    expect(screen.getByTestId('dialogs')).toBe(dialogs)
    expect(screen.getAllByTestId('dialogs')).toHaveLength(1)
    act(() => resize(800))
    expect(screen.queryByTestId('appbar')).not.toBeInTheDocument()
    expect(screen.getByTestId('dialogs')).toBe(dialogs)
  })

  it('renders NowPlayingPanel when enabled', () => {
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    expect(screen.getByTestId('now-playing-panel')).toBeInTheDocument()
  })

  it('hides NowPlayingPanel when disabled', () => {
    config.enableNowPlaying = false
    render(
      <Provider store={store}>
        <AppBar />
      </Provider>,
    )
    expect(screen.queryByTestId('now-playing-panel')).toBeNull()
  })
})
