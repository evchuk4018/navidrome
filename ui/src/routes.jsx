import React from 'react'
import { Route } from 'react-router-dom'
import Personal from './personal/Personal'
import { MusicAlbum, MusicArtist, MusicSearch } from './music'
import QuickPick from './quickpick/QuickPick'
import config from './config'
import {
  HomeTubeChannel,
  HomeTubeChannels,
  HomeTubeFeed,
} from './hometube/HomeTubeViews'

const routes = [
  <Route exact path="/quick-pick" component={QuickPick} key={'quick-pick'} />,
  <Route exact path="/personal" render={() => <Personal />} key={'personal'} />,
  <Route exact path="/search" component={MusicSearch} key={'music-search'} />,
  <Route
    exact
    path="/search/artist/:id"
    component={MusicArtist}
    key={'music-artist'}
  />,
  <Route
    exact
    path="/search/album/:id"
    component={MusicAlbum}
    key={'music-album'}
  />,
  ...(config.homeTubeBaseURL
    ? [
        <Route
          exact
          path={['/hometube', '/hometube/feed']}
          component={HomeTubeFeed}
          key={'hometube-feed'}
        />,
        <Route
          exact
          path="/hometube/channels"
          component={HomeTubeChannels}
          key={'hometube-channels'}
        />,
        <Route
          exact
          path="/hometube/channels/:id"
          component={HomeTubeChannel}
          key={'hometube-channel'}
        />,
      ]
    : []),
]

export default routes
