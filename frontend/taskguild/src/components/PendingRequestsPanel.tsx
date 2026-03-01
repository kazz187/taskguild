import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { RequestItem } from './RequestItem'
import { useRequestKeyboard } from '@/hooks/useRequestKeyboard'
import { shortId } from '@/lib/id'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'

interface TaskGroup {
  taskId: string
  taskTitle: string
  interactions: Interaction[]
}

export function PendingRequestsPanel({
  pendingRequests,
  onRespond,
  isRespondPending,
  enabled = true,
  className,
  taskMap,
  projectId,
}: {
  pendingRequests: Interaction[]
  onRespond: (interactionId: string, response: string) => void
  isRespondPending: boolean
  enabled?: boolean
  className?: string
  taskMap?: Map<string, string>
  projectId?: string
}) {
  const { selectedId, setSelectedId } = useRequestKeyboard({
    pendingRequests,
    onRespond,
    isRespondPending,
    enabled,
  })

  // Group pending requests by taskId, preserving order by earliest createdAt
  const taskGroups = useMemo(() => {
    const groupMap = new Map<string, Interaction[]>()
    const groupOrder: string[] = []

    for (const interaction of pendingRequests) {
      const tid = interaction.taskId
      if (!groupMap.has(tid)) {
        groupMap.set(tid, [])
        groupOrder.push(tid)
      }
      groupMap.get(tid)!.push(interaction)
    }

    return groupOrder.map((taskId): TaskGroup => ({
      taskId,
      taskTitle: taskMap?.get(taskId) ?? shortId(taskId),
      interactions: groupMap.get(taskId)!,
    }))
  }, [pendingRequests, taskMap])

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
      <div className="space-y-4">
        {taskGroups.map((group) => (
          <div key={group.taskId}>
            {/* Task group header */}
            <div className="flex items-center gap-2 mb-1.5">
              {projectId ? (
                <Link
                  to="/projects/$projectId/tasks/$taskId"
                  params={{ projectId, taskId: group.taskId }}
                  className="text-[11px] text-cyan-400 hover:text-cyan-300 font-medium truncate transition-colors"
                >
                  {group.taskTitle}
                </Link>
              ) : (
                <span className="text-[11px] text-cyan-400 font-medium truncate">
                  {group.taskTitle}
                </span>
              )}
              <div className="flex-1 border-t border-slate-700/50" />
            </div>
            {/* Request items within this task group */}
            <div className="space-y-2">
              {group.interactions.map((interaction) => (
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
        ))}
      </div>
    </div>
  )
}
