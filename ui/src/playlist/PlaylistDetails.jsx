import { useState } from 'react'
import { Card, CardContent, Typography, useMediaQuery } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { useTranslate } from 'react-admin'
import Lightbox from 'react-image-lightbox'
import 'react-image-lightbox/style.css'
import {
  CollapsibleComment,
  DurationField,
  ImageUploadOverlay,
  LoveButton,
  SizeField,
  isWritable,
  OverflowTooltip,
} from '../common'
import subsonic from '../subsonic'
import { Artwork } from '../common/Artwork'
import { sidebarColors } from '../common/pinkPageStyles'

const useStyles = makeStyles(
  (theme) => ({
    root: {
      width: '100%',
      minWidth: 0,
      marginBottom: theme.spacing(2),
      overflow: 'hidden',
      backgroundColor: '#171017',
      backgroundImage: 'none',
      border: `1px solid ${sidebarColors.divider}`,
      borderRadius: 20,
      color: sidebarColors.text,
      boxShadow: '0 12px 32px rgba(0, 0, 0, .24)',
    },
    cardContents: {
      display: 'flex',
      alignItems: 'stretch',
      minWidth: 0,
      gap: theme.spacing(1),
      [theme.breakpoints.down('xs')]: {
        gap: 0,
      },
    },
    details: {
      display: 'flex',
      flexDirection: 'column',
      flex: '1 1 auto',
      minWidth: 0,
    },
    content: {
      flex: '2 0 auto',
      minWidth: 0,
      padding: theme.spacing(2.5),
      '&:last-child': { paddingBottom: theme.spacing(2.5) },
    },
    coverParent: {
      width: 'clamp(96px, 24vw, 220px)',
      height: 'clamp(96px, 24vw, 220px)',
      minWidth: 'clamp(96px, 24vw, 220px)',
      alignSelf: 'center',
      margin: theme.spacing(2),
      [theme.breakpoints.down('xs')]: {
        width: 'clamp(76px, 27vw, 128px)',
        height: 'clamp(76px, 27vw, 128px)',
        minWidth: 'clamp(76px, 27vw, 128px)',
        margin: theme.spacing(1.5),
      },
      backgroundColor: sidebarColors.divider,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      position: 'relative',
    },
    cover: {
      objectFit: 'contain',
      cursor: 'pointer',
      display: 'block',
      width: '100%',
      height: '100%',
      backgroundColor: 'transparent',
      transition: 'opacity 0.3s ease-in-out',
      borderRadius: 12,
    },
    title: {
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      wordBreak: 'break-word',
      minWidth: 0,
      color: `${sidebarColors.text} !important`,
      fontWeight: 700,
      lineHeight: 1.15,
    },
    titleRow: {
      display: 'flex',
      alignItems: 'center',
    },
    loveButton: {
      marginLeft: theme.spacing(0.5),
      flexShrink: 0,
    },
    stats: {
      marginTop: theme.spacing(1.5),
      marginBottom: theme.spacing(0.5),
      color: `${sidebarColors.secondary} !important`,
      lineHeight: 1.5,
    },
  }),
  {
    name: 'NDPlaylistDetails',
  },
)

const PlaylistDetails = (props) => {
  const { record = {} } = props
  const translate = useTranslate()
  const classes = useStyles()
  const isDesktop = useMediaQuery((theme) => theme.breakpoints.up('lg'))
  const [isLightboxOpen, setLightboxOpen] = useState(false)

  const fullImageUrl = subsonic.getCoverArtUrl(record)

  return (
    <Card className={classes.root}>
      <div className={classes.cardContents}>
        <div className={classes.coverParent}>
          <Artwork
            record={record}
            square
            fit="contain"
            className={classes.cover}
            title={record.name}
            onClick={() => setLightboxOpen(true)}
          />
          {isWritable(record.ownerId) && (
            <ImageUploadOverlay
              entityType="playlist"
              entityId={record.id}
              hasUploadedImage={!!record.uploadedImage}
            />
          )}
        </div>
        <div className={classes.details}>
          <CardContent className={classes.content}>
            <div className={classes.titleRow}>
              <OverflowTooltip title={record.name || ''}>
                <Typography
                  variant={isDesktop ? 'h5' : 'h6'}
                  className={classes.title}
                >
                  {record.name || translate('ra.page.loading')}
                </Typography>
              </OverflowTooltip>
              <LoveButton
                className={classes.loveButton}
                record={record}
                resource={'playlist'}
                size={isDesktop ? 'default' : 'small'}
                aria-label={translate('resources.playlist.fields.starred')}
                color="primary"
              />
            </div>
            <Typography component="p" className={classes.stats}>
              {record.songCount ? (
                <span>
                  {record.songCount}{' '}
                  {translate('resources.song.name', {
                    smart_count: record.songCount,
                  })}
                  {' · '}
                  <DurationField record={record} source={'duration'} />
                  {' · '}
                  <SizeField record={record} source={'size'} />
                </span>
              ) : (
                <span>&nbsp;</span>
              )}
            </Typography>
            <CollapsibleComment record={record} />
          </CardContent>
        </div>
      </div>
      {isLightboxOpen && (
        <Lightbox
          imagePadding={50}
          animationDuration={200}
          imageTitle={record.name}
          mainSrc={fullImageUrl}
          onCloseRequest={() => setLightboxOpen(false)}
        />
      )}
    </Card>
  )
}

export default PlaylistDetails
