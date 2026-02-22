import { useCallback, useMemo } from 'react'
import { useQuery } from '@connectrpc/connect-query'
import { listTasks } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import type { Workflow, WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { TaskCard } from './TaskCard'

interface TaskBoardProps {
  projectId: string
  workflow: Workflow
}

const TASK_EVENT_TYPES = [
  EventType.TASK_CREATED,
  EventType.TASK_UPDATED,
  EventType.TASK_STATUS_CHANGED,
  EventType.TASK_DELETED,
  EventType.AGENT_ASSIGNED,
  EventType.AGENT_STATUS_CHANGED,
]

export function TaskBoard({ projectId, workflow }: TaskBoardProps) {
  const { data, refetch } = useQuery(listTasks, {
    projectId,
    workflowId: workflow.id,
  })

  const onEvent = useCallback(() => {
    refetch()
  }, [refetch])

  useEventSubscription(TASK_EVENT_TYPES, projectId, onEvent)

  const tasks = data?.tasks ?? []
  const sortedStatuses = useMemo(
    () => [...workflow.statuses].sort((a, b) => a.order - b.order),
    [workflow.statuses],
  )

  // Build agent config lookup by status ID
  const agentConfigByStatusId = useMemo(() => {
    const map = new Map<string, string>()
    for (const cfg of workflow.agentConfigs) {
      map.set(cfg.workflowStatusId, cfg.name)
    }
    return map
  }, [workflow.agentConfigs])

  // Group tasks by status
  const tasksByStatus = useMemo(() => {
    const map = new Map<string, Task[]>()
    for (const s of sortedStatuses) {
      map.set(s.id, [])
    }
    for (const t of tasks) {
      const arr = map.get(t.statusId)
      if (arr) arr.push(t)
    }
    return map
  }, [tasks, sortedStatuses])

  return (
    <div className="flex-1 overflow-x-auto p-6">
      <div className="flex gap-4 h-full min-w-max">
        {sortedStatuses.map((status) => (
          <StatusColumn
            key={status.id}
            status={status}
            tasks={tasksByStatus.get(status.id) ?? []}
            agentConfigName={agentConfigByStatusId.get(status.id)}
          />
        ))}
      </div>
    </div>
  )
}

function StatusColumn({
  status,
  tasks,
  agentConfigName,
}: {
  status: WorkflowStatus
  tasks: Task[]
  agentConfigName?: string
}) {
  return (
    <div className="w-72 shrink-0 flex flex-col bg-slate-900/50 rounded-xl border border-slate-800">
      {/* Column header */}
      <div className="px-4 py-3 border-b border-slate-800">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <StatusDot status={status} />
            <h3 className="text-sm font-semibold text-white">{status.name}</h3>
            <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5">
              {tasks.length}
            </span>
          </div>
        </div>
        {agentConfigName && (
          <p className="text-xs text-cyan-400/70 mt-1">
            Agent: {agentConfigName}
          </p>
        )}
      </div>

      {/* Task list */}
      <div className="flex-1 overflow-y-auto p-2 space-y-2">
        {tasks.map((task) => (
          <TaskCard key={task.id} task={task} />
        ))}
        {tasks.length === 0 && (
          <p className="text-center text-gray-600 text-xs py-4">No tasks</p>
        )}
      </div>
    </div>
  )
}

function StatusDot({ status }: { status: WorkflowStatus }) {
  let color = 'bg-gray-500'
  if (status.isInitial) color = 'bg-blue-500'
  else if (status.isTerminal) color = 'bg-green-500'
  return <span className={`w-2 h-2 rounded-full ${color}`} />
}
