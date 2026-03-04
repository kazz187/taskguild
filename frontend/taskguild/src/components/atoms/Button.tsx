import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost'
  size?: 'xs' | 'sm' | 'md'
  icon?: ReactNode
  iconOnly?: boolean
  loading?: boolean
}

const variantClasses: Record<NonNullable<ButtonProps['variant']>, string> = {
  primary: 'bg-cyan-600 hover:bg-cyan-500 text-white',
  secondary: 'text-gray-400 hover:text-white',
  danger: 'bg-amber-600 hover:bg-amber-500 text-white',
  ghost: 'text-gray-500 hover:text-gray-300 hover:bg-slate-800',
}

const sizeClasses: Record<NonNullable<ButtonProps['size']>, string> = {
  xs: 'px-2 py-1 text-xs',
  sm: 'px-3 py-1.5 text-xs',
  md: 'px-4 py-2 text-sm',
}

const iconOnlySizeClasses: Record<NonNullable<ButtonProps['size']>, string> = {
  xs: 'p-1',
  sm: 'p-1.5',
  md: 'p-2',
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    {
      variant = 'primary',
      size = 'sm',
      icon,
      iconOnly = false,
      loading = false,
      disabled,
      className = '',
      children,
      ...rest
    },
    ref,
  ) => {
    const base = 'inline-flex items-center justify-center gap-1.5 rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed'
    const variantCls = variantClasses[variant]
    const sizeCls = iconOnly ? iconOnlySizeClasses[size] : sizeClasses[size]

    return (
      <button
        ref={ref}
        disabled={disabled || loading}
        className={`${base} ${variantCls} ${sizeCls} ${className}`}
        {...rest}
      >
        {icon && <span className="shrink-0">{icon}</span>}
        {!iconOnly && children}
      </button>
    )
  },
)

Button.displayName = 'Button'
