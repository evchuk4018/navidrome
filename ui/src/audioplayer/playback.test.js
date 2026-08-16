import { describe, it, expect, vi } from 'vitest'
import {
  playAudio,
  pauseAudio,
  togglePlayback,
  resumeContext,
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

  it('normalizes a rejected play() back to paused', async () => {
    const audio = {
      paused: true,
      play: vi.fn(() => Promise.reject(new Error('NotAllowedError'))),
      pause: vi.fn(),
    }

    playAudio(audio, null)
    await Promise.resolve()

    expect(audio.play).toHaveBeenCalledOnce()
    expect(audio.pause).toHaveBeenCalledOnce()
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
})
