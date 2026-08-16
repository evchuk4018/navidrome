import React from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import MiniPlayer from './MiniPlayer'

describe('<MiniPlayer />', () => {
  const track = {
    name: 'The One That Got Away',
    singer: 'Katy Perry',
    cover: '/cover.jpg',
    duration: 240,
  }

  afterEach(cleanup)

  it('renders the current track and its progress', () => {
    render(
      <MiniPlayer
        track={track}
        audioInstance={{ paused: false }}
        currentTime={60}
        duration={240}
        isPlaying
        onExpand={vi.fn()}
      />,
    )

    expect(screen.getByText(track.name)).toBeInTheDocument()
    expect(screen.getByText(track.singer)).toBeInTheDocument()
    expect(
      screen.getByTestId('mini-player').querySelector('img'),
    ).toHaveAttribute('src', track.cover)
    expect(screen.getByTestId('mini-player-progress')).toHaveStyle({
      width: '25%',
    })
    expect(
      screen.getByRole('button', { name: 'Click to pause' }),
    ).toBeInTheDocument()
  })

  it('shows the radio planning status while finding a next song', () => {
    render(
      <MiniPlayer
        track={track}
        audioInstance={{ paused: false }}
        radioPlanningStatus="selecting"
        onExpand={vi.fn()}
      />,
    )

    expect(screen.getByText('Pondering next song…')).toBeInTheDocument()
  })

  it('toggles playback without expanding the player', () => {
    const play = vi.fn(() => Promise.resolve())
    const onExpand = vi.fn()
    render(
      <MiniPlayer
        track={track}
        audioInstance={{ paused: true, play }}
        onExpand={onExpand}
      />,
    )

    fireEvent.click(screen.getByTestId('mini-player-toggle'))

    expect(play).toHaveBeenCalledOnce()
    expect(onExpand).not.toHaveBeenCalled()
  })

  it('pauses when already playing', () => {
    const pause = vi.fn()
    const onExpand = vi.fn()
    render(
      <MiniPlayer
        track={track}
        audioInstance={{ paused: false, pause }}
        onExpand={onExpand}
      />,
    )

    fireEvent.click(screen.getByTestId('mini-player-toggle'))

    expect(pause).toHaveBeenCalledOnce()
    expect(onExpand).not.toHaveBeenCalled()
  })

  it('opens the full player from the details button, arrow, or swipe up', () => {
    const onExpand = vi.fn()
    render(
      <MiniPlayer
        track={track}
        audioInstance={{ paused: true }}
        onExpand={onExpand}
      />,
    )
    const miniPlayer = screen.getByTestId('mini-player')

    fireEvent.click(screen.getByTestId('mini-player-details'))
    fireEvent.click(screen.getByTestId('mini-player-expand'))
    fireEvent.touchStart(miniPlayer, {
      touches: [{ clientY: 300 }],
    })
    fireEvent.touchEnd(miniPlayer, {
      changedTouches: [{ clientY: 240 }],
    })

    expect(onExpand).toHaveBeenCalledTimes(3)
  })

  it('falls back to a music icon when artwork fails to load', () => {
    render(
      <MiniPlayer
        track={track}
        audioInstance={{ paused: true }}
        onExpand={vi.fn()}
      />,
    )
    const image = screen.getByTestId('mini-player').querySelector('img')

    fireEvent.error(image)

    expect(screen.getByTestId('mini-player').querySelector('img')).toBeNull()
  })

  it('renders nothing without a track', () => {
    const { container } = render(
      <MiniPlayer audioInstance={{ paused: true }} onExpand={vi.fn()} />,
    )

    expect(container).toBeEmptyDOMElement()
  })
})
