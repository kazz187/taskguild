import { forwardRef, type InputHTMLAttributes } from 'react'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  /** @default 'md' */
  inputSize?: 'xs' | 'sm' | 'md'
}

const sizeClasses: Record<NonNullable<InputProps['inputSize']>, string> = {
  xs: 'px-2 py-1.5 text-xs',
  sm: 'px-3 py-1.5 text-sm',
  md: 'px-3 py-2 text-sm',
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ inputSize = 'md', className = '', ...rest }, ref) => {
    const base =
      'w-full bg-slate-800 border border-slate-700 rounded-lg text-white focus:outline-none focus:border-cyan-500 transition-colors placeholder-gray-600'
    const sizeCls = sizeClasses[inputSize]

    return (
      <input
        ref={ref}
        className={`${base} ${sizeCls} ${className}`}
        {...rest}
      />
    )
  },
)

Input.displayName = 'Input'
