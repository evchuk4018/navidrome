import React from 'react'
import { Route } from 'react-router-dom'
import Personal from './personal/Personal'
import { MusicAlbum, MusicArtist, MusicSearch } from './music'
import QuickPick from './quickpick/QuickPick'

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
]

export default routes
