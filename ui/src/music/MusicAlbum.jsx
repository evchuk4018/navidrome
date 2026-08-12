import { useEffect, useState } from 'react'
import { useHistory, useParams } from 'react-router-dom'
import {
  Box,
  Button,
  Card,
  CardContent,
  CircularProgress,
  Divider,
  List,
  ListItem,
  ListItemText,
  Typography,
} from '@material-ui/core'
import ArrowBackIcon from '@material-ui/icons/ArrowBack'
import { makeStyles } from '@material-ui/core/styles'
import * as musicProvider from './provider'
import { DownloadButton, DownloadStatus } from './DownloadStatus'
import { useDownloadJobs } from './useDownloadJobs'

const useStyles = makeStyles((theme) => ({
  root: {
    margin: '0 auto',
    maxWidth: 900,
    padding: theme.spacing(3),
  },
  header: {
    alignItems: 'center',
    display: 'flex',
    gap: theme.spacing(2),
    margin: theme.spacing(2, 0, 3),
  },
  image: {
    height: 180,
    objectFit: 'cover',
    width: 180,
  },
  placeholder: {
    alignItems: 'center',
    background: theme.palette.action.hover,
    display: 'flex',
    fontSize: 48,
    height: 180,
    justifyContent: 'center',
    width: 180,
  },
  track: {
    gap: theme.spacing(2),
  },
}))

const MusicAlbum = () => {
  const classes = useStyles()
  const history = useHistory()
  const { id } = useParams()
  const [album, setAlbum] = useState(null)
  const [error, setError] = useState('')
  const { jobs, refreshJobs } = useDownloadJobs()

  useEffect(() => {
    let mounted = true
    setAlbum(null)
    setError('')
    musicProvider
      .getAlbum(id)
      .then((value) => mounted && setAlbum(value))
      .catch(() => mounted && setError('Album information is unavailable right now.'))
    return () => {
      mounted = false
    }
  }, [id])

  return (
    <Box className={classes.root}>
      <Button startIcon={<ArrowBackIcon />} onClick={() => history.goBack()}>
        Back
      </Button>
      <DownloadStatus jobs={jobs} />
      {error && <Typography color="error">{error}</Typography>}
      {!album && !error && (
        <Box display="flex" justifyContent="center" p={5}>
          <CircularProgress />
        </Box>
      )}
      {album && (
        <>
          <Box className={classes.header}>
            {album.album.imageUrl ? (
              <img className={classes.image} src={album.album.imageUrl} alt="" />
            ) : (
              <div className={classes.placeholder}>
                {album.album.title?.slice(0, 1) || '♪'}
              </div>
            )}
            <Box flex={1}>
              <Typography variant="h4">{album.album.title}</Typography>
              <Typography color="textSecondary" paragraph>
                {album.album.artistName} {album.album.year ? `• ${album.album.year}` : ''}
              </Typography>
              <DownloadButton
                kind="album"
                id={album.album.id}
                onCreated={refreshJobs}
              />
            </Box>
          </Box>
          <Card>
            <CardContent>
              <Typography variant="h6" gutterBottom>
                Tracks
              </Typography>
              <List>
                {album.tracks?.map((track, index) => (
                  <Box key={track.id}>
                    <ListItem className={classes.track}>
                      <ListItemText
                        primary={`${track.trackNumber || index + 1}. ${track.title}`}
                        secondary={track.artistName}
                      />
                      <DownloadButton
                        kind="song"
                        id={track.id}
                        onCreated={refreshJobs}
                      />
                    </ListItem>
                    {index < album.tracks.length - 1 && <Divider />}
                  </Box>
                ))}
              </List>
            </CardContent>
          </Card>
        </>
      )}
    </Box>
  )
}

export default MusicAlbum
