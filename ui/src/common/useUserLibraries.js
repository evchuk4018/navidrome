import { useCallback, useEffect } from 'react'
import { useDispatch } from 'react-redux'
import { useDataProvider } from 'react-admin'
import { setUserLibraries } from '../actions'
import { useRefreshOnEvents } from './useRefreshOnEvents'

// Library initialization belongs to the layout even when its selector is absent.
export const useUserLibraries = () => {
  const dispatch = useDispatch()
  const dataProvider = useDataProvider()
  const loadUserLibraries = useCallback(async () => {
    const userId = localStorage.getItem('userId')
    if (!userId) return
    try {
      const { data } = await dataProvider.getOne('user', { id: userId })
      dispatch(setUserLibraries(data.libraries || []))
    } catch (error) {
      // eslint-disable-next-line no-console
      console.warn(
        'Could not load user libraries (this may be expected for non-admin users):',
        error,
      )
    }
  }, [dataProvider, dispatch])

  useEffect(() => {
    loadUserLibraries()
  }, [loadUserLibraries])

  useRefreshOnEvents({
    events: ['library', 'user'],
    onRefresh: loadUserLibraries,
  })
}
