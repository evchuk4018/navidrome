import React from 'react'
import { render, screen } from '@testing-library/react'
import useStyle from './styles'
import { emittedRules } from '../layout/testMediaQuery'

const PlayerStyles = () => {
  const classes = useStyle({ visible: true, enableCoverAnimation: true })
  return (
    <div className={classes.player}>
      <div className="music-player-panel" data-testid="dock" />
      <div className="react-jinke-music-player-mobile" data-testid="overlay" />
    </div>
  )
}

it('keeps the tablet dock above navigation and the expanded mobile overlay above both', () => {
  render(<PlayerStyles />)
  const dock = screen.getByTestId('dock')
  const overlay = screen.getByTestId('overlay')
  const responsive = emittedRules().filter(({ media }) =>
    media.includes('(max-width:959.95px)'),
  )
  const dockRule = responsive.find(({ rule }) =>
    dock.matches(rule.selectorText),
  ).rule
  expect(dockRule.style.getPropertyValue('bottom')).toBe(
    'calc(64px + env(safe-area-inset-bottom, 0px))',
  )
  expect(dockRule.style.getPropertyValue('z-index')).toBe('1001')
  const overlayRule = responsive.find(({ rule }) =>
    overlay.matches(rule.selectorText),
  ).rule
  expect(overlayRule.style.getPropertyValue('z-index')).toBe('1200')
})
