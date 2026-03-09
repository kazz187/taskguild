import { forwardRef, type InputHTMLAttributes } from 'react'

export interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label?: string
  /** @default 'cyan' */
  color?: 'cyan' | 'purple' | 'amber'
}

const colorClasses: Record<NonNullable<CheckboxProps['color']>, string> = {
  cyan: 'accent-cyan-500',
  purple: 'accent-purple-500',
  amber: 'accent-amber-500',
}

export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(
  ({ label, color = 'cyan', className = '', disabled, ...rest }, ref) => {
    const colorCls = colorClasses[color]

    if (label) {
      return (
        <label
          className={`flex items-center gap-2 text-sm text-gray-400 ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} ${className}`}
        >
          <input
            ref={ref}
            type="checkbox"
            disabled={disabled}
            className={colorCls}
            {...rest}
          />
          {label}
        </label>
      )
    }

    return (
      <input
        ref={ref}
        type="checkbox"
        disabled={disabled}
        className={`${colorCls} ${className}`}
        {...rest}
      />
    )
  },
)

Checkbox.displayName = 'Checkbox'
