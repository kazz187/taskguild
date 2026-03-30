import type { LucideIcon } from 'lucide-react'

export interface EmptyStateProps {
  icon: LucideIcon
  message: string
  hint?: string
}

export function EmptyState({ icon: Icon, message, hint }: EmptyStateProps) {
  return (
    <div className="text-center py-12 text-gray-500">
      <Icon className="w-8 h-8 mx-auto mb-3 opacity-30" />
      <p className="text-sm">{message}</p>
      {hint && <p className="text-xs mt-1 text-gray-600">{hint}</p>}
    </div>
  )
}
