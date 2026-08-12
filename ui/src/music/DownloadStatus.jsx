import { useState } from 'react'
import {
  Box,
  Chip,
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  Paper,
  Typography,
} from '@material-ui/core'
import { useNotify } from 'react-admin'
import * as musicProvider from './provider'

const activeStatuses = new Set(['queued', 'running'])

export const DownloadButton = ({ kind, id, onCreated }) => {
  const notify = useNotify()
  const [loading, setLoading] = useState(false)

  const handleDownload = (event) => {
    event.stopPropagation()
    if (loading) return
    setLoading(true)
    musicProvider
      .createDownload(kind, id)
      .then((job) => {
        notify('Download queued')
        onCreated?.(job)
      })
      .catch(() => notify('Unable to queue download', 'warning'))
      .finally(() => setLoading(false))
  }

  return (
    <button
      type="button"
      onClick={handleDownload}
      disabled={loading}
      style={{
        border: 0,
        borderRadius: 4,
        cursor: loading ? 'wait' : 'pointer',
        padding: '6px 10px',
      }}
    >
      {loading ? 'Queueing…' : kind === 'album' ? 'Download album' : 'Download'}
    </button>
  )
}

export const DownloadStatus = ({ jobs }) => {
  if (!jobs || jobs.length === 0) return null

  return (
    <Paper elevation={1} style={{ marginBottom: 24, padding: 16 }}>
      <Typography variant="h6">Downloads</Typography>
      <List dense>
        {jobs.slice(0, 6).map((job) => {
          const progress = job.total > 0 ? (job.completed / job.total) * 100 : 0
          const label = job.title || job.album || `${job.kind} download`
          const statusLabel = job.status === 'succeeded' ? 'Complete' : job.status
          return (
            <ListItem key={job.id} disableGutters>
              <ListItemText
                primary={label}
                secondary={job.message || job.error}
              />
              <Box display="flex" alignItems="center" gridGap={8}>
                {activeStatuses.has(job.status) && job.total > 0 && (
                  <LinearProgress
                    variant="determinate"
                    value={Math.min(progress, 100)}
                    style={{ width: 80 }}
                  />
                )}
                <Chip
                  size="small"
                  color={job.status === 'failed' ? 'secondary' : 'default'}
                  label={statusLabel}
                />
              </Box>
            </ListItem>
          )
        })}
      </List>
    </Paper>
  )
}
