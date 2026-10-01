import React from 'react'
import { act, render, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import { libraryReducer } from '../reducers/libraryReducer'
import { activityReducer } from '../reducers/activityReducer'
import { EVENT_REFRESH_RESOURCE } from '../actions'
import { useUserLibraries } from './useUserLibraries'

const mocks = vi.hoisted(() => ({ getOne: vi.fn() }))
const dataProvider = { getOne: mocks.getOne }
vi.mock('react-admin', () => ({ useDataProvider: () => dataProvider }))

const LibraryBootstrap = () => {
  useUserLibraries()
  return null
}
const libraries = [{ id: 'lib-1' }, { id: 'lib-2' }]
const renderBootstrap = () => {
  const store = createStore(
    combineReducers({ library: libraryReducer, activity: activityReducer }),
    {
      library: { userLibraries: libraries, selectedLibraries: ['lib-2'] },
    },
  )
  return {
    ...render(
      <Provider store={store}>
        <LibraryBootstrap />
      </Provider>,
    ),
    store,
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  vi.spyOn(Date, 'now').mockReturnValue(1000)
  localStorage.clear()
  localStorage.setItem('userId', 'user-1')
  mocks.getOne.mockResolvedValue({ data: { libraries } })
})

describe('headless library initialization', () => {
  it('loads libraries without rendering a control and preserves valid saved selections', async () => {
    const { container, store } = renderBootstrap()
    await waitFor(() =>
      expect(mocks.getOne).toHaveBeenCalledWith('user', { id: 'user-1' }),
    )
    expect(container).toBeEmptyDOMElement()
    expect(store.getState().library).toEqual({
      userLibraries: libraries,
      selectedLibraries: ['lib-2'],
    })
  })

  it.each([{ library: ['lib-1'] }, { user: ['user-1'] }, { '*': '*' }])(
    'reloads and reconciles selections on relevant events: %j',
    async (resources) => {
      const { store } = renderBootstrap()
      await waitFor(() => expect(mocks.getOne).toHaveBeenCalledOnce())
      mocks.getOne.mockResolvedValue({ data: { libraries: [libraries[0]] } })
      vi.mocked(Date.now).mockReturnValue(2000)
      act(
        () =>
          void store.dispatch({
            type: EVENT_REFRESH_RESOURCE,
            data: resources,
          }),
      )
      await waitFor(() =>
        expect(store.getState().library.userLibraries).toEqual([libraries[0]]),
      )
      expect(store.getState().library.selectedLibraries).toEqual([])
      expect(mocks.getOne).toHaveBeenCalledTimes(2)
    },
  )

  it('keeps existing library state on a failed load', async () => {
    mocks.getOne.mockRejectedValue(new Error('Unavailable'))
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { store } = renderBootstrap()
    await waitFor(() => expect(warning).toHaveBeenCalledOnce())
    expect(store.getState().library).toEqual({
      userLibraries: libraries,
      selectedLibraries: ['lib-2'],
    })
  })

  it('does not request user libraries without an authenticated user', () => {
    localStorage.clear()
    renderBootstrap()
    expect(mocks.getOne).not.toHaveBeenCalled()
  })
})
