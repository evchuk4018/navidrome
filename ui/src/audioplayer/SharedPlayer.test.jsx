import React, { createRef } from 'react'
import { act, cleanup, fireEvent, render, within } from '@testing-library/react'
import { Provider } from 'react-redux'
import { createStore } from 'redux'
import MusicPlayer from 'navidrome-music-player/es/index'
import HomeTubePlayerView from '../hometube/HomeTubePlayerView'

const shared = vi.hoisted(() => {
  window.matchMedia = vi.fn((media) => ({
    matches: false,
    media,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  }))
  return {
    desktop: vi.fn(),
    mobile: vi.fn(),
    progress: vi.fn(),
    queue: vi.fn(),
    mobileLayout: false,
  }
})
vi.mock(
  'navidrome-music-player/es/components/PlayerDesktop',
  async (original) => {
    const { default: View } = await original()
    return {
      default: (props) => {
        shared.desktop(props)
        return <View {...props} />
      },
    }
  },
)
vi.mock(
  'navidrome-music-player/es/components/PlayerMobile',
  async (original) => {
    const { default: View } = await original()
    return {
      default: (props) => {
        shared.mobile(props)
        return <View {...props} />
      },
    }
  },
)
vi.mock(
  'navidrome-music-player/es/components/PlayerProgress',
  async (original) => {
    const { default: View } = await original()
    return {
      default: (props) => {
        shared.progress(props)
        return <View {...props} />
      },
    }
  },
)
vi.mock(
  'navidrome-music-player/es/components/AudioListsPanel',
  async (original) => {
    const { default: View } = await original()
    return {
      default: (props) => {
        shared.queue(props)
        return <View {...props} />
      },
    }
  },
)
vi.mock('@material-ui/core', async (original) => ({
  ...(await original()),
  useMediaQuery: () => shared.mobileLayout,
}))
vi.mock('react-admin', async (original) => ({
  ...(await original()),
  useTranslate: () => (key) => key,
}))
vi.mock('./AudioTitle', () => ({
  default: ({ audioInfo }) => <span>{audioInfo.video?.title}</span>,
}))
vi.mock('./PlayerToolbar', () => ({ default: () => null }))
const song = {
  uuid: 'song',
  __PLAYER_KEY__: 'song',
  name: 'Song',
  musicSrc: '/song.mp3',
  duration: 120,
}
const video = {
  id: 'video',
  title: 'Video',
  channelName: 'Channel',
  durationSeconds: 120,
}
const store = createStore(() => ({ player: { mode: 'order' } }))
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.clearAllMocks()
  shared.mobileLayout = false
})

it.each([false, true])(
  'music and HomeTube render the exact dependency components (mobile: %s)',
  (mobile) => {
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    shared.mobileLayout = mobile
    const ref = createRef()
    const ended = vi.fn(),
      next = vi.fn()
    const music = render(
      <MusicPlayer
        ref={ref}
        audioLists={[song]}
        mode="full"
        autoPlay={false}
        autoPlayInitLoadPlayList={false}
        showMediaSession={false}
        locale={{ playListsText: 'Queue' }}
        navigation={{
          queue: [song],
          playId: 'song',
          select: vi.fn(),
          remove: vi.fn(),
          previous: vi.fn(),
          next,
        }}
        onAudioEnded={ended}
      />,
    )
    act(() =>
      ref.current.setState({ isMobile: mobile, toggle: true, loading: false }),
    )
    const view = mobile ? shared.mobile : shared.desktop
    expect(view).toHaveBeenCalled()
    const musicView = view.mock.calls.length
    const progressCount = shared.progress.mock.calls.length
    const queueCount = shared.queue.mock.calls.length
    const home = render(
      <Provider store={store}>
        <HomeTubePlayerView
          video={video}
          expanded
          artwork={<video data-testid="video-artwork" />}
          element={{
            currentTime: 0,
            volume: 1,
            addEventListener: () => {},
            removeEventListener: () => {},
          }}
          position={20}
          duration={120}
          playing={false}
          ready
          queue={[{ video }]}
          onToggle={vi.fn()}
          onNext={next}
          onClose={vi.fn()}
        />
      </Provider>,
    )
    expect(view.mock.calls.length).toBeGreaterThan(musicView)
    expect(shared.progress.mock.calls.length).toBeGreaterThan(progressCount)
    expect(home.getByTestId('video-artwork')).toBeInTheDocument()
    const controls = mobile
      ? '.react-jinke-music-player-mobile-toggle .prev-audio'
      : '.player-content .prev-audio'
    expect(
      music.baseElement.querySelector(controls)?.querySelector('svg')
        ?.outerHTML,
    ).toBe(
      home.container.querySelector(controls)?.querySelector('svg')?.outerHTML,
    )
    const trigger = within(home.container).getByRole('button', {
      name: 'Queue',
    })
    fireEvent.click(trigger)
    expect(shared.queue.mock.calls.length).toBeGreaterThan(queueCount)
    const dialog = within(home.container).getByRole('dialog', { name: 'Queue' })
    expect(dialog).toHaveFocus()
    fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(trigger).toHaveFocus()
    const id = ref.current.state.playId
    act(() => ref.current.onAudioEnd())
    expect(ended).toHaveBeenCalledOnce()
    expect(ref.current.state.playId).toBe(id)
    expect(next).not.toHaveBeenCalled()
  },
)
