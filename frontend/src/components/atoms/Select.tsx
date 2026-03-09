import { forwardRef, type SelectHTMLAttributes } from 'react'

export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  /** @default 'md' */
  selectSize?: 'xs' | 'sm' | 'md'
}

const sizeClasses: Record<NonNullable<SelectProps['selectSize']>, string> = {
  xs: 'px-2 py-1.5 text-xs',
  sm: 'px-3 py-1.5 text-sm',
  md: 'px-3 py-2 text-sm',
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ selectSize = 'md', className = '', children, ...rest }, ref) => {
    const base =
      'w-full bg-slate-800 border border-slate-700 rounded-lg text-white focus:outline-none focus:border-cyan-500 transition-colors'
    const sizeCls = sizeClasses[selectSize]

    return (
      <select
        ref={ref}
        className={`${base} ${sizeCls} ${className}`}
        {...rest}
      >
        {children}
      </select>
    )
  },
)

Select.displayName = 'Select'
