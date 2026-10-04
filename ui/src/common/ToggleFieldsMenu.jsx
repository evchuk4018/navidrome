import React, { useState } from 'react'
import PropTypes from 'prop-types'
import IconButton from '@material-ui/core/IconButton'
import Menu from '@material-ui/core/Menu'
import MenuItem from '@material-ui/core/MenuItem'
import { makeStyles, Typography } from '@material-ui/core'
import MoreVertIcon from '@material-ui/icons/MoreVert'
import Checkbox from '@material-ui/core/Checkbox'
import { useDispatch, useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import { setToggleableFields } from '../actions'
import { sidebarColors } from '../layout/sidebarStyles'

const useStyles = makeStyles({
  menuIcon: {
    position: 'relative',
    top: '-0.5em',
  },
  menu: {
    width: '24ch',
  },
  columns: {
    maxHeight: '21rem',
    overflow: 'auto',
  },
  title: {
    margin: '1rem',
  },
  modernMenu: {
    width: '24ch',
    color: `${sidebarColors.text} !important`,
    backgroundColor: '#171017',
    border: `1px solid ${sidebarColors.divider}`,
    borderRadius: 12,
  },
  modernMenuItem: {
    color: `${sidebarColors.text} !important`,
    '&:hover, &.Mui-selected': {
      backgroundColor: `${sidebarColors.selection} !important`,
    },
  },
  modernTitle: {
    color: `${sidebarColors.secondary} !important`,
  },
})

export const ToggleFieldsMenu = ({
  resource,
  topbarComponent: TopBarComponent,
  hideColumns,
  modern,
}) => {
  const [anchorEl, setAnchorEl] = useState(null)
  const dispatch = useDispatch()
  const translate = useTranslate()
  const toggleableColumns = useSelector(
    (state) => state.settings.toggleableFields[resource],
  )
  const omittedColumns =
    useSelector((state) => state.settings.omittedFields[resource]) || []

  const classes = useStyles()
  const open = Boolean(anchorEl)

  const handleOpen = (event) => {
    setAnchorEl(event.currentTarget)
  }
  const handleClose = () => {
    setAnchorEl(null)
  }

  const handleClick = (selectedColumn) => {
    dispatch(
      setToggleableFields({
        [resource]: {
          ...toggleableColumns,
          [selectedColumn]: !toggleableColumns[selectedColumn],
        },
      }),
    )
  }

  return (
    <div className={classes.menuIcon}>
      <IconButton
        aria-label={translate('ra.action.open_menu')}
        aria-controls="long-menu"
        aria-haspopup="true"
        onClick={handleOpen}
      >
        <MoreVertIcon />
      </IconButton>
      <Menu
        id="long-menu"
        anchorEl={anchorEl}
        keepMounted
        open={open}
        onClose={handleClose}
        classes={{
          paper: modern ? classes.modernMenu : classes.menu,
        }}
      >
        {TopBarComponent && <TopBarComponent />}
        {!hideColumns && toggleableColumns ? (
          <div>
            <Typography
              className={modern ? classes.modernTitle : classes.title}
            >
              {translate('ra.toggleFieldsMenu.columnsToDisplay')}
            </Typography>
            <div className={classes.columns}>
              {Object.entries(toggleableColumns).map(([key, val]) =>
                !omittedColumns.includes(key) ? (
                  <MenuItem
                    key={key}
                    className={modern ? classes.modernMenuItem : undefined}
                    onClick={() => handleClick(key)}
                  >
                    <Checkbox checked={val} />
                    {translate(`resources.${resource}.fields.${key}`)}
                  </MenuItem>
                ) : null,
              )}
            </div>
          </div>
        ) : null}
      </Menu>
    </div>
  )
}

ToggleFieldsMenu.propTypes = {
  resource: PropTypes.string.isRequired,
  topbarComponent: PropTypes.elementType,
  hideColumns: PropTypes.bool,
  modern: PropTypes.bool,
}
