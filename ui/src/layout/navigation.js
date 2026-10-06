import { useTranslate } from 'react-admin'
import SearchIcon from '@material-ui/icons/Search'
import FlashOnIcon from '@material-ui/icons/FlashOn'
import MusicNoteOutlinedIcon from '@material-ui/icons/MusicNoteOutlined'
import PlaylistPlayIcon from '@material-ui/icons/PlaylistPlay'
import OndemandVideoIcon from '@material-ui/icons/OndemandVideo'
import config from '../config'

export const COMPACT_NAVIGATION_QUERY = '(max-width:959.95px)'
export const COMPACT_NAVIGATION_MEDIA = `@media ${COMPACT_NAVIGATION_QUERY}`
export const BOTTOM_NAVIGATION_HEIGHT = 64
export const BOTTOM_NAVIGATION_SPACE = `calc(${BOTTOM_NAVIGATION_HEIGHT}px + env(safe-area-inset-bottom, 0px))`

export const useNavigationLinks = () => {
  const translate = useTranslate()
  return [
    { to: '/quick-pick', label: 'Quick Pick', icon: FlashOnIcon, exact: true },
    {
      to: '/search',
      label: translate('menu.search', { _: 'Search' }),
      icon: SearchIcon,
      exact: true,
    },
    {
      to: '/song',
      label: translate('resources.song.name', { smart_count: 2, _: 'Songs' }),
      icon: MusicNoteOutlinedIcon,
    },
    {
      to: '/playlist',
      label: translate('resources.playlist.name', {
        smart_count: 2,
        _: 'Playlists',
      }),
      icon: PlaylistPlayIcon,
    },
    ...(config.homeTubeBaseURL
      ? [
          {
            to: '/hometube',
            label: 'HomeTube',
            icon: OndemandVideoIcon,
          },
        ]
      : []),
  ]
}
