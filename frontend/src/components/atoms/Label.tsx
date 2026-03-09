import type { LabelHTMLAttributes } from 'react'

export interface LabelProps extends LabelHTMLAttributes<HTMLLabelElement> {
  /** @default 'xs' */
  size?: 'xs' | 'sm'
}

const sizeClasses: Record<NonNullable<LabelProps['size']>, string> = {
  xs: 'text-xs',
  sm: 'text-sm',
}

export function Label({ size = 'xs', className = '', children, ...rest }: LabelProps) {
  const sizeCls = sizeClasses[size]

  return (
    <label className={`block text-gray-400 mb-1 ${sizeCls} ${className}`} {...rest}>
      {children}
    </label>
  )
}
