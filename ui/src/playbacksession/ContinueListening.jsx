import React from 'react'
import { Box, Button, Typography } from '@material-ui/core'
import { useSelector } from 'react-redux'
import { playbackSession } from './controller'
import { clientUniqueId } from '../dataProvider/httpClient'
import { usePlaybackQueue } from '../audioplayer/PlaybackQueueContext'
import { usePlaybackOwnership } from './usePlaybackOwnership'

const ContinueListening = () => {
  const { remote, error, claiming } = usePlaybackOwnership()
  const queue = usePlaybackQueue()
  const current = useSelector(
    (state) =>
      state.player?.queue?.[
        state.player.playIndex ?? Math.max(0, state.player.savedPlayIndex || 0)
      ],
  )
  const available =
    remote?.selectedEntryId && remote.ownerClientId !== clientUniqueId
  if (!available && !error && !current?.unavailable) return null
  const seconds = Math.floor((remote?.positionMs || 0) / 1000)
  const position = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
  return (
    <Box
      component="section"
      aria-label="Continue listening"
      role="status"
      display="flex"
      alignItems="center"
      flexWrap="wrap"
      gridGap={12}
      p={2}
      style={{ borderBottom: '1px solid rgba(128,128,128,.25)' }}
    >
      <Box flex={1} minWidth={180}>
        <Typography variant="subtitle2">Continue listening</Typography>
        <Typography variant="body2">
          {current?.unavailable
            ? 'This item is unavailable. Retry or skip to continue.'
            : error ||
              `${remote?.currentEntry?.title || 'Your saved queue'}${remote?.currentEntry?.artist ? ' — ' + remote.currentEntry.artist : ''} · ${position}`}
        </Typography>
        {available && remote.deviceLabel && (
          <Typography variant="caption">{remote.deviceLabel}</Typography>
        )}
      </Box>
      <Button
        color="primary"
        disabled={claiming}
        onClick={() =>
          available || current?.unavailable
            ? playbackSession.resumeHere()
            : playbackSession.explicitPlay()
        }
      >
        {claiming
          ? 'Resuming…'
          : current?.unavailable || error
            ? 'Retry'
            : 'Resume here'}
      </Button>
      {current?.unavailable && (
        <Button onClick={() => queue?.next()}>Skip</Button>
      )}
    </Box>
  )
}

export default ContinueListening
