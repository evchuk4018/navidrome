import React, { cloneElement } from 'react'
import {
  sanitizeListRestProps,
  TopToolbar,
  CreateButton,
  Button,
  useListContext,
  useTranslate,
} from 'react-admin'
import { useMediaQuery } from '@material-ui/core'
import RestoreIcon from '@material-ui/icons/Restore'
import { ToggleFieldsMenu } from '../common'

const PlaylistListActions = ({ className, ...rest }) => {
  const isNotSmall = useMediaQuery((theme) => theme.breakpoints.up('sm'))
  const translate = useTranslate()
  const { setSort, setPage } = useListContext()

  const restoreDefaultOrder = () => {
    setSort('liked_songs_first', 'ASC')
    setPage(1)
  }

  return (
    <TopToolbar className={className} {...sanitizeListRestProps(rest)}>
      {cloneElement(rest.filters, { context: 'button' })}
      <Button
        onClick={restoreDefaultOrder}
        label={translate('resources.playlist.actions.defaultOrder', {
          _: 'Default order',
        })}
        aria-label={translate('resources.playlist.actions.defaultOrder', {
          _: 'Default order',
        })}
      >
        <RestoreIcon />
      </Button>
      <CreateButton basePath="/playlist">
        {translate('ra.action.create')}
      </CreateButton>
      {isNotSmall && <ToggleFieldsMenu resource="playlist" modern />}
    </TopToolbar>
  )
}

export default PlaylistListActions
