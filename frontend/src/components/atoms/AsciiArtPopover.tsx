import { useState, useCallback, type ReactNode } from 'react'
import {
  useFloating,
  offset,
  flip,
  shift,
  autoUpdate,
  FloatingPortal,
  useDismiss,
  useHover,
  useFocus,
  useRole,
  useInteractions,
} from '@floating-ui/react'
import { useLongPress } from '../../hooks/useLongPress.ts'
import { MarkdownDescription } from '../organisms/MarkdownDescription.tsx'

export interface AsciiArtPopoverProps {
  /** Raw description text (may contain code fences or raw ASCII art). */
  content: string
  /** Trigger element — typically an icon. */
  children: ReactNode
}

/** Whether the content contains markdown code fences. */
function hasCodeFence(text: string): boolean {
  return /^`{3,}/m.test(text)
}

/**
 * Popover that displays ASCII art / diagrams on hover (desktop) or
 * long-press (mobile). Renders code-fenced content via MarkdownDescription
 * and raw multi-line text in a monospace `<pre>`.
 */
export function AsciiArtPopover({ content, children }: AsciiArtPopoverProps) {
  const [open, setOpen] = useState(false)

  const { refs, floatingStyles, context } = useFloating({
    open,
    onOpenChange: setOpen,
    placement: 'top',
    middleware: [offset(8), flip(), shift({ padding: 8 })],
    whileElementsMounted: autoUpdate,
  })

  const hover = useHover(context, { move: false, delay: { open: 150 } })
  const focus = useFocus(context)
  const dismiss = useDismiss(context)
  const role = useRole(context, { role: 'dialog' })

  const { getReferenceProps, getFloatingProps } = useInteractions([
    hover,
    focus,
    dismiss,
    role,
  ])

  // Mobile long-press support
  const longPress = useLongPress(
    useCallback(() => setOpen(true), []),
  )

  return (
    <>
      <span
        ref={refs.setReference}
        {...getReferenceProps()}
        {...longPress}
        onClick={(e) => e.stopPropagation()}
        onMouseDown={(e) => e.stopPropagation()}
        className="inline-flex cursor-help"
      >
        {children}
      </span>
      {open && (
        <FloatingPortal>
          <div
            ref={refs.setFloating}
            style={floatingStyles}
            {...getFloatingProps()}
            onClick={(e) => e.stopPropagation()}
            className="z-50 p-3 bg-slate-800 border border-slate-600 rounded-lg shadow-xl max-w-[600px] max-h-[400px] overflow-auto"
          >
            {hasCodeFence(content) ? (
              <MarkdownDescription content={content} className="text-xs" />
            ) : (
              <pre className="font-mono text-xs text-gray-300 whitespace-pre leading-relaxed">
                {content}
              </pre>
            )}
          </div>
        </FloatingPortal>
      )}
    </>
  )
}
