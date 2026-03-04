import type { ReactNode } from 'react'

export interface BadgeProps {
  children: ReactNode
  /** @default 'gray' */
  color?: 'cyan' | 'green' | 'amber' | 'red' | 'blue' | 'purple' | 'orange' | 'yellow' | 'gray'
  /** @default 'sm' */
  size?: 'xs' | 'sm'
  /** @default 'solid' */
  variant?: 'solid' | 'outline'
  /** Use rounded-full (pill shape) instead of rounded */
  pill?: boolean
  icon?: ReactNode
  className?: string
}

const colorMap: Record<
  NonNullable<BadgeProps['color']>,
  { solid: string; outline: string }
> = {
  cyan: {
    solid: 'bg-cyan-500/10 text-cyan-400',
    outline: 'bg-cyan-500/10 text-cyan-400 border border-cyan-500/20',
  },
  green: {
    solid: 'bg-green-500/10 text-green-400',
    outline: 'bg-green-500/10 text-green-400 border border-green-500/20',
  },
  amber: {
    solid: 'bg-amber-500/20 text-amber-400',
    outline: 'bg-amber-500/10 text-amber-400 border border-amber-500/20',
  },
  red: {
    solid: 'bg-red-500/10 text-red-400',
    outline: 'bg-red-500/10 text-red-400 border border-red-500/20',
  },
  blue: {
    solid: 'bg-blue-500/20 text-blue-400',
    outline: 'bg-blue-500/10 text-blue-400 border border-blue-500/20',
  },
  purple: {
    solid: 'bg-purple-500/10 text-purple-400',
    outline: 'bg-purple-500/10 text-purple-400 border border-purple-500/20',
  },
  orange: {
    solid: 'bg-orange-500/10 text-orange-400',
    outline: 'bg-orange-500/10 text-orange-400 border border-orange-500/20',
  },
  yellow: {
    solid: 'bg-yellow-500/10 text-yellow-400',
    outline: 'bg-yellow-500/10 text-yellow-400 border border-yellow-500/20',
  },
  gray: {
    solid: 'bg-gray-500/20 text-gray-300',
    outline: 'bg-slate-800 text-gray-500 border border-slate-700',
  },
}

const sizeClasses: Record<NonNullable<BadgeProps['size']>, string> = {
  xs: 'px-1.5 py-0.5 text-[10px]',
  sm: 'px-2.5 py-0.5 text-xs',
}

export function Badge({
  children,
  color = 'gray',
  size = 'sm',
  variant = 'solid',
  pill = false,
  icon,
  className = '',
}: BadgeProps) {
  const colorCls = colorMap[color][variant]
  const sizeCls = sizeClasses[size]
  const roundedCls = pill ? 'rounded-full' : 'rounded'

  return (
    <span
      className={`inline-flex items-center gap-1 font-medium shrink-0 ${roundedCls} ${colorCls} ${sizeCls} ${className}`}
    >
      {icon && <span className="shrink-0">{icon}</span>}
      {children}
    </span>
  )
}
