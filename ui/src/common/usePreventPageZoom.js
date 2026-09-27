import { useEffect } from 'react'

const zoomKeys = new Set(['+', '=', '-', '_', '0'])
const zoomCodes = new Set([
  'Equal',
  'Minus',
  'Digit0',
  'NumpadAdd',
  'NumpadSubtract',
  'Numpad0',
])

export const usePreventPageZoom = () => {
  useEffect(() => {
    const preventZoom = (event) => event.preventDefault()
    const preventPinch = (event) => {
      if (event.touches.length > 1) event.preventDefault()
    }
    const preventWheelZoom = (event) => {
      if (event.ctrlKey || event.metaKey) event.preventDefault()
    }
    const preventKeyZoom = (event) => {
      if (
        (event.ctrlKey || event.metaKey) &&
        !event.altKey &&
        (zoomKeys.has(event.key) || zoomCodes.has(event.code))
      ) {
        event.preventDefault()
      }
    }
    const capture = { capture: true, passive: false }

    document.addEventListener('touchstart', preventPinch, capture)
    document.addEventListener('touchmove', preventPinch, capture)
    document.addEventListener('gesturestart', preventZoom, capture)
    document.addEventListener('gesturechange', preventZoom, capture)
    document.addEventListener('wheel', preventWheelZoom, capture)
    document.addEventListener('keydown', preventKeyZoom, true)

    return () => {
      document.removeEventListener('touchstart', preventPinch, capture)
      document.removeEventListener('touchmove', preventPinch, capture)
      document.removeEventListener('gesturestart', preventZoom, capture)
      document.removeEventListener('gesturechange', preventZoom, capture)
      document.removeEventListener('wheel', preventWheelZoom, capture)
      document.removeEventListener('keydown', preventKeyZoom, true)
    }
  }, [])
}
