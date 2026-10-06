import React, { useState } from 'react'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import {
  MediaSessionCoordinator,
  useMediaSessionCoordinator,
  useMediaSessionSource,
} from './MediaSessionCoordinator'
import { SleepTimerProvider, useSleepTimer } from './SleepTimerContext'
import SleepTimerButton from './SleepTimerButton'
import userEvent from '@testing-library/user-event'

const makeMedia = () => {
  const element = document.createElement('audio')
  let paused = false
  Object.defineProperty(element, 'paused', { get: () => paused })
  element.pause = vi.fn(() => {
    paused = true
    element.dispatchEvent(new Event('pause'))
  })
  element.play = vi.fn(() => {
    paused = false
    element.dispatchEvent(new Event('play'))
    return Promise.resolve()
  })
  return element
}

const Source = ({ source, element, active, onSleepExpire }) => {
  useMediaSessionSource({ source, element, active, onSleepExpire })
  return null
}

const Harness = ({ music, homeTube, onMusicExpire, onHomeTubeExpire }) => {
  const [source, setSource] = useState('music')
  const [minimized, setMinimized] = useState(false)
  const timer = useSleepTimer()
  const coordinator = useMediaSessionCoordinator()
  return (
    <>
      <Source
        source="music"
        element={music}
        active={source === 'music'}
        onSleepExpire={onMusicExpire}
      />
      {homeTube && (
        <Source
          source="hometube"
          element={homeTube}
          active={source === 'hometube'}
          onSleepExpire={onHomeTubeExpire}
        />
      )}
      <button onClick={() => setSource('hometube')}>Switch to HomeTube</button>
      <button onClick={() => setSource('music')}>Switch to music</button>
      <button onClick={() => setMinimized((current) => !current)}>
        Toggle minimized
      </button>
      <button
        onClick={() => {
          coordinator.allowPlayback()
          ;(source === 'music' ? music : homeTube).play()
        }}
      >
        Resume
      </button>
      <button onClick={() => timer.start(15)}>Arm directly</button>
      <output aria-label="Countdown">{timer.remainingSeconds}</output>
      {!minimized && <SleepTimerButton />}
    </>
  )
}

const setup = (props = {}) => {
  const music = makeMedia()
  const homeTube = props.musicOnly ? null : makeMedia()
  const onMusicExpire = vi.fn()
  const onHomeTubeExpire = vi.fn()
  const view = render(
    <MediaSessionCoordinator>
      <SleepTimerProvider>
        <Harness
          music={music}
          homeTube={homeTube}
          onMusicExpire={onMusicExpire}
          onHomeTubeExpire={onHomeTubeExpire}
        />
      </SleepTimerProvider>
    </MediaSessionCoordinator>,
  )
  return { ...view, music, homeTube, onMusicExpire, onHomeTubeExpire }
}

const openTimer = () =>
  fireEvent.click(screen.getByRole('button', { name: 'Sleep timer' }))
const choose = (minutes) => {
  openTimer()
  fireEvent.click(screen.getByRole('button', { name: `${minutes} min` }))
}

describe('shared sleep timer', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-06T12:00:00Z'))
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 44,
      height: 44,
      top: 600,
      bottom: 644,
      left: 100,
      right: 144,
      x: 100,
      y: 600,
      toJSON: () => {},
    })
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it.each([15, 30, 60])(
    'starts the %i-minute preset immediately and shows only time and Stop afterward',
    (minutes) => {
      const { music } = setup()
      openTimer()
      expect(
        within(screen.getByRole('dialog'))
          .getAllByRole('button')
          .map((button) => button.textContent),
      ).toEqual(['15 min', '30 min', '60 min'])
      fireEvent.click(screen.getByRole('button', { name: `${minutes} min` }))
      expect(
        screen.getByRole('button', { name: 'Sleep timer' }),
      ).toHaveAttribute('aria-expanded', 'false')
      expect(screen.getByLabelText('Countdown')).toHaveTextContent(
        String(minutes * 60),
      )
      expect(music.play).not.toHaveBeenCalled()
      const moon = screen.getByRole('button', { name: 'Sleep timer' })
      openTimer()
      expect(
        within(screen.getByRole('dialog')).getAllByRole('button'),
      ).toHaveLength(1)
      expect(screen.getByRole('button', { name: 'Stop' })).toBeEnabled()
      expect(screen.getByLabelText('Time remaining')).toHaveTextContent(
        `${minutes}:00`,
      )
      expect(moon).toHaveAttribute('data-active', 'true')
    },
  )

  it('Stop cancels without pausing or resuming and restores the preset menu', () => {
    const { music, onMusicExpire } = setup()
    choose(15)
    openTimer()
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    act(() => {
      vi.advanceTimersByTime(16 * 60 * 1000)
    })
    expect(screen.getByLabelText('Countdown')).toHaveTextContent('0')
    expect(music.pause).not.toHaveBeenCalled()
    expect(music.play).not.toHaveBeenCalled()
    expect(onMusicExpire).not.toHaveBeenCalled()
    openTimer()
    expect(screen.getByRole('button', { name: '15 min' })).toBeEnabled()
  })

  it('keeps its deadline across minimize and source switches and pauses the current source', () => {
    const { music, homeTube, onMusicExpire, onHomeTubeExpire } = setup()
    choose(15)
    fireEvent.click(screen.getByRole('button', { name: 'Toggle minimized' }))
    act(() => {
      vi.advanceTimersByTime(5 * 60 * 1000)
    })
    fireEvent.click(screen.getByRole('button', { name: 'Switch to HomeTube' }))
    fireEvent.click(screen.getByRole('button', { name: 'Toggle minimized' }))
    openTimer()
    expect(screen.getByLabelText('Time remaining')).toHaveTextContent('10:00')
    act(() => {
      vi.advanceTimersByTime(10 * 60 * 1000)
    })
    expect(homeTube.pause).toHaveBeenCalledOnce()
    expect(music.pause).not.toHaveBeenCalled()
    expect(onHomeTubeExpire).toHaveBeenCalledOnce()
    expect(onMusicExpire).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: '15 min' })).toBeEnabled()
  })

  it('works with music alone, blocks recovery and queue advance, and permits explicit resume', () => {
    const { music, onMusicExpire } = setup({ musicOnly: true })
    const next = vi.fn()
    music.addEventListener('ended', next)
    choose(15)
    act(() => {
      vi.advanceTimersByTime(15 * 60 * 1000)
    })
    expect(music.paused).toBe(true)
    expect(onMusicExpire).toHaveBeenCalledOnce()
    act(() => {
      music.play()
      music.dispatchEvent(new Event('ended'))
    })
    expect(music.paused).toBe(true)
    expect(next).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }))
    expect(music.paused).toBe(false)
    act(() => {
      music.dispatchEvent(new Event('ended'))
    })
    expect(next).toHaveBeenCalledOnce()
  })

  it('checks a throttled deadline before an ended event can advance the queue', () => {
    const { music, onMusicExpire } = setup()
    const next = vi.fn()
    music.addEventListener('ended', next)
    fireEvent.click(screen.getByRole('button', { name: 'Arm directly' }))
    vi.setSystemTime(new Date('2026-10-06T12:15:01Z'))
    act(() => {
      music.dispatchEvent(new Event('ended'))
    })
    expect(next).not.toHaveBeenCalled()
    expect(onMusicExpire).toHaveBeenCalledOnce()
    expect(music.paused).toBe(true)
  })

  it('rechecks the wall-clock deadline when a background tab becomes visible', () => {
    const { music } = setup()
    choose(30)
    vi.setSystemTime(new Date('2026-10-06T12:35:00Z'))
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(music.paused).toBe(true)
    expect(screen.getByLabelText('Countdown')).toHaveTextContent('0')
  })

  it('dismisses with Escape, restores focus, and leaves a running countdown armed', () => {
    setup()
    choose(60)
    const moon = screen.getByRole('button', { name: 'Sleep timer' })
    moon.focus()
    openTimer()
    fireEvent.keyDown(screen.getByRole('dialog'), {
      key: 'Escape',
      code: 'Escape',
    })
    act(() => {
      vi.advanceTimersByTime(300)
    })
    expect(moon).toHaveAttribute('aria-expanded', 'false')
    expect(moon).toHaveFocus()
    expect(screen.getByLabelText('Countdown')).toHaveTextContent('3600')
  })

  it('allows keyboard selection without sending Space or Enter to global player shortcuts', async () => {
    const { music } = setup()
    const keyboard = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    const globalShortcut = vi.fn()
    document.addEventListener('keydown', globalShortcut)
    try {
      screen.getByRole('button', { name: 'Sleep timer' }).focus()
      await keyboard.keyboard(' ')
      expect(
        screen.getByRole('dialog', { name: 'Sleep timer' }),
      ).toBeInTheDocument()
      await keyboard.tab()
      await keyboard.keyboard('{Enter}')
      expect(screen.getByLabelText('Countdown')).toHaveTextContent('900')
      expect(globalShortcut).not.toHaveBeenCalled()
      expect(music.play).not.toHaveBeenCalled()
    } finally {
      document.removeEventListener('keydown', globalShortcut)
    }
  })
})
