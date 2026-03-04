import { useRef, useEffect, type ReactNode } from 'react'

/* ─── DropdownMenu ─── */

export interface DropdownMenuProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** @default 'right' */
  align?: 'left' | 'right'
  children: ReactNode
  className?: string
}

export function DropdownMenu({
  open,
  onOpenChange,
  align = 'right',
  children,
  className = '',
}: DropdownMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null)

  // Close on outside click
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onOpenChange(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open, onOpenChange])

  if (!open) return null

  const alignCls = align === 'left' ? 'left-0' : 'right-0'

  return (
    <div
      ref={menuRef}
      className={`absolute ${alignCls} top-full mt-1 z-30 bg-slate-800 border border-slate-600 rounded-lg shadow-xl py-1 min-w-[160px] animate-fade-in-down ${className}`}
    >
      {children}
    </div>
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
