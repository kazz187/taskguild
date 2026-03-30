import { useEffect } from 'react'
import type { ReactNode, MouseEvent } from 'react'
import { X } from 'lucide-react'

/* ─── Main Modal ─── */

export interface ModalProps {
  open: boolean
  onClose: () => void
  /** @default 'md' */
  size?: 'sm' | 'md' | 'lg' | 'full'
  /** @default 50 */
  zIndex?: 50 | 60
  /** @default true */
  closeOnBackdrop?: boolean
  children: ReactNode
}

const sizeClasses: Record<NonNullable<ModalProps['size']>, string> = {
  sm: 'bg-slate-900 border border-slate-700 rounded-xl w-full max-w-sm shadow-2xl',
  md: 'bg-slate-900 border border-slate-700 rounded-xl w-full max-w-lg shadow-2xl max-h-[80vh] flex flex-col',
  lg: 'bg-slate-900 border border-slate-700 rounded-xl w-full max-w-2xl shadow-2xl max-h-[85vh] flex flex-col',
  full: 'bg-slate-900 border border-slate-700 rounded-none md:rounded-xl w-full h-full md:h-auto md:max-w-2xl md:max-h-[85vh] flex flex-col shadow-2xl',
}

const zIndexClasses: Record<NonNullable<ModalProps['zIndex']>, string> = {
  50: 'z-50',
  60: 'z-[60]',
}

export function Modal({
  open,
  onClose,
  size = 'md',
  zIndex = 50,
  closeOnBackdrop = true,
  children,
}: ModalProps) {
  useEffect(() => {
    if (!open) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, onClose])

  if (!open) return null

  const handleBackdropClick = (e: MouseEvent) => {
    if (closeOnBackdrop && e.target === e.currentTarget) {
      onClose()
    }
  }

  const paddingCls = size === 'full' ? 'p-0 md:p-4' : 'p-4'

  return (
    <div
      className={`fixed inset-0 ${zIndexClasses[zIndex]} bg-black/60 flex items-center justify-center ${paddingCls}`}
      onMouseDown={handleBackdropClick}
    >
      <div className={sizeClasses[size]}>
        {children}
      </div>
    </div>
  )
}

/* ─── Modal.Header ─── */

export interface ModalHeaderProps {
  children: ReactNode
  onClose?: () => void
  className?: string
}

function ModalHeader({ children, onClose, className = '' }: ModalHeaderProps) {
  return (
    <div className={`flex items-center justify-between px-4 pt-4 pb-2 shrink-0 ${className}`}>
      <div className="flex items-center gap-2 flex-1 min-w-0">
        {children}
      </div>
      {onClose && (
        <button
          onClick={onClose}
          className="text-gray-500 hover:text-gray-300 transition-colors shrink-0 p-1"
        >
          <X className="w-5 h-5" />
        </button>
      )}
    </div>
  )
}

/* ─── Modal.Body ─── */

export interface ModalBodyProps {
  children: ReactNode
  className?: string
}

function ModalBody({ children, className = '' }: ModalBodyProps) {
  return (
    <div className={`flex-1 overflow-y-auto p-4 space-y-3 min-h-0 ${className}`}>
      {children}
    </div>
  )
}

/* ─── Modal.Footer ─── */

export interface ModalFooterProps {
  children: ReactNode
  /** @default 'end' */
  align?: 'end' | 'between'
  className?: string
}

function ModalFooter({ children, align = 'end', className = '' }: ModalFooterProps) {
  const alignCls = align === 'between' ? 'justify-between' : 'justify-end'

  return (
    <div className={`border-t border-slate-800 px-4 py-3 flex items-center gap-2 shrink-0 ${alignCls} ${className}`}>
      {children}
    </div>
  )
}

/* ─── Attach sub-components ─── */

Modal.Header = ModalHeader
Modal.Body = ModalBody
Modal.Footer = ModalFooter
