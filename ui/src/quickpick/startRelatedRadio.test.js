import { beforeEach, describe, expect, it, vi } from 'vitest'
import { startRelatedRadio } from './startRelatedRadio'

const mocks = vi.hoisted(() => ({ createPersonalRadio: vi.fn() }))

vi.mock('./provider', () => ({
  createPersonalRadio: mocks.createPersonalRadio,
  radioSongs: (response) => ({
    ids: response.items.map((item) => item.id),
    data: Object.fromEntries(
      response.items.map((item) => [
        item.id,
        {
          id: item.song.id,
          radioItemId: item.id,
          radioItemType: item.type,
        },
      ]),
    ),
  }),
}))

const response = (id) => ({
  session: { id },
  planningStatus: 'selecting',
  items: [
    { id: `${id}-seed`, type: 'seed', song: { id: 'seed' } },
    { id: `${id}-next`, type: 'library', song: { id: 'next' } },
  ],
})

describe('startRelatedRadio', () => {
  beforeEach(() => mocks.createPersonalRadio.mockReset())

  it('plays the seed first and attaches a related session for refills', async () => {
    mocks.createPersonalRadio.mockResolvedValue(response('session-1'))
    const dispatch = vi.fn()
    await startRelatedRadio(dispatch, vi.fn(), { id: 'seed', title: 'Seed' })

    expect(dispatch.mock.calls[0][0]).toEqual(
      expect.objectContaining({ type: 'PLAYER_PLAY_TRACKS', id: 'seed' }),
    )
    expect(mocks.createPersonalRadio).toHaveBeenCalledWith('seed', 'related')
    expect(dispatch.mock.calls[1][0]).toEqual(
      expect.objectContaining({
        type: 'PLAYER_SET_RADIO_SESSION',
        data: expect.objectContaining({ id: 'session-1' }),
      }),
    )
    expect(dispatch.mock.calls[2][0]).toEqual(
      expect.objectContaining({ type: 'PLAYER_SYNC_RADIO_TRACKS' }),
    )
  })

  it('ignores an older session response after a newer Play', async () => {
    let finishFirst
    mocks.createPersonalRadio
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishFirst = resolve
          }),
      )
      .mockResolvedValueOnce(response('new-session'))
    const dispatch = vi.fn()
    const first = startRelatedRadio(dispatch, vi.fn(), { id: 'first' })
    await vi.waitFor(() =>
      expect(mocks.createPersonalRadio).toHaveBeenCalledTimes(1),
    )
    const second = startRelatedRadio(dispatch, vi.fn(), { id: 'second' })
    finishFirst(response('old-session'))
    await Promise.all([first, second])

    const sessions = dispatch.mock.calls
      .map(([action]) => action)
      .filter((action) => action.type === 'PLAYER_SET_RADIO_SESSION')
    expect(sessions).toHaveLength(1)
    expect(sessions[0].data.id).toBe('new-session')
  })
})
