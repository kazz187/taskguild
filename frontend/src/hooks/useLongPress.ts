import { useRef, useCallback } from 'react'
import type { TouchEvent as ReactTouchEvent } from 'react'

/**
 * Hook that fires a callback after a sustained touch (long-press).
 * Cancels if the finger moves more than `threshold` px or lifts early.
 * Also suppresses the native context-menu that a long-press triggers.
 */
export function useLongPress(
  onLongPress: () => void,
  ms = 500,
  threshold = 10,
) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const startRef = useRef<{ x: number; y: number } | null>(null)

  const clear = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
    startRef.current = null
  }, [])

  const onTouchStart = useCallback(
    (e: ReactTouchEvent) => {
      const touch = e.touches[0]
      startRef.current = { x: touch.clientX, y: touch.clientY }

      timerRef.current = setTimeout(() => {
        timerRef.current = null
        onLongPress()
      }, ms)

      // Suppress native context-menu during this press
      const target = e.currentTarget as HTMLElement
      const suppress = (ev: Event) => {
        ev.preventDefault()
        target.removeEventListener('contextmenu', suppress)
      }
      target.addEventListener('contextmenu', suppress, { once: true })
    },
    [onLongPress, ms],
  )

  const onTouchMove = useCallback(
    (e: ReactTouchEvent) => {
      if (!startRef.current) return
      const touch = e.touches[0]
      const dx = touch.clientX - startRef.current.x
      const dy = touch.clientY - startRef.current.y
      if (dx * dx + dy * dy > threshold * threshold) {
        clear()
      }
    },
    [clear, threshold],
  )

  const onTouchEnd = useCallback(() => {
    clear()
  }, [clear])

  return { onTouchStart, onTouchMove, onTouchEnd }
}
