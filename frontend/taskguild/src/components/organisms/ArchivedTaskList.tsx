import { useState, useCallback } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listArchivedTasks,
  unarchiveTask,
} from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import type { WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { Archive, ChevronDown, ChevronRight, RotateCcw } from 'lucide-react'
import { Badge } from '../atoms/index.ts'
import { Button } from '../atoms/index.ts'

interface ArchivedTaskListProps {
  projectId: string
  workflowId: string
  statusById: Map<string, WorkflowStatus>
}

export function ArchivedTaskList({ projectId, workflowId, statusById }: ArchivedTaskListProps) {
  const [expanded, setExpanded] = useState(false)

  const { data, refetch } = useQuery(listArchivedTasks, {
    projectId,
    workflowId,
  })
  const archivedTasks = data?.tasks ?? []

  const unarchiveMut = useMutation(unarchiveTask)

  const onEvent = useCallback(() => {
    refetch()
  }, [refetch])

  useEventSubscription(
    [EventType.TASK_ARCHIVED, EventType.TASK_UNARCHIVED],
    projectId,
    onEvent,
  )

  if (archivedTasks.length === 0) return null

  const handleRestore = (taskId: string) => {
    unarchiveMut.mutate(
      { id: taskId },
      {
        onSuccess: () => {
          refetch()
        },
      },
    )
  }

  return (
    <div className="mt-4 mx-4 md:mx-6 mb-4">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 text-sm text-gray-500 hover:text-gray-300 transition-colors"
      >
        {expanded ? (
          <ChevronDown className="w-4 h-4" />
        ) : (
          <ChevronRight className="w-4 h-4" />
        )}
        <Archive className="w-4 h-4" />
        <span>
          Archived Tasks ({archivedTasks.length})
        </span>
      </button>

      {expanded && (
        <div className="mt-3 space-y-2">
          {archivedTasks.map((task) => (
            <ArchivedTaskCard
              key={task.id}
              task={task}
              statusName={statusById.get(task.statusId)?.name ?? task.statusId}
              onRestore={() => handleRestore(task.id)}
              isRestoring={unarchiveMut.isPending}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function ArchivedTaskCard({
  task,
  statusName,
  onRestore,
  isRestoring,
}: {
  task: Task
  statusName: string
  onRestore: () => void
  isRestoring: boolean
}) {
  return (
    <div className="bg-slate-900/30 border border-slate-800 rounded-lg p-3 flex items-center justify-between gap-2">
      <div className="flex items-center gap-3 min-w-0 flex-1">
        <Archive className="w-4 h-4 text-gray-600 shrink-0" />
        <div className="min-w-0 flex-1">
          <h4 className="text-sm text-gray-400 truncate">{task.title}</h4>
          <div className="flex items-center gap-2 mt-0.5">
            <Badge color="green" size="xs" variant="outline" pill>
              {statusName}
            </Badge>
            {task.updatedAt && (
              <span className="text-[10px] text-gray-600">
                {new Date(Number(task.updatedAt.seconds) * 1000).toLocaleDateString()}
              </span>
            )}
          </div>
        </div>
      </div>
      <Button
        variant="ghost"
        size="xs"
        icon={<RotateCcw className="w-3 h-3" />}
        onClick={onRestore}
        disabled={isRestoring}
        className="text-[11px] text-gray-500 hover:text-cyan-400 border border-slate-700 hover:border-cyan-500/40 rounded-md shrink-0"
        title="Restore task"
      >
        Restore
      </Button>
    </div>
  )
}
