import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRadioFeedbackQueue } from './radioFeedback'

vi.mock('../dataProvider', () => ({ httpClient: vi.fn() }))
vi.mock('../consts', () => ({ REST_URL: '/api' }))

const feedback = {
  itemId: 'item',
  event: 'completed',
  listenedMs: 100,
  durationMs: 100,
}
const flush = () => vi.advanceTimersByTimeAsync(0)

describe('radio feedback delivery', () => {
  let send, warn, queue
  beforeEach(() => {
    vi.useFakeTimers()
    send = vi.fn().mockResolvedValue({})
    warn = vi.fn()
    queue = createRadioFeedbackQueue(send, warn)
  })
  afterEach(() => {
    queue.dispose()
    vi.useRealTimers()
  })

  it('reuses an immutable event and its ID when a response is lost', async () => {
    send.mockRejectedValueOnce(new TypeError('response lost'))
    const original = { ...feedback }
    queue.enqueue('session', original)
    original.listenedMs = 999
    await flush()
    const event = send.mock.calls[0][1]
    expect(event).toEqual({ ...feedback, eventId: expect.any(String) })
    expect(Object.isFrozen(event)).toBe(true)
    await vi.advanceTimersByTimeAsync(1000)
    expect(send).toHaveBeenCalledTimes(2)
    expect(send.mock.calls[1][1]).toBe(event)
    expect(warn).not.toHaveBeenCalled()
  })

  it('keeps completion before the next start while allowing other sessions to proceed', async () => {
    send.mockRejectedValueOnce({ status: 503 })
    queue.enqueue('old-session', feedback)
    queue.enqueue('old-session', {
      ...feedback,
      itemId: 'next',
      event: 'started',
    })
    await flush()
    queue.enqueue('new-session', {
      ...feedback,
      itemId: 'new',
      event: 'started',
    })
    await flush()
    expect(
      send.mock.calls.map(([session, event]) => [session, event.itemId]),
    ).toEqual([
      ['old-session', 'item'],
      ['new-session', 'new'],
    ])
    await vi.advanceTimersByTimeAsync(1000)
    expect(
      send.mock.calls.map(([session, event]) => [session, event.itemId]),
    ).toEqual([
      ['old-session', 'item'],
      ['new-session', 'new'],
      ['old-session', 'item'],
      ['old-session', 'next'],
    ])
    expect(send.mock.calls[2][1].eventId).not.toBe(
      send.mock.calls[3][1].eventId,
    )
  })

  it.each([500, 502, 503, 504])(
    'bounds retries for HTTP %s and warns once',
    async (status) => {
      send.mockRejectedValue({ status, message: 'unavailable' })
      queue.enqueue('session', feedback)
      await vi.advanceTimersByTimeAsync(7000)
      expect(send).toHaveBeenCalledTimes(4)
      expect(
        new Set(send.mock.calls.map(([, event]) => event.eventId)).size,
      ).toBe(1)
      expect(warn).toHaveBeenCalledTimes(1)
      expect(warn).toHaveBeenCalledWith(
        expect.objectContaining({ attempts: 4, status, itemId: 'item' }),
      )
    },
  )

  it.each([400, 401, 403, 404, 409])(
    'does not retry permanent HTTP %s failures',
    async (status) => {
      send.mockRejectedValue({ status })
      queue.enqueue('session', feedback)
      await vi.advanceTimersByTimeAsync(100000)
      expect(send).toHaveBeenCalledTimes(1)
      expect(warn).toHaveBeenCalledTimes(1)
    },
  )

  it('aborts timed out requests and bounds recovery even if the transport never settles', async () => {
    send.mockImplementation(() => new Promise(() => {}))
    queue.enqueue('session', feedback)
    await vi.advanceTimersByTimeAsync(47000)
    expect(send).toHaveBeenCalledTimes(4)
    expect(
      send.mock.calls.every(([, , options]) => options.signal.aborted),
    ).toBe(true)
    expect(warn).toHaveBeenCalledTimes(1)
  })

  it('clears pending retries and aborts in-flight requests when disposed', async () => {
    send
      .mockRejectedValueOnce({ status: 503 })
      .mockImplementation(() => new Promise(() => {}))
    queue.enqueue('retry', feedback)
    queue.enqueue('active', feedback)
    await flush()
    const active = send.mock.calls[1][2].signal
    queue.dispose()
    expect(active.aborted).toBe(true)
    queue.enqueue('later', feedback)
    await vi.advanceTimersByTimeAsync(100000)
    expect(send).toHaveBeenCalledTimes(2)
    expect(warn).not.toHaveBeenCalled()
  })
})
