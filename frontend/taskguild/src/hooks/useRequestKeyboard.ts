import { useState, useEffect, useRef, useCallback } from 'react'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'

interface UseRequestKeyboardOptions {
  pendingRequests: Interaction[]
  onRespond: (interactionId: string, response: string) => void
  isRespondPending?: boolean
  enabled?: boolean
}

interface UseRequestKeyboardResult {
  selectedId: string | null
  setSelectedId: (id: string | null) => void
}

function isInputFocused(): boolean {
  const el = document.activeElement
  if (!el) return false
  const tag = el.tagName.toLowerCase()
  return (
    tag === 'input' ||
    tag === 'textarea' ||
    tag === 'select' ||
    (el as HTMLElement).isContentEditable
  )
}

function getPermissionShortcutValue(key: string): string | null {
  switch (key) {
    case 'y':
      return 'allow'
    case 'Y':
      return 'always_allow'
    case 'n':
      return 'deny'
    default:
      return null
  }
}

export function useRequestKeyboard({
  pendingRequests,
  onRespond,
  isRespondPending = false,
  enabled = true,
}: UseRequestKeyboardOptions): UseRequestKeyboardResult {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const prevRequestsRef = useRef<Interaction[]>([])

  // Auto-select when exactly 1 pending request, or re-select when selected request disappears
  useEffect(() => {
    if (pendingRequests.length === 0) {
      setSelectedId(null)
      prevRequestsRef.current = pendingRequests
      return
    }

    if (pendingRequests.length === 1) {
      setSelectedId(pendingRequests[0].id)
      prevRequestsRef.current = pendingRequests
      return
    }

    // If the currently selected ID is still in the list, keep it
    if (selectedId && pendingRequests.some((r) => r.id === selectedId)) {
      prevRequestsRef.current = pendingRequests
      return
    }

    // Selected request disappeared — re-select at the same index position
    if (selectedId) {
      const prevIndex = prevRequestsRef.current.findIndex((r) => r.id === selectedId)
      const clampedIndex = Math.min(Math.max(prevIndex, 0), pendingRequests.length - 1)
      setSelectedId(pendingRequests[clampedIndex]?.id ?? null)
    }

    prevRequestsRef.current = pendingRequests
  }, [pendingRequests, selectedId])

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (!enabled) return
      if (isInputFocused()) return
      if (isRespondPending) return
      if (pendingRequests.length === 0) return

      const currentIndex = pendingRequests.findIndex((r) => r.id === selectedId)

      switch (e.key) {
        case 'j': {
          e.preventDefault()
          if (currentIndex < 0) {
            // Nothing selected — select first
            setSelectedId(pendingRequests[0].id)
          } else {
            // Wrap around
            const next = (currentIndex + 1) % pendingRequests.length
            setSelectedId(pendingRequests[next].id)
          }
          break
        }
        case 'k': {
          e.preventDefault()
          if (currentIndex < 0) {
            // Nothing selected — select last
            setSelectedId(pendingRequests[pendingRequests.length - 1].id)
          } else {
            // Wrap around
            const prev = (currentIndex - 1 + pendingRequests.length) % pendingRequests.length
            setSelectedId(pendingRequests[prev].id)
          }
          break
        }
        case 'y':
        case 'Y':
        case 'n': {
          if (!selectedId) return
          const selected = pendingRequests.find((r) => r.id === selectedId)
          if (!selected || selected.type !== InteractionType.PERMISSION_REQUEST) return
          const value = getPermissionShortcutValue(e.key)
          if (!value) return
          // Verify the option actually exists in the interaction
          if (!selected.options.some((opt) => opt.value === value)) return
          e.preventDefault()
          onRespond(selectedId, value)
          break
        }
        default: {
          // Number keys 1-9 for QUESTION type options
          const num = parseInt(e.key, 10)
          if (num >= 1 && num <= 9) {
            if (!selectedId) return
            const selected = pendingRequests.find((r) => r.id === selectedId)
            if (!selected) return
            if (num > selected.options.length) return
            e.preventDefault()
            const option = selected.options[num - 1]
            onRespond(selectedId, option.value)
          }
          break
        }
      }
    },
    [enabled, pendingRequests, selectedId, isRespondPending, onRespond],
  )

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  return { selectedId, setSelectedId }
}
