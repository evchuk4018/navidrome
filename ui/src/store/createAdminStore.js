import {
  applyMiddleware,
  combineReducers,
  compose,
  legacy_createStore as createStore,
} from 'redux'
import { routerMiddleware, connectRouter } from 'connected-react-router'
import createSagaMiddleware from 'redux-saga'
import { all, fork } from 'redux-saga/effects'
import { adminReducer, adminSaga, USER_LOGOUT } from 'react-admin'
import throttle from 'lodash.throttle'
import { loadState, saveState } from './persistState'
import {
  playbackSession,
  playbackSessionMiddleware,
} from '../playbacksession/controller'
import { readPlaybackCache } from '../playbacksession/codec'

const createAdminStore = ({
  authProvider,
  dataProvider,
  history,
  customReducers = {},
}) => {
  const reducer = combineReducers({
    admin: adminReducer,
    router: connectRouter(history),
    ...customReducers,
  })
  const resettableAppReducer = (state, action) =>
    reducer(action.type !== USER_LOGOUT ? state : undefined, action)

  const saga = function* rootSaga() {
    yield all([adminSaga(dataProvider, authProvider)].map(fork))
  }
  const sagaMiddleware = createSagaMiddleware()

  const composeEnhancers =
    (process.env.NODE_ENV === 'development' &&
      typeof window !== 'undefined' &&
      window.__REDUX_DEVTOOLS_EXTENSION_COMPOSE__ &&
      window.__REDUX_DEVTOOLS_EXTENSION_COMPOSE__({
        trace: true,
        traceLimit: 25,
      })) ||
    compose

  const persistedState = loadState() || {}
  if (customReducers.player && persistedState.player) {
    const localPlayer = localStorage.getItem('userId')
      ? persistedState.player
      : { volume: persistedState.player.volume }
    persistedState.player = {
      ...customReducers.player(undefined, { type: '@@INIT' }),
      ...localPlayer,
    }
  }
  const cached = readPlaybackCache(localStorage.getItem('userId'))
  if (cached && customReducers.player) {
    const restored = customReducers.player(persistedState?.player, {
      type: 'PLAYER_RESTORE_SESSION',
      snapshot: cached,
      play: false,
    })
    if (persistedState) persistedState.player = restored
  } else if (persistedState?.player) {
    persistedState.player.autoPlay = false
    persistedState.player.selectionAutomatic = true
    persistedState.player.playIndex = persistedState.player.savedPlayIndex || 0
  }
  const store = createStore(
    resettableAppReducer,
    persistedState,
    composeEnhancers(
      applyMiddleware(
        playbackSessionMiddleware,
        sagaMiddleware,
        routerMiddleware(history),
      ),
    ),
  )

  store.subscribe(
    throttle(() => {
      const state = store.getState()
      saveState({
        theme: state.theme,
        library: state.library,
        player: (({ volume }) => ({
          volume,
        }))(state.player),
        albumView: state.albumView,
        settings: state.settings,
      })
    }),
    1000,
  )

  sagaMiddleware.run(saga)
  playbackSession.start(store)
  return store
}

export default createAdminStore
