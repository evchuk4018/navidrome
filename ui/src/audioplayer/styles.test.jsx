import React from 'react'
import { render, screen } from '@testing-library/react'
import useStyle from './styles'
import { emittedRules } from '../layout/testMediaQuery'

const PlayerStyles = () => {
  const classes = useStyle({ visible: true, enableCoverAnimation: true })
  return (
    <div className={classes.player} data-testid="player">
      <div className="music-player-panel" data-testid="dock" />
      <div className="react-jinke-music-player-mobile" data-testid="overlay" />
      <div className="audio-lists-panel" data-testid="queue">
        <div className="audio-lists-panel-header">
          <h2 className="audio-lists-panel-header-title">
            Queue
            <span className="audio-lists-panel-header-actions">
              <span data-testid="queue-action">
                <svg aria-hidden="true" />
              </span>
            </span>
          </h2>
        </div>
        <div className="audio-lists-panel-content">
          <ul>
            <li className="audio-item playing" data-testid="playing-row">
              <svg data-testid="playing-icon" aria-hidden="true" />
              <span className="player-singer">Artist</span>
            </li>
          </ul>
        </div>
      </div>
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

it('keeps the queue above the expanded player with an opaque pink surface', () => {
  render(<PlayerStyles />)
  const queue = screen.getByTestId('queue')
  const queueRule = emittedRules().find(
    ({ rule }) =>
      queue.matches(rule.selectorText) &&
      rule.style.getPropertyValue('z-index') === '1250',
  ).rule
  const overlayRule = emittedRules()
    .filter(({ media }) => media.includes('(max-width:959.95px)'))
    .find(({ rule }) =>
      screen.getByTestId('overlay').matches(rule.selectorText),
    ).rule

  expect(queueRule.style.getPropertyValue('background-color')).toBe('#0d0d0d')
  expect(queueRule.style.getPropertyPriority('background-color')).toBe(
    'important',
  )
  expect(Number(queueRule.style.getPropertyValue('z-index'))).toBeGreaterThan(
    Number(overlayRule.style.getPropertyValue('z-index')),
  )
})

it('keeps queue row colors above a late light-theme player stylesheet', () => {
  render(<PlayerStyles />)
  const player = screen.getByTestId('player')
  const queue = screen.getByTestId('queue')
  const row = screen.getByTestId('playing-row')
  const icon = screen.getByTestId('playing-icon')
  const playerClass = player.className.split(/\s+/)[0]
  const lightThemeSelector =
    '.react-jinke-music-player-main.light-theme .audio-lists-panel .audio-item.playing'
  const lateTheme = document.createElement('style')
  lateTheme.textContent = `
    ${lightThemeSelector},
    ${lightThemeSelector} svg {
      color: #31c27c !important;
    }
  `
  document.head.appendChild(lateTheme)
  player.classList.add('react-jinke-music-player-main', 'light-theme')

  const queueRule = emittedRules().find(
    ({ rule }) =>
      queue.matches(rule.selectorText) &&
      rule.style.getPropertyValue('z-index') === '1250',
  ).rule
  const playingRowRule = emittedRules().find(
    ({ rule }) =>
      row.matches(rule.selectorText) &&
      rule.style.getPropertyValue('color') === '#ff91be',
  ).rule
  const playingIconRule = emittedRules().find(
    ({ rule }) =>
      icon.matches(rule.selectorText) &&
      rule.style.getPropertyValue('color') === '#ff2a7f',
  ).rule

  expect(queueRule.selectorText).toContain(`.${playerClass}.${playerClass}`)
  expect(playingRowRule.selectorText).toContain(
    `.${playerClass}.${playerClass}`,
  )
  expect(playingIconRule.selectorText).toContain(
    `.${playerClass}.${playerClass}`,
  )
  expect(playingRowRule.style.getPropertyPriority('color')).toBe('important')
  expect(playingIconRule.style.getPropertyPriority('color')).toBe('important')
  const classSpecificity = (selector) =>
    (selector.match(/\.[A-Za-z0-9_-]+/g) || []).length
  expect(classSpecificity(playingRowRule.selectorText)).toBeGreaterThan(
    classSpecificity(lightThemeSelector),
  )
  expect(classSpecificity(playingIconRule.selectorText)).toBeGreaterThan(
    classSpecificity(`${lightThemeSelector} svg`),
  )

  lateTheme.remove()
})
