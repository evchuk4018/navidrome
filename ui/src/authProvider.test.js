import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  clearQuickPickCache: vi.fn(),
  jwtDecode: vi.fn(),
}))

vi.mock('./quickpick/cache', () => ({
  clearQuickPickCache: mocks.clearQuickPickCache,
}))
vi.mock('jwt-decode', () => ({ jwtDecode: mocks.jwtDecode }))
vi.mock('./utils', () => ({
  baseUrl: (path) => `http://localhost${path}`,
}))

import authProvider from './authProvider'

describe('Quick Pick authentication cache boundaries', () => {
  beforeEach(() => {
    localStorage.clear()
    mocks.clearQuickPickCache.mockReset()
    mocks.jwtDecode.mockReset().mockReturnValue({})
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        status: 200,
        statusText: 'OK',
        json: () =>
          Promise.resolve({
            token: 'token',
            id: 'user-1',
            name: 'User',
            username: 'user',
            isAdmin: false,
            subsonicSalt: 'salt',
            subsonicToken: 'subsonic-token',
          }),
      }),
    )
  })

  afterEach(() => vi.unstubAllGlobals())

  it('clears cached content after a successful login', async () => {
    await authProvider.login({ username: 'user', password: 'password' })
    expect(mocks.clearQuickPickCache).toHaveBeenCalled()
  })

  it('clears cached content when logging out or handling an expired session', async () => {
    await authProvider.logout()
    expect(mocks.clearQuickPickCache).toHaveBeenCalledOnce()

    mocks.clearQuickPickCache.mockReset()
    await expect(
      authProvider.checkError({ status: 401 }),
    ).rejects.toBeUndefined()
    expect(mocks.clearQuickPickCache).toHaveBeenCalledOnce()
  })

  it('keeps cached content for non-authentication errors', async () => {
    await authProvider.checkError({ status: 500 })
    expect(mocks.clearQuickPickCache).not.toHaveBeenCalled()
  })
})
