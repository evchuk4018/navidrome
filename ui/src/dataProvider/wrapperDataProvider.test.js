import { describe, it, expect, vi, beforeEach } from 'vitest'
import wrapperDataProvider from './wrapperDataProvider'

const { mockProvider, mockHttpClient } = vi.hoisted(() => ({
  mockProvider: {
    update: vi.fn(),
    create: vi.fn(),
    getOne: vi.fn(),
    getList: vi.fn(),
  },
  mockHttpClient: vi.fn(),
}))

vi.mock('ra-data-json-server', () => ({ default: () => mockProvider }))
vi.mock('./httpClient', () => ({ default: mockHttpClient }))

describe('wrapperDataProvider', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mockProvider.update.mockResolvedValue({ data: { id: 'u1' } })
    mockProvider.create.mockResolvedValue({ data: { id: 'u1' } })
    mockProvider.getList.mockResolvedValue({ data: [] })
    mockHttpClient.mockResolvedValue({ json: [] })
  })

  describe('default playlist ordering', () => {
    const baseParams = {
      pagination: { page: 1, perPage: 25 },
      sort: { field: 'liked_songs_first', order: 'ASC' },
    }

    it('keeps the sentinel for the unfiltered playlist list', async () => {
      await wrapperDataProvider.getList('playlist', {
        ...baseParams,
        filter: {},
      })

      expect(mockProvider.getList).toHaveBeenCalledWith('playlist', {
        ...baseParams,
        filter: {},
      })
    })

    it('restores the sentinel after clearing an active filter without mutating params', async () => {
      const params = {
        ...baseParams,
        filter: {},
      }

      await wrapperDataProvider.getList('playlist', params)
      expect(mockProvider.getList).toHaveBeenLastCalledWith('playlist', params)

      params.filter.q = 'mix'
      await wrapperDataProvider.getList('playlist', params)
      expect(mockProvider.getList).toHaveBeenLastCalledWith('playlist', {
        ...params,
        sort: { field: 'name', order: 'ASC' },
      })
      expect(params.sort).toEqual(baseParams.sort)

      params.filter.q = ''
      await wrapperDataProvider.getList('playlist', params)
      expect(mockProvider.getList).toHaveBeenLastCalledWith('playlist', params)
      expect(params.sort).toEqual(baseParams.sort)
    })

    it.each([
      { q: 'mix' },
      { owner_id: 'user-1' },
      { starred: true },
      { starred: false },
      { library_id: ['library-1'] },
    ])(
      'uses ordinary name ordering for an active filter %#',
      async (filter) => {
        await wrapperDataProvider.getList('playlist', {
          ...baseParams,
          filter,
        })

        expect(mockProvider.getList).toHaveBeenCalledWith('playlist', {
          ...baseParams,
          sort: { field: 'name', order: 'ASC' },
          filter,
        })
      },
    )

    it.each([{ q: '' }, { q: undefined }, { q: null }, { q: [] }])(
      'keeps the sentinel for an empty filter value %#',
      async (filter) => {
        await wrapperDataProvider.getList('playlist', {
          ...baseParams,
          filter,
        })

        expect(mockProvider.getList).toHaveBeenCalledWith('playlist', {
          ...baseParams,
          filter,
        })
      },
    )

    it('does not rewrite an explicit playlist sort', async () => {
      await wrapperDataProvider.getList('playlist', {
        ...baseParams,
        sort: { field: 'updated_at', order: 'DESC' },
        filter: { q: 'mix' },
      })

      expect(mockProvider.getList).toHaveBeenCalledWith('playlist', {
        ...baseParams,
        sort: { field: 'updated_at', order: 'DESC' },
        filter: { q: 'mix' },
      })
    })
  })

  describe('update user', () => {
    it('sets library associations when an admin edits a non-admin user', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: false, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalledWith(
        'user',
        expect.objectContaining({ id: 'u1' }),
      )
      expect(mockHttpClient).toHaveBeenCalledWith('/api/user/u1/library', {
        method: 'PUT',
        body: JSON.stringify({ libraryIds: [1] }),
      })
    })

    it('does not call the admin-only library endpoint when a non-admin edits their own profile', async () => {
      localStorage.setItem('role', 'regular')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: {
          name: 'Sam',
          isAdmin: false,
          libraryIds: [1],
          currentPassword: 'old',
          password: 'new',
        },
      })

      expect(mockProvider.update).toHaveBeenCalled()
      expect(mockHttpClient).not.toHaveBeenCalled()
    })

    it('does not set library associations when the edited user is an admin', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: true, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalled()
      expect(mockHttpClient).not.toHaveBeenCalled()
    })

    it('strips libraryIds from the user update payload', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: false, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalledWith(
        'user',
        expect.objectContaining({
          data: { name: 'Sam', isAdmin: false },
        }),
      )
    })
  })
})
