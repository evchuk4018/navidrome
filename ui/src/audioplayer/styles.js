import { makeStyles } from '@material-ui/core/styles'
import {
  BOTTOM_NAVIGATION_SPACE,
  COMPACT_NAVIGATION_MEDIA,
} from '../layout/navigation'
import { sidebarColors } from '../layout/sidebarStyles'

const useStyle = makeStyles(
  () => ({
    audioTitle: {
      textDecoration: 'none',
      color: `${sidebarColors.activeText} !important`,
    },
    songTitle: {
      fontWeight: 'bold',
      '&:hover + $qualityInfo': {
        opacity: 1,
      },
    },
    songInfo: {
      display: 'block',
      marginTop: '2px',
    },
    radioPlanning: {
      display: 'block',
      marginTop: '2px',
      color: `${sidebarColors.secondary} !important`,
      fontSize: 'smaller',
      fontStyle: 'italic',
    },
    songAlbum: {
      fontStyle: 'italic',
      fontSize: 'smaller',
    },
    qualityInfo: {
      marginTop: '-4px',
      opacity: 0,
      transition: 'all 500ms ease-out',
    },
    player: {
      display: (props) => (props.visible ? 'block' : 'none'),
      [COMPACT_NAVIGATION_MEDIA]: {
        '& .music-player-panel': {
          bottom: BOTTOM_NAVIGATION_SPACE,
          zIndex: 1001,
        },
        '& .react-jinke-music-player-mobile': { zIndex: 1200 },
      },
      // The dependency renders the queue and expanded mobile player as
      // sibling fixed layers. Keep the queue above the full player so it is
      // usable on phones, where both layers cover the viewport.
      '&& .audio-lists-panel': {
        // Sit above the expanded player (1200) while leaving Material UI's
        // modal layer (1300) available for dialogs opened from the queue.
        zIndex: 1250,
        color: `${sidebarColors.text} !important`,
        backgroundColor: `${sidebarColors.background} !important`,
        border: `1px solid ${sidebarColors.divider}`,
        boxShadow: '0 12px 32px rgba(0, 0, 0, .35)',
        '&.audio-lists-panel-mobile': {
          borderRadius: 0,
        },
        '& .audio-lists-panel-content .audio-item:nth-child(odd)': {
          backgroundColor: '#21111a !important',
        },
        '& .audio-lists-panel-header': {
          color: `${sidebarColors.text} !important`,
          backgroundColor: '#21111a !important',
          borderBottom: `1px solid ${sidebarColors.divider}`,
          textShadow: 'none',
          '& svg': {
            color: `${sidebarColors.navigation} !important`,
          },
          '& .audio-lists-panel-header-actions > *:hover svg': {
            color: `${sidebarColors.activeText} !important`,
          },
        },
        '& .audio-lists-panel-content .audio-item': {
          color: `${sidebarColors.text} !important`,
          backgroundColor: '#171017 !important',
          borderBottom: `1px solid ${sidebarColors.divider}`,
          '& svg': {
            color: `${sidebarColors.navigation} !important`,
          },
          '& .player-singer': {
            color: `${sidebarColors.secondary} !important`,
          },
          '&:hover, &:active': {
            backgroundColor: `${sidebarColors.selection} !important`,
            '& svg': {
              color: `${sidebarColors.activeText} !important`,
            },
          },
          '&.playing': {
            color: `${sidebarColors.activeText} !important`,
            backgroundColor: `${sidebarColors.selection} !important`,
            '& svg': { color: `${sidebarColors.accent} !important` },
            '& .player-singer': {
              color: `${sidebarColors.activeText} !important`,
            },
          },
        },
        '&.audio-lists-panel-mobile .audio-lists-panel-content': {
          paddingBottom: 'env(safe-area-inset-bottom, 0px)',
        },
      },
      // The source-owned mini player and the dependency's expanded player
      // share the same pink surface and controls on compact screens.
      '& .react-jinke-music-player-mobile': {
        zIndex: 1200,
        boxSizing: 'border-box',
        overflowY: 'auto',
        paddingTop: 'max(20px, env(safe-area-inset-top, 0px))',
        paddingBottom: 'max(20px, env(safe-area-inset-bottom, 0px))',
        '& > .group': { flexShrink: 0 },
        color: `${sidebarColors.text} !important`,
        backgroundColor: `${sidebarColors.background} !important`,
        '& .react-jinke-music-player-mobile-header-right': {
          color: `${sidebarColors.navigation} !important`,
        },
        '& .react-jinke-music-player-mobile-singer-name': {
          color: `${sidebarColors.secondary} !important`,
          '&::before, &::after': {
            backgroundColor: `${sidebarColors.divider} !important`,
          },
        },
        '& .react-jinke-music-player-mobile-progress .current-time, & .react-jinke-music-player-mobile-progress .duration':
          {
            color: `${sidebarColors.secondary} !important`,
          },
        '& .react-jinke-music-player-mobile-progress .rc-slider-rail': {
          backgroundColor: `${sidebarColors.divider} !important`,
        },
        '& .react-jinke-music-player-mobile-progress .rc-slider-handle, & .react-jinke-music-player-mobile-progress .rc-slider-track':
          {
            backgroundColor: `${sidebarColors.accent} !important`,
          },
        '& .react-jinke-music-player-mobile-operation svg': {
          color: `${sidebarColors.navigation} !important`,
        },
        '& .react-jinke-music-player-mobile-operation .item:hover svg': {
          color: `${sidebarColors.activeText} !important`,
        },
      },
      '&.hometube-shared-player .react-jinke-music-player-mobile .react-jinke-music-player-mobile-cover':
        {
          aspectRatio: '16/9',
          height: 'auto',
          width: '85%',
          maxWidth: 600,
          borderRadius: 0,
          border: 0,
          boxShadow: 'none',
          animation: 'none',
          flexShrink: 0,
        },
      '&.hometube-shared-player .hometube-artwork': {
        borderRadius: 0,
        width: '100%',
        height: '100%',
        margin: 0,
        flexShrink: 0,
        animation: 'none',
      },
      '&.hometube-shared-player .music-player-panel .hometube-artwork': {
        width: 'min(18vw, 112px)',
        height: 'auto',
        aspectRatio: '16/9',
        marginRight: 20,
      },
      '& .react-jinke-music-player-mobile-operation .items': {
        flexWrap: 'wrap',
        rowGap: 8,
        padding: 0,
      },
      '@media (max-width:810px) and (orientation:landscape)': {
        '& .music-player-panel': {
          height: 'auto',
          paddingBottom: 'env(safe-area-inset-bottom, 0px)',
        },
        '& .music-player-panel .panel-content': {
          flexWrap: 'wrap',
          rowGap: 8,
          paddingTop: 8,
          paddingBottom: 8,
        },
        '& .progress-bar-content': { flex: '1 1 50%', minWidth: 0 },
        '& .player-content': {
          flex: '1 1 100%',
          flexWrap: 'wrap',
          justifyContent: 'center',
          rowGap: 8,
        },
      },
      '& [aria-disabled="true"]': { opacity: 0.35, cursor: 'default' },
      // The dependency's mini mode is a draggable circular controller. The
      // source-owned MiniPlayer renders the collapsed UI instead; the
      // dependency remains responsible for the expanded player and audio.
      '& .react-jinke-music-player': {
        display: 'none',
      },
      '@media screen and (max-width:810px)': {
        '& .sound-operation': {
          display: 'none',
        },
      },
      '@media (prefers-reduced-motion)': {
        '& .music-player-panel .panel-content div.img-rotate': {
          animation: 'none',
        },
      },
      '& .progress-bar-content': {
        display: 'flex',
        flexDirection: 'column',
      },
      '& .play-mode-title': {
        'pointer-events': 'none',
      },
      '& .music-player-panel .panel-content div.img-rotate': {
        // Customize desktop player when cover animation is disabled
        animationDuration: (props) => !props.enableCoverAnimation && '0s',
        borderRadius: (props) => !props.enableCoverAnimation && '0',
        // Fix cover display when image is not square
        backgroundSize: 'contain',
        backgroundPosition: 'center',
      },
      '& .react-jinke-music-player-mobile .react-jinke-music-player-mobile-cover':
        {
          // Customize mobile player when cover animation is disabled
          borderRadius: (props) => !props.enableCoverAnimation && '0',
          width: (props) => !props.enableCoverAnimation && '85%',
          maxWidth: (props) => !props.enableCoverAnimation && '600px',
          height: (props) => !props.enableCoverAnimation && 'auto',
          // Fix cover display when image is not square
          aspectRatio: '1/1',
          display: 'flex',
        },
      '& .react-jinke-music-player-mobile .react-jinke-music-player-mobile-cover img.cover':
        {
          animationDuration: (props) => !props.enableCoverAnimation && '0s',
          objectFit: 'contain', // Fix cover display when image is not square
        },
      // Hide old singer display
      '& .react-jinke-music-player-mobile .react-jinke-music-player-mobile-singer':
        {
          display: 'none',
        },
      // Hide extra whitespace from switch div
      '& .react-jinke-music-player-mobile .react-jinke-music-player-mobile-switch':
        {
          display: 'none',
        },
      '& .music-player-panel .panel-content .progress-bar-content section.audio-main':
        {
          display: (props) => (props.isRadio ? 'none' : 'inline-flex'),
        },
      '& .react-jinke-music-player-mobile-progress': {
        display: (props) => (props.isRadio ? 'none' : 'flex'),
      },
    },
  }),
  { name: 'NDAudioPlayer' },
)

export default useStyle
