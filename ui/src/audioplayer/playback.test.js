import { describe, it, expect, vi } from 'vitest'
import {
  playAudio,
  pauseAudio,
  togglePlayback,
  resumeContext,
  resumePlayback,
  cancelPlaybackRecovery,
} from './playback'

describe('playback helpers', () => {
  it('plays a paused element and resumes a suspended AudioContext', () => {
    const audio = { paused: true, play: vi.fn(() => Promise.resolve()) }
    const context = {
      state: 'suspended',
      resume: vi.fn(() => Promise.resolve()),
    }

    playAudio(audio, context)

    expect(context.resume).toHaveBeenCalledOnce()
    expect(audio.play).toHaveBeenCalledOnce()
  })

  it('does not play an already-playing element', () => {
    const audio = { paused: false, play: vi.fn() }

    playAudio(audio, null)

    expect(audio.play).not.toHaveBeenCalled()
  })

  it('does not cancel recovery when play() is interrupted', async () => {
    const audio = {
      paused: true,
      play: vi.fn(() => Promise.reject(new Error('NotAllowedError'))),
      pause: vi.fn(),
    }

    playAudio(audio, null)
    await Promise.resolve()

    expect(audio.play).toHaveBeenCalledOnce()
    expect(audio.pause).not.toHaveBeenCalled()
  })

  it('pauses a playing element', () => {
    const audio = { paused: false, pause: vi.fn() }

    pauseAudio(audio)

    expect(audio.pause).toHaveBeenCalledOnce()
  })

  it('does not pause an already-paused element', () => {
    const audio = { paused: true, pause: vi.fn() }

    pauseAudio(audio)

    expect(audio.pause).not.toHaveBeenCalled()
  })

  it('toggles between play and pause based on the element state', () => {
    const paused = { paused: true, play: vi.fn(() => Promise.resolve()) }
    const playing = { paused: false, pause: vi.fn() }

    togglePlayback(paused, null)
    togglePlayback(playing, null)

    expect(paused.play).toHaveBeenCalledOnce()
    expect(playing.pause).toHaveBeenCalledOnce()
  })

  it('uses native resume for a ready paused element', () => {
    const togglePlay = vi.fn()
    const play = vi.fn()
    const audio = {
      paused: true,
      readyState: 4,
      networkState: 1,
      togglePlay,
      play,
    }

    togglePlayback(audio, null)

    expect(togglePlay).not.toHaveBeenCalled()
    expect(play).toHaveBeenCalledOnce()
  })

  it('reloads an evicted source before using native playback', () => {
    const load = vi.fn()
    const play = vi.fn(() => Promise.resolve())
    const togglePlay = vi.fn()
    const audio = {
      paused: true,
      readyState: 0,
      networkState: 0,
      load,
      play,
      togglePlay,
    }

    togglePlayback(audio, null)

    expect(load).toHaveBeenCalledOnce()
    expect(play).toHaveBeenCalledOnce()
    expect(togglePlay).not.toHaveBeenCalled()
  })

  it('does nothing without an audio element', () => {
    expect(() => togglePlayback(null, null)).not.toThrow()
    expect(() => playAudio(null, null)).not.toThrow()
  })

  it('does not resume a running AudioContext', () => {
    const context = { state: 'running', resume: vi.fn() }

    resumeContext(context)

    expect(context.resume).not.toHaveBeenCalled()
  })

  it('swallows AudioContext resume rejection', async () => {
    const context = {
      state: 'suspended',
      resume: vi.fn(() => Promise.reject(new Error('NotAllowedError'))),
    }

    expect(() => resumeContext(context)).not.toThrow()
    await Promise.resolve()
  })

  it('swallows a synchronous AudioContext resume failure', () => {
    expect(() =>
      resumeContext({
        state: 'closed',
        resume: () => {
          throw new Error('InvalidStateError')
        },
      }),
    ).not.toThrow()
  })
})

const createAudio = ({
  evicted = false,
  playPromise = Promise.resolve(),
} = {}) => {
  const listeners = new Map()
  const audio = {
    src: '/song.mp3',
    paused: true,
    ended: false,
    currentTime: 45,
    duration: 120,
    networkState: evicted ? 0 : 1,
    readyState: evicted ? 0 : 4,
    emit: (event) => {
      listeners.get(event)?.forEach((listener) => listener())
    },
    addEventListener: (event, listener) => {
      if (!listeners.has(event)) listeners.set(event, new Set())
      listeners.get(event).add(listener)
    },
    removeEventListener: (event, listener) => {
      listeners.get(event)?.delete(listener)
    },
    load: vi.fn(() => {
      audio.currentTime = 0
      audio.networkState = 2
      audio.readyState = 0
      audio.emit('loadstart')
    }),
    play: vi.fn(() => {
      audio.paused = false
      audio.emit('play')
      return playPromise
    }),
    pause: vi.fn(() => {
      audio.paused = true
      audio.emit('pause')
    }),
    togglePlay: vi.fn(),
  }
  return audio
}

const loadMetadata = (audio) => {
  audio.readyState = 1
  audio.emit('loadedmetadata')
}

describe('explicit resume', () => {
  it('calls native play before resuming the context and coalesces repeated Play', () => {
    const audio = createAudio()
    const context = { state: 'suspended', resume: vi.fn() }

    resumePlayback(audio, context)
    resumePlayback(audio, context)

    expect(audio.play).toHaveBeenCalledOnce()
    expect(audio.togglePlay).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
    expect(audio.currentTime).toBe(45)
    expect(audio.play.mock.invocationCallOrder[0]).toBeLessThan(
      context.resume.mock.invocationCallOrder[0],
    )
  })

  it('coalesces Play even before a pending native play changes paused', () => {
    const audio = createAudio({ playPromise: new Promise(() => {}) })
    audio.play.mockImplementation(() => new Promise(() => {}))

    resumePlayback(audio)
    resumePlayback(audio)

    expect(audio.play).toHaveBeenCalledOnce()
    pauseAudio(audio)
    resumePlayback(audio)
    expect(audio.play).toHaveBeenCalledTimes(2)
    cancelPlaybackRecovery(audio)
  })

  it('resumes an interrupted context even if the element is already playing', () => {
    const audio = createAudio()
    audio.paused = false
    const context = { state: 'interrupted', resume: vi.fn() }

    resumePlayback(audio, context)

    expect(context.resume).toHaveBeenCalledOnce()
    expect(audio.play).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
  })

  it('reloads only once and restores an evicted source after metadata arrives', async () => {
    const audio = createAudio({ evicted: true })

    resumePlayback(audio)
    resumePlayback(audio)
    await Promise.resolve()
    expect(audio.currentTime).toBe(0)
    loadMetadata(audio)

    expect(audio.currentTime).toBe(45)
    expect(audio.load).toHaveBeenCalledOnce()
    expect(audio.play).toHaveBeenCalledOnce()
    expect(audio.load.mock.invocationCallOrder[0]).toBeLessThan(
      audio.play.mock.invocationCallOrder[0],
    )
  })

  it('uses the same recovery from the mini-player toggle', () => {
    const audio = createAudio({ evicted: true })
    togglePlayback(audio)
    loadMetadata(audio)

    expect(audio.currentTime).toBe(45)
    expect(audio.togglePlay).not.toHaveBeenCalled()
    expect(audio.play).toHaveBeenCalledOnce()
  })

  it('clamps a restored position to the newly loaded duration', () => {
    const audio = createAudio({ evicted: true })
    resumePlayback(audio)
    audio.duration = 30
    loadMetadata(audio)

    expect(audio.currentTime).toBe(30)
  })

  it('retries position restoration at canplay if metadata is not seekable yet', () => {
    const audio = createAudio({ evicted: true })
    resumePlayback(audio)
    let currentTime = 0
    let seekable = false
    Object.defineProperty(audio, 'currentTime', {
      get: () => currentTime,
      set: (value) => {
        if (!seekable) throw new Error('InvalidStateError')
        currentTime = value
      },
    })
    loadMetadata(audio)
    expect(audio.currentTime).toBe(0)
    seekable = true
    audio.emit('canplay')

    expect(audio.currentTime).toBe(45)
  })

  it.each([false, true])(
    'restarts a finished track (evicted: %s)',
    (evicted) => {
      const audio = createAudio({ evicted })
      audio.ended = true
      audio.currentTime = 120

      resumePlayback(audio)
      if (evicted) loadMetadata(audio)

      expect(audio.currentTime).toBe(0)
      expect(audio.play).toHaveBeenCalledOnce()
    },
  )

  it('does not restore or restart after an explicit Pause, and retains position for retry', () => {
    const audio = createAudio({ evicted: true })
    resumePlayback(audio)
    pauseAudio(audio)
    pauseAudio(audio)
    loadMetadata(audio)

    expect(audio.currentTime).toBe(0)
    expect(audio.play).toHaveBeenCalledOnce()
    expect(audio.pause).toHaveBeenCalledOnce()
    resumePlayback(audio)
    expect(audio.currentTime).toBe(45)
    expect(audio.play).toHaveBeenCalledTimes(2)
  })

  it('cancels restoration when the native player pauses during recovery', () => {
    const audio = createAudio({ evicted: true })
    resumePlayback(audio)
    audio.pause()
    loadMetadata(audio)

    expect(audio.currentTime).toBe(0)
    expect(audio.play).toHaveBeenCalledOnce()
  })

  it('does not apply the old position to a different source', () => {
    const audio = createAudio({ evicted: true })
    resumePlayback(audio)
    audio.src = '/next.mp3'
    loadMetadata(audio)

    expect(audio.currentTime).toBe(0)
    audio.paused = true
    resumePlayback(audio)
    expect(audio.currentTime).toBe(0)
  })

  it('cancels a same-URL queue change or explicit seek without restoring old progress', () => {
    const audio = createAudio({ evicted: true })
    resumePlayback(audio)
    cancelPlaybackRecovery(audio)
    audio.currentTime = 12
    loadMetadata(audio)

    expect(audio.currentTime).toBe(12)
    expect(audio.play).toHaveBeenCalledOnce()
  })

  it('allows a rejected resume to be retried at its original position', async () => {
    const audio = createAudio({ evicted: true })
    audio.play.mockRejectedValueOnce(
      new DOMException('Blocked', 'NotAllowedError'),
    )
    resumePlayback(audio)
    await Promise.resolve()
    expect(audio.pause).not.toHaveBeenCalled()
    loadMetadata(audio)
    resumePlayback(audio)

    expect(audio.currentTime).toBe(45)
    expect(audio.play).toHaveBeenCalledTimes(2)
  })

  it('retains position restoration if the dependency interrupts play with a reload', async () => {
    const audio = createAudio({ evicted: true })
    audio.play.mockRejectedValueOnce(new DOMException('Reloaded', 'AbortError'))
    resumePlayback(audio)
    await Promise.resolve()
    loadMetadata(audio)

    expect(audio.currentTime).toBe(45)
    expect(audio.pause).not.toHaveBeenCalled()
  })

  it('does not let a late failed Play cancel a newer attempt', async () => {
    let reject
    const audio = createAudio({
      evicted: true,
      playPromise: new Promise((_, fail) => {
        reject = fail
      }),
    })
    resumePlayback(audio)
    pauseAudio(audio)
    audio.play.mockImplementationOnce(() => {
      audio.paused = false
      return Promise.resolve()
    })
    resumePlayback(audio)
    reject(new DOMException('Blocked', 'NotAllowedError'))
    await Promise.resolve()
    loadMetadata(audio)

    expect(audio.currentTime).toBe(45)
    expect(audio.play).toHaveBeenCalledTimes(2)
  })

  it('leaves a synchronous playback failure retryable', () => {
    const audio = createAudio({ evicted: true })
    audio.play.mockImplementationOnce(() => {
      throw new Error('NotAllowedError')
    })
    expect(() => resumePlayback(audio)).not.toThrow()
    resumePlayback(audio)
    loadMetadata(audio)

    expect(audio.currentTime).toBe(45)
    expect(audio.play).toHaveBeenCalledTimes(2)
  })

  it('does nothing without an element', () => {
    expect(() => resumePlayback(null)).not.toThrow()
    expect(() => cancelPlaybackRecovery(null)).not.toThrow()
  })
})
