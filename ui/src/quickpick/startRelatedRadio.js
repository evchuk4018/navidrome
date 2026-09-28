import { playTracks, setRadioSession, syncRadioTracks } from '../actions'
import { createPersonalRadio, radioSongs } from './provider'

let latestStart = 0
let sessionCreation = Promise.resolve()

// Keep session creation ordered so a slower, older request cannot end the
// server session created for a newer selection.
export const startRelatedRadio = (dispatch, notify, song, options = {}) => {
  const { searchPlayRequestId, isCurrent = () => true } = options
  const start = ++latestStart
  const current = () => start === latestStart && isCurrent()
  if (!current()) return Promise.resolve()

  dispatch({
    ...playTracks({ [song.id]: song }, [song.id]),
    ...(searchPlayRequestId && { searchPlayRequestId }),
  })

  const create = sessionCreation
    .catch(() => {})
    .then(() => {
      if (!current()) return null
      return createPersonalRadio(song.id, 'related')
    })
  sessionCreation = create
  return create
    .then((response) => {
      if (!response || !current()) return
      const enriched = radioSongs(response)
      const seed = response.items.find((item) => item.type === 'seed')
      dispatch(
        setRadioSession({
          id: response.session.id,
          seedItemId: seed?.id,
          planningStatus: response.planningStatus,
        }),
      )
      const rest = enriched.ids.filter(
        (key) => enriched.data[key].radioItemType !== 'seed',
      )
      if (rest.length) dispatch(syncRadioTracks(enriched.data, rest))
    })
    .catch(() => {
      if (current())
        notify(
          'Radio could not be started; playing the selected song',
          'warning',
        )
    })
}
