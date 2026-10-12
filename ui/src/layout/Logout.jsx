import React, { useCallback } from 'react'
import { useDispatch } from 'react-redux'
import { Logout as RALogout } from 'react-admin'
import { clearQueue } from '../actions'
import { playbackSession } from '../playbacksession/controller'

const Logout = (props) => {
  const dispatch = useDispatch()
  const handleClick = useCallback(() => {
    playbackSession.identityChanged()
    dispatch(clearQueue())
  }, [dispatch])

  return (
    <span onClick={handleClick}>
      <RALogout {...props} />
    </span>
  )
}

export default Logout
