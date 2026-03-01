import { RequestItem } from './RequestItem'
import { useRequestKeyboard } from '@/hooks/useRequestKeyboard'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'

export function PendingRequestsPanel({
  pendingRequests,
  onRespond,
  isRespondPending,
  enabled = true,
  className,
}: {
  pendingRequests: Interaction[]
  onRespond: (interactionId: string, response: string) => void
  isRespondPending: boolean
  enabled?: boolean
  className?: string
}) {
  const { selectedId, setSelectedId } = useRequestKeyboard({
    pendingRequests,
    onRespond,
    isRespondPending,
    enabled,
  })

  if (pendingRequests.length === 0) return null

  return (
    <div className={className}>
      <div className="flex items-center gap-2 mb-2">
        <p className="text-xs text-gray-500 uppercase tracking-wide">
          Pending Requests
        </p>
        <span className="inline-flex items-center justify-center px-1.5 py-0.5 text-[10px] font-bold bg-amber-500/20 text-amber-400 rounded">
          {pendingRequests.length}
        </span>
        <span className="ml-auto text-[10px] text-gray-600 font-mono hidden sm:inline">
          j/k navigate · y allow · Y always · n deny
        </span>
      </div>
      <div className="space-y-3">
        {pendingRequests.map((interaction) => (
          <RequestItem
            key={interaction.id}
            interaction={interaction}
            onRespond={onRespond}
            isRespondPending={isRespondPending}
            isSelected={interaction.id === selectedId}
            onSelect={() => setSelectedId(interaction.id)}
          />
        ))}
      </div>
    </div>
  )
}
