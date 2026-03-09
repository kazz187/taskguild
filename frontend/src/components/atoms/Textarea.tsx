import { forwardRef, type TextareaHTMLAttributes } from 'react'

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  /** @default 'md' */
  textareaSize?: 'sm' | 'md'
  /** Use monospace font (for code) */
  mono?: boolean
}

const sizeClasses: Record<NonNullable<TextareaProps['textareaSize']>, string> = {
  sm: 'px-3 py-2 text-sm min-h-[100px]',
  md: 'px-3 py-2 text-sm min-h-[150px] md:min-h-[200px]',
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ textareaSize = 'md', mono = false, className = '', ...rest }, ref) => {
    const base =
      'w-full bg-slate-800 border border-slate-700 rounded-lg text-white focus:outline-none focus:border-cyan-500 transition-colors placeholder-gray-600'
    const sizeCls = sizeClasses[textareaSize]
    const fontCls = mono ? 'font-mono' : ''

    return (
      <textarea
        ref={ref}
        className={`${base} ${sizeCls} ${fontCls} ${className}`}
        {...rest}
      />
    )
  },
)

Textarea.displayName = 'Textarea'
