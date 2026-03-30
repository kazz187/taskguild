export interface MutationErrorProps {
  error: Error | null | undefined
  className?: string
}

export function MutationError({ error, className = '' }: MutationErrorProps) {
  if (!error) return null
  return (
    <p className={`text-red-400 text-sm mt-3 ${className}`}>{error.message}</p>
  )
}
