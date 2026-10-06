import React, { useState } from 'react'
import { Button, IconButton, Popover } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import Brightness3OutlinedIcon from '@material-ui/icons/Brightness3Outlined'
import { sidebarColors } from '../layout/sidebarStyles'
import { useSleepTimer } from './SleepTimerContext'

const useStyles = makeStyles((theme) => ({
  moon: {
    color: sidebarColors.navigation,
    '&[data-active="true"]': {
      color: `${sidebarColors.accent} !important`,
      '& svg': { color: `${sidebarColors.accent} !important` },
    },
  },
  popup: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(1),
    padding: theme.spacing(1.5),
    color: sidebarColors.text,
    backgroundColor: sidebarColors.selection,
    border: `1px solid ${sidebarColors.divider}`,
    borderRadius: 12,
    '& button': { color: sidebarColors.activeText, minHeight: 44 },
  },
  remaining: {
    minWidth: 60,
    textAlign: 'center',
    fontVariantNumeric: 'tabular-nums',
  },
}))

const SleepTimerButton = ({ className, iconClassName }) => {
  const classes = useStyles()
  const timer = useSleepTimer()
  const [anchor, setAnchor] = useState(null)
  const close = () => setAnchor(null)
  const remaining = `${Math.floor(timer.remainingSeconds / 60)}:${String(timer.remainingSeconds % 60).padStart(2, '0')}`
  return (
    <>
      <IconButton
        className={`${classes.moon} ${className || ''}`}
        aria-label="Sleep timer"
        title="Sleep timer"
        aria-haspopup="dialog"
        aria-expanded={Boolean(anchor)}
        data-active={timer.isActive}
        onKeyDown={(event) => event.stopPropagation()}
        onKeyUp={(event) => event.stopPropagation()}
        onClick={(event) => {
          event.stopPropagation()
          setAnchor(event.currentTarget)
        }}
      >
        <Brightness3OutlinedIcon className={iconClassName} />
      </IconButton>
      <Popover
        open={Boolean(anchor)}
        anchorEl={anchor}
        container={document.fullscreenElement || undefined}
        onClose={close}
        anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
        transformOrigin={{ vertical: 'bottom', horizontal: 'center' }}
        marginThreshold={12}
        classes={{ paper: classes.popup }}
        PaperProps={{
          role: 'dialog',
          'aria-label': 'Sleep timer',
          onKeyDown: (event) => {
            if (event.key === 'Escape') close()
            event.stopPropagation()
          },
          onKeyUp: (event) => event.stopPropagation(),
        }}
      >
        {timer.isActive ? (
          <>
            <span className={classes.remaining} aria-label="Time remaining">
              {remaining}
            </span>
            <Button
              onClick={() => {
                timer.stop()
                close()
              }}
            >
              Stop
            </Button>
          </>
        ) : (
          [15, 30, 60].map((minutes) => (
            <Button
              key={minutes}
              onClick={() => {
                timer.start(minutes)
                close()
              }}
            >
              {minutes} min
            </Button>
          ))
        )}
      </Popover>
    </>
  )
}

export default SleepTimerButton
