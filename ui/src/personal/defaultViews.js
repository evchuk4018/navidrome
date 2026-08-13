import albumLists, { defaultAlbumList } from '../album/albumLists'

export const resourceDefaultViews = [
  'quick-pick',
  'artist',
  'song',
  'playlist',
  'radio',
]

export const isResourceDefaultView = (defaultView) =>
  resourceDefaultViews.includes(defaultView)

export const getDefaultViewChoices = (translate) => [
  ...Object.keys(albumLists).map((type) => ({
    id: type,
    name: translate(`resources.album.lists.${type}`),
  })),
  ...resourceDefaultViews.map((resource) => ({
    id: resource,
    name:
      resource === 'quick-pick'
        ? 'Quick Pick'
        : translate(`resources.${resource}.name`, { smart_count: 2 }),
  })),
]

export const getStoredDefaultView = () => {
  if (!localStorage.getItem('quickPickDefaultV1')) {
    localStorage.setItem('defaultView', 'quick-pick')
    localStorage.setItem('quickPickDefaultV1', '1')
  }
  return localStorage.getItem('defaultView') || defaultAlbumList
}
