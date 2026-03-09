import type { ReactNode } from 'react'
import { Label } from '../atoms/Label.tsx'

export interface FormFieldProps {
  label?: string
  /** @default 'xs' */
  labelSize?: 'xs' | 'sm'
  children: ReactNode
  /** Optional hint text below the field */
  hint?: string
  className?: string
}

export function FormField({
  label,
  labelSize = 'xs',
  children,
  hint,
  className = '',
}: FormFieldProps) {
  return (
    <div className={className}>
      {label && <Label size={labelSize}>{label}</Label>}
      {children}
      {hint && (
        <p className="text-[10px] text-gray-600 mt-0.5">{hint}</p>
      )}
    </div>
  )
}
