import { useEffect, useState } from 'react'
import { useHistory, useParams } from 'react-router-dom'
import {
  Box,
  Button,
  Card,
  CardActionArea,
  CardContent,
  CircularProgress,
  Grid,
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
    maxWidth: 1100,
    padding: theme.spacing(3),
  },
  card: {
    height: '100%',
  },
  image: {
    height: 180,
    objectFit: 'cover',
    width: '100%',
  },
  placeholder: {
    alignItems: 'center',
    background: theme.palette.action.hover,
    display: 'flex',
    fontSize: 48,
    height: 180,
    justifyContent: 'center',
  },
  actions: {
    display: 'flex',
    justifyContent: 'flex-end',
    padding: theme.spacing(0, 2, 2),
  },
}))

const MusicArtist = () => {
  const classes = useStyles()
  const history = useHistory()
  const { id } = useParams()
  const [artist, setArtist] = useState(null)
  const [error, setError] = useState('')
  const { jobs, refreshJobs } = useDownloadJobs()

  useEffect(() => {
    let mounted = true
    setArtist(null)
    setError('')
    musicProvider
      .getArtist(id)
      .then((value) => mounted && setArtist(value))
      .catch(() => mounted && setError('Artist information is unavailable right now.'))
    return () => {
      mounted = false
    }
  }, [id])

  return (
    <Box className={classes.root}>
      <Button startIcon={<ArrowBackIcon />} onClick={() => history.goBack()}>
        Back to search
      </Button>
      <DownloadStatus jobs={jobs} />
      {error && <Typography color="error">{error}</Typography>}
      {!artist && !error && (
        <Box display="flex" justifyContent="center" p={5}>
          <CircularProgress />
        </Box>
      )}
      {artist && (
        <>
          <Typography variant="h4" gutterBottom>
            {artist.artist.name}
          </Typography>
          <Typography color="textSecondary" paragraph>
            {artist.albums?.length || 0} albums
          </Typography>
          <Grid container spacing={2}>
            {artist.albums?.map((album) => (
              <Grid item xs={12} sm={6} md={4} key={album.id}>
                <Card className={classes.card}>
                  <CardActionArea
                    onClick={() => history.push(`/search/album/${album.id}`)}
                  >
                    {album.imageUrl ? (
                      <img className={classes.image} src={album.imageUrl} alt="" />
                    ) : (
                      <div className={classes.placeholder}>
                        {album.title?.slice(0, 1) || '♪'}
                      </div>
                    )}
                    <CardContent>
                      <Typography variant="h6">{album.title}</Typography>
                      <Typography color="textSecondary" variant="body2">
                        {album.year || 'Release year unknown'}
                        {album.trackCount ? ` • ${album.trackCount} tracks` : ''}
                      </Typography>
                    </CardContent>
                  </CardActionArea>
                  <Box className={classes.actions}>
                    <DownloadButton
                      kind="album"
                      id={album.id}
                      onCreated={refreshJobs}
                    />
                  </Box>
                </Card>
              </Grid>
            ))}
          </Grid>
        </>
      )}
    </Box>
  )
}

export default MusicArtist
