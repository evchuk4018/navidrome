import { describe, expect, it } from 'vitest'
import {
  nextPlayableRadioIndex,
  radioReadyAhead,
  shouldRetryRadioStream,
} from './radioPlayback'

describe('radio playback buffering', () => {
  const queue = [
    { radioSessionId: 's', radioItemId: 'current', musicSrc: 'a' },
    { radioSessionId: 's', radioItemId: 'pending', radioPending: true },
    { radioSessionId: 's', radioItemId: 'ready', musicSrc: 'b' },
    { radioSessionId: 'other', radioItemId: 'other', musicSrc: 'c' },
  ]

  it('counts ready tracks only and finds the first successor after a temporary end', () => {
    expect(radioReadyAhead(queue, 0, 's')).toBe(1)
    expect(nextPlayableRadioIndex(queue, 1, 's')).toBe(2)
    expect(nextPlayableRadioIndex(queue, 2, 's')).toBe(-1)
  })

  it('allows one stream retry before reporting the track unplayable', () => {
    const attempts = new Set()
    expect(shouldRetryRadioStream(attempts, 's:item')).toBe(true)
    expect(shouldRetryRadioStream(attempts, 's:item')).toBe(false)
  })
})
