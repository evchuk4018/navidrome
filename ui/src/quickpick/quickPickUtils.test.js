import { describe, expect, it } from 'vitest'
import {
  beginQuickPickRequest,
  getQuickPickItemKey,
  isCurrentQuickPickRequest,
  splitQuickPickShelves,
} from './quickPickUtils'

const song = (id, section = 'listen_again') => ({
  id,
  section,
  song: { id, title: `Song ${id}`, artist: 'Artist' },
})

describe('Quick Pick shelf composition', () => {
  it('keeps two six-item shelves and backfills sparse sections', () => {
    const result = splitQuickPickShelves([
      ...Array.from({ length: 8 }, (_, index) => song(`f-${index}`)),
      ...Array.from({ length: 4 }, (_, index) =>
        song(`r-${index}`, 'start_radio'),
      ),
    ])

    expect(result.listenAgain).toHaveLength(6)
    expect(result.startRadio).toHaveLength(6)
    expect(
      new Set(
        result.listenAgain.concat(result.startRadio).map(getQuickPickItemKey),
      ).size,
    ).toBe(12)
    expect(
      result.startRadio.every((item) => item.section === 'start_radio'),
    ).toBe(false)
  })

  it('uses legacy kinds when section is missing', () => {
    const result = splitQuickPickShelves([
      { kind: 'playlist', playlist: { id: 'playlist-1', name: 'Mix' } },
      {
        kind: 'recommendation',
        song: { id: 'song-1', title: 'New', artist: 'A' },
      },
    ])

    expect(result.listenAgain.map(getQuickPickItemKey)).toContain('playlist-1')
    expect(result.startRadio.map(getQuickPickItemKey)).toContain('song-1')
  })
})

describe('Quick Pick request ordering', () => {
  it('aborts stale A when B starts and suppresses duplicate B clicks', () => {
    const ref = { current: { token: 0, key: null, controller: null } }
    const requestA = beginQuickPickRequest(ref, 'a')
    const requestB = beginQuickPickRequest(ref, 'b')

    expect(requestA.controller.signal.aborted).toBe(true)
    expect(isCurrentQuickPickRequest(ref, requestA.token)).toBe(false)
    expect(isCurrentQuickPickRequest(ref, requestB.token)).toBe(true)
    expect(beginQuickPickRequest(ref, 'b')).toBeNull()
  })
})
