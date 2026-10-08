import { act, renderHook } from '@testing-library/react-hooks'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useRadioFeedback } from './useRadioFeedback'

const mocks = vi.hoisted(() => ({ send: vi.fn() }))
vi.mock('../quickpick/provider', () => ({
  sendRadioFeedback: mocks.send,
  radioErrorDetails: (error) => ({ message: error.message }),
}))

beforeEach(() => {
  vi.useFakeTimers()
  mocks.send.mockReset().mockImplementation(() => new Promise(() => {}))
})
afterEach(() => vi.useRealTimers())

it.each(['logout', 'unmount'])(
  'aborts and clears feedback on %s',
  async (operation) => {
    const hook = renderHook(
      ({ authenticated }) => useRadioFeedback(authenticated),
      {
        initialProps: { authenticated: true },
      },
    )
    act(() =>
      hook.result.current({ sessionId: 'session', itemId: 'item' }, 'started'),
    )
    await act(() => vi.advanceTimersByTimeAsync(0))
    const signal = mocks.send.mock.calls[0][2].signal
    if (operation === 'logout') hook.rerender({ authenticated: false })
    else hook.unmount()
    expect(signal.aborted).toBe(true)
    await act(() => vi.advanceTimersByTimeAsync(100000))
    expect(mocks.send).toHaveBeenCalledTimes(1)
    hook.unmount()
  },
)
