import { RefreshCw } from 'lucide-react'
import { Button } from '../atoms/index.ts'

export interface SyncButtonProps {
  onClick: () => void
  isPending: boolean
  label?: string
  title?: string
  disabled?: boolean
}

export function SyncButton({ onClick, isPending, label = 'Sync from Repo', title, disabled }: SyncButtonProps) {
  return (
    <Button
      variant="secondary"
      size="sm"
      icon={<RefreshCw className={`w-4 h-4 ${isPending ? 'animate-spin' : ''}`} />}
      onClick={onClick}
      disabled={disabled ?? isPending}
      title={title}
      className="border border-slate-700 hover:border-slate-600"
    >
      <span className="hidden sm:inline">{label}</span>
      <span className="sm:hidden">Sync</span>
    </Button>
  )
}
