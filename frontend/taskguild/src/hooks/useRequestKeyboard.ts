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
  // Track interaction IDs that have already been responded to (synchronous guard against double-send)
  const respondedIdsRef = useRef<Set<string>>(new Set())

  // Clean up respondedIds when interactions disappear from the pending list
  useEffect(() => {
    const currentIds = new Set(pendingRequests.map((r) => r.id))
    for (const id of respondedIdsRef.current) {
      if (!currentIds.has(id)) {
        respondedIdsRef.current.delete(id)
      }
    }
  }, [pendingRequests])

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

  /**
   * Respond to an interaction with a synchronous double-send guard.
   * Marks the interaction ID as responded immediately (via ref, not state)
   * so that even rapid key presses within the same render frame are blocked.
   * Also advances selection to the next pending request.
   */
  const guardedRespond = useCallback(
    (interactionId: string, response: string) => {
      if (respondedIdsRef.current.has(interactionId)) return
      respondedIdsRef.current.add(interactionId)

      onRespond(interactionId, response)

      // Advance selection to the next un-responded pending request
      const currentIndex = pendingRequests.findIndex((r) => r.id === interactionId)
      const remaining = pendingRequests.filter(
        (r) => r.id !== interactionId && !respondedIdsRef.current.has(r.id),
      )
      if (remaining.length === 0) {
        setSelectedId(null)
      } else {
        // Pick the request at or after the current index
        const nextAfter = pendingRequests.find(
          (r, idx) => idx > currentIndex && !respondedIdsRef.current.has(r.id),
        )
        setSelectedId(nextAfter?.id ?? remaining[0].id)
      }
    },
    [onRespond, pendingRequests],
  )

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
          guardedRespond(selectedId, value)
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
            guardedRespond(selectedId, option.value)
          }
          break
        }
      }
    },
    [enabled, pendingRequests, selectedId, isRespondPending, guardedRespond],
  )

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  return { selectedId, setSelectedId }
}
