import { useState, useEffect, useRef, useCallback, type ReactNode } from 'react'
import { ArrowDown } from 'lucide-react'
import { Button } from '../atoms/index.ts'

/** Threshold in pixels to consider "at the bottom" of scroll container */
const BOTTOM_THRESHOLD = 30

/**
 * Hook that manages auto-scroll behavior for a scrollable container.
 * @param scrollKey - a value that changes when content updates (triggers auto-scroll)
 */
function useAutoScroll(scrollKey: string | number | undefined) {
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
  }, [scrollKey])

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
  /** The text content to display and auto-scroll (simple string mode) */
  content?: string
  /** Children to render inside the <pre> element (rich mode) */
  children?: ReactNode
  /** Optional key to trigger auto-scroll when children change */
  scrollKey?: string | number
  /** CSS classes for the <pre> element */
  className: string
}

/**
 * A <pre> element with auto-scroll behavior.
 * Supports either plain `content` string or rich `children` with a `scrollKey`.
 */
export function AutoScrollPre({ content, children, scrollKey, className }: AutoScrollPreProps) {
  const effectiveScrollKey = scrollKey ?? content
  const { scrollRef, handleScroll, showScrollButton, scrollToBottom } =
    useAutoScroll(effectiveScrollKey)

  return (
    <div className="relative">
      <pre ref={scrollRef} onScroll={handleScroll} className={className}>
        {children ?? content}
      </pre>
      {showScrollButton && (
        <Button
          variant="ghost"
          size="xs"
          icon={<ArrowDown className="w-3 h-3" />}
          onClick={scrollToBottom}
          className="absolute bottom-2 right-4 text-[10px] text-gray-300 bg-slate-800/80 backdrop-blur-sm border border-slate-600/50 hover:bg-slate-700/90 hover:text-white shadow-lg"
        >
          Latest
        </Button>
      )}
    </div>
  )
}
