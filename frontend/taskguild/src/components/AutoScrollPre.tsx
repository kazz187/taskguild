import { useState, useEffect, useRef, useCallback } from 'react'
import { ArrowDown } from 'lucide-react'

/** Threshold in pixels to consider "at the bottom" of scroll container */
const BOTTOM_THRESHOLD = 30

/**
 * Hook that manages auto-scroll behavior for a scrollable container.
 *
 * - Auto-scrolls to bottom when content changes (if enabled)
 * - Pauses auto-scroll when user scrolls up
 * - Resumes auto-scroll when user scrolls back to the bottom
 */
function useAutoScroll(content: string | undefined) {
  const scrollRef = useRef<HTMLPreElement>(null)
  const isAutoScrollEnabled = useRef(true)
  const [showScrollButton, setShowScrollButton] = useState(false)

  const isNearBottom = (el: HTMLElement) => {
    return el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD
  }

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return

    const atBottom = isNearBottom(el)
    isAutoScrollEnabled.current = atBottom
    setShowScrollButton(!atBottom)
  }, [])

  // Auto-scroll when content changes
  useEffect(() => {
    const el = scrollRef.current
    if (!el || !isAutoScrollEnabled.current) return
    el.scrollTop = el.scrollHeight
  }, [content])

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
    isAutoScrollEnabled.current = true
    setShowScrollButton(false)
  }, [])

  return { scrollRef, handleScroll, showScrollButton, scrollToBottom }
}

interface AutoScrollPreProps {
  /** The text content to display and auto-scroll */
  content: string
  /** CSS classes for the <pre> element */
  className: string
}

/**
 * A <pre> element with auto-scroll behavior.
 *
 * Auto-scrolls to the bottom as content grows.
 * Pauses auto-scroll when the user scrolls up to view past output.
 * Resumes auto-scroll when the user scrolls to the bottom.
 * Shows a "scroll to latest" button when auto-scroll is paused.
 */
export function AutoScrollPre({ content, className }: AutoScrollPreProps) {
  const { scrollRef, handleScroll, showScrollButton, scrollToBottom } =
    useAutoScroll(content)

  return (
    <div className="relative">
      <pre ref={scrollRef} onScroll={handleScroll} className={className}>
        {content}
      </pre>
      {showScrollButton && (
        <button
          onClick={scrollToBottom}
          className="absolute bottom-2 right-4 flex items-center gap-1 px-2 py-1 text-[10px] text-gray-300 bg-slate-800/80 backdrop-blur-sm border border-slate-600/50 rounded-md hover:bg-slate-700/90 hover:text-white transition-colors shadow-lg"
        >
          <ArrowDown className="w-3 h-3" />
          <span>Latest</span>
        </button>
      )}
    </div>
  )
}
