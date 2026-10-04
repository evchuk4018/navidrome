import React from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'

const listContext = vi.hoisted(() => ({
  currentSort: { field: 'name', order: 'ASC' },
  setSort: vi.fn(),
  setPage: vi.fn(),
}))

vi.mock('react-admin', () => ({
  Button: ({ children, onClick, disabled, ...props }) => (
    <button onClick={onClick} disabled={disabled} {...props}>
      {children}
    </button>
  ),
  CreateButton: ({ children }) => <button>{children}</button>,
  TopToolbar: ({ children }) => <div>{children}</div>,
  sanitizeListRestProps: () => ({}),
  useListContext: () => listContext,
  useMediaQuery: () => true,
  useTranslate: () => (key, options) => options?._ || key,
}))

vi.mock('../common', () => ({ ToggleFieldsMenu: () => null }))

import PlaylistListActions from './PlaylistListActions'

describe('<PlaylistListActions />', () => {
  it('restores the liked-songs-first default and resets the page', () => {
    render(
      <ThemeProvider theme={createTheme()}>
        <PlaylistListActions filters={<span />} />
      </ThemeProvider>,
    )

    const button = screen.getByRole('button', { name: 'Default order' })
    fireEvent.click(button)

    expect(listContext.setSort).toHaveBeenCalledWith('liked_songs_first', 'ASC')
    expect(listContext.setPage).toHaveBeenCalledWith(1)
  })
})
