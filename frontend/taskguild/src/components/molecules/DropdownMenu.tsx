import { useLayoutEffect, type ReactNode, type RefObject } from 'react'
import {
  useFloating,
  offset,
  flip,
  shift,
  autoUpdate,
  FloatingPortal,
  useDismiss,
  useInteractions,
} from '@floating-ui/react'

/* ─── DropdownMenu ─── */

export interface DropdownMenuProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** @default 'right' */
  align?: 'left' | 'right'
  /** Reference element (trigger button) to anchor the dropdown */
  triggerRef?: RefObject<HTMLElement | null>
  children: ReactNode
  className?: string
}

export function DropdownMenu({
  open,
  onOpenChange,
  align = 'right',
  triggerRef,
  children,
  className = '',
}: DropdownMenuProps) {
  const { refs, floatingStyles, context } = useFloating({
    open,
    onOpenChange,
    placement: align === 'left' ? 'bottom-start' : 'bottom-end',
    middleware: [offset(4), flip(), shift({ padding: 8 })],
    whileElementsMounted: autoUpdate,
  })

  const dismiss = useDismiss(context)
  const { getFloatingProps } = useInteractions([dismiss])

  // Sync external trigger ref to floating reference element.
  // useLayoutEffect ensures the reference is set before paint to avoid a
  // frame at (0,0) when the dropdown first opens.
  useLayoutEffect(() => {
    if (triggerRef?.current) {
      refs.setReference(triggerRef.current)
    }
  }, [triggerRef, refs, open])

  if (!open) return null

  return (
    <FloatingPortal>
      <div
        ref={refs.setFloating}
        style={floatingStyles}
        {...getFloatingProps()}
        className={`z-50 bg-slate-800 border border-slate-600 rounded-lg shadow-xl py-1 min-w-[160px] animate-fade-in-down ${className}`}
      >
        {children}
      </div>
    </FloatingPortal>
  )
}

/* ─── DropdownMenu.Item ─── */

export interface DropdownMenuItemProps {
  children: ReactNode
  onClick: () => void
  disabled?: boolean
  /** @default 'default' */
  variant?: 'default' | 'danger' | 'warning'
  className?: string
}

const variantClasses: Record<NonNullable<DropdownMenuItemProps['variant']>, string> = {
  default: 'text-gray-300 hover:bg-slate-700 hover:text-white',
  danger: 'text-gray-400 hover:bg-slate-700 hover:text-red-300',
  warning: 'text-gray-400 hover:bg-slate-700 hover:text-amber-300',
}

function DropdownMenuItem({
  children,
  onClick,
  disabled = false,
  variant = 'default',
  className = '',
}: DropdownMenuItemProps) {
  const variantCls = disabled
    ? 'text-gray-600 cursor-not-allowed'
    : variantClasses[variant]

  return (
    <button
      onClick={disabled ? undefined : onClick}
      disabled={disabled}
      className={`w-full text-left px-3 py-2 text-sm transition-colors flex items-center gap-2 ${variantCls} ${className}`}
    >
      {children}
    </button>
  )
}

/* ─── Attach sub-components ─── */

DropdownMenu.Item = DropdownMenuItem
