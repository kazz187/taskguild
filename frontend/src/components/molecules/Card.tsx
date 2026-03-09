import type { ReactNode, HTMLAttributes } from 'react'

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode
  /** @default 'default' */
  variant?: 'default' | 'nested' | 'interactive' | 'success' | 'error'
}

const variantClasses: Record<NonNullable<CardProps['variant']>, string> = {
  default: 'bg-slate-900 border border-slate-800 rounded-xl p-4',
  nested: 'bg-slate-800/50 border border-slate-700 rounded p-3',
  interactive: 'bg-slate-800 border border-slate-700 rounded-lg p-3 hover:border-slate-600 transition-colors cursor-pointer',
  success: 'bg-green-500/10 border border-green-500/20 rounded-lg p-3 text-green-400',
  error: 'bg-red-500/10 border border-red-500/20 rounded-lg p-3 text-red-400',
}

export function Card({
  children,
  variant = 'default',
  className = '',
  ...rest
}: CardProps) {
  const variantCls = variantClasses[variant]

  return (
    <div className={`${variantCls} ${className}`} {...rest}>
      {children}
    </div>
  )
}
