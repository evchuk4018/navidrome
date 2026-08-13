import { describe, expect, it } from 'vitest'
import { isRadioPlanning, radioPlanningMessage } from './radioPlanning'

describe('personal radio planning status', () => {
  it('keeps polling while the server retries a failed discovery', () => {
    expect(isRadioPlanning('retrying')).toBe(true)
    expect(radioPlanningMessage('retrying')).toContain('trying another')
  })

  it('stops polling once a queue is ready or no discovery is available', () => {
    expect(isRadioPlanning('ready')).toBe(false)
    expect(isRadioPlanning('no_discovery')).toBe(false)
  })
})
