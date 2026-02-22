import { useCallback, useMemo, useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTasks, updateTaskStatus } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import type { Workflow, WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import {
  DndContext,
  DragOverlay,
  useSensor,
  useSensors,
  PointerSensor,
  useDroppable,
  type DragStartEvent,
  type DragEndEvent,
} from '@dnd-kit/core'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { TaskCard } from './TaskCard'
import { TaskDetailModal } from './TaskDetailModal'
import { TaskCreateModal } from './TaskCreateModal'
import { Plus } from 'lucide-react'

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
  const [editingTaskId, setEditingTaskId] = useState<string | null>(null)
  const [activeTask, setActiveTask] = useState<Task | null>(null)

  const statusMut = useMutation(updateTaskStatus)

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
  )

  const onEvent = useCallback(() => {
    refetch()
  }, [refetch])

  useEventSubscription(TASK_EVENT_TYPES, projectId, onEvent)

  const tasks = data?.tasks ?? []
  const sortedStatuses = useMemo(
    () => [...workflow.statuses].sort((a, b) => a.order - b.order),
    [workflow.statuses],
  )

  const statusById = useMemo(() => {
    const map = new Map<string, WorkflowStatus>()
    for (const s of sortedStatuses) {
      map.set(s.id, s)
    }
    return map
  }, [sortedStatuses])

  const agentConfigByStatusId = useMemo(() => {
    const map = new Map<string, string>()
    for (const cfg of workflow.agentConfigs) {
      map.set(cfg.workflowStatusId, cfg.name)
    }
    return map
  }, [workflow.agentConfigs])

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

  const allowedStatusIds = useMemo(() => {
    if (!activeTask) return new Set<string>()
    const currentStatus = statusById.get(activeTask.statusId)
    return new Set(currentStatus?.transitionsTo ?? [])
  }, [activeTask, statusById])

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const task = event.active.data.current?.task as Task | undefined
    if (task) setActiveTask(task)
  }, [])

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event
      setActiveTask(null)

      if (!over) return

      const task = active.data.current?.task as Task | undefined
      const targetStatusId = over.id as string
      if (!task || task.statusId === targetStatusId) return

      const currentStatus = statusById.get(task.statusId)
      if (!currentStatus?.transitionsTo.includes(targetStatusId)) return

      statusMut.mutate(
        { id: task.id, statusId: targetStatusId },
        { onSuccess: () => refetch() },
      )
    },
    [statusById, statusMut, refetch],
  )

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className="flex-1 overflow-x-auto p-6">
        <div className="flex gap-4 h-full min-w-max">
          {sortedStatuses.map((status) => (
            <StatusColumn
              key={status.id}
              projectId={projectId}
              workflowId={workflow.id}
              status={status}
              tasks={tasksByStatus.get(status.id) ?? []}
              agentConfigName={agentConfigByStatusId.get(status.id)}
              onEdit={setEditingTaskId}
              onCreated={() => refetch()}
              isDropTarget={allowedStatusIds.has(status.id)}
              isDragging={activeTask !== null}
            />
          ))}
        </div>

        <DragOverlay>
          {activeTask ? (
            <div className="w-72">
              <TaskCard task={activeTask} isDragOverlay />
            </div>
          ) : null}
        </DragOverlay>

        {editingTaskId && (
          <TaskDetailModal
            taskId={editingTaskId}
            projectId={projectId}
            statuses={sortedStatuses}
            currentStatusId={tasks.find((t) => t.id === editingTaskId)?.statusId ?? ''}
            onClose={() => setEditingTaskId(null)}
            onChanged={() => refetch()}
          />
        )}
      </div>
    </DndContext>
  )
}

function StatusColumn({
  projectId,
  workflowId,
  status,
  tasks,
  agentConfigName,
  onEdit,
  onCreated,
  isDropTarget,
  isDragging,
}: {
  projectId: string
  workflowId: string
  status: WorkflowStatus
  tasks: Task[]
  agentConfigName?: string
  onEdit: (id: string) => void
  onCreated: () => void
  isDropTarget: boolean
  isDragging: boolean
}) {
  const { setNodeRef, isOver } = useDroppable({ id: status.id })
  const [showCreateModal, setShowCreateModal] = useState(false)

  let borderClass = 'border-slate-800'
  if (isDragging && isDropTarget) {
    borderClass = isOver ? 'border-cyan-400' : 'border-cyan-500/50'
  }

  return (
    <div
      ref={setNodeRef}
      className={`w-72 shrink-0 flex flex-col bg-slate-900/50 rounded-xl border transition-colors ${borderClass}`}
    >
      {/* Column header */}
      <div className="px-4 py-3 border-b border-slate-800">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 min-w-0">
            <StatusDot status={status} />
            <h3 className="text-sm font-semibold text-white truncate">{status.name}</h3>
            <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5 shrink-0">
              {tasks.length}
            </span>
            {agentConfigName && (
              <span className="text-[10px] text-cyan-400/70 bg-cyan-500/10 border border-cyan-500/20 rounded-full px-1.5 py-0.5 truncate shrink-0">
                {agentConfigName}
              </span>
            )}
          </div>
          {status.isInitial && (
            <button
              onClick={() => setShowCreateModal(true)}
              className="text-gray-500 hover:text-cyan-400 transition-colors shrink-0"
              title="Add task"
            >
              <Plus className="w-4 h-4" />
            </button>
          )}
        </div>
      </div>

      {/* Task list */}
      <div className="flex-1 overflow-y-auto p-2 space-y-2">
        {tasks.map((task) => (
          <TaskCard key={task.id} task={task} onEdit={onEdit} />
        ))}
        {tasks.length === 0 && (
          <p className="text-center text-gray-600 text-xs py-4">No tasks</p>
        )}
      </div>

      {showCreateModal && (
        <TaskCreateModal
          projectId={projectId}
          workflowId={workflowId}
          onCreated={onCreated}
          onClose={() => setShowCreateModal(false)}
        />
      )}
    </div>
  )
}

function StatusDot({ status }: { status: WorkflowStatus }) {
  let color = 'bg-gray-500'
  if (status.isInitial) color = 'bg-blue-500'
  else if (status.isTerminal) color = 'bg-green-500'
  return <span className={`w-2 h-2 rounded-full ${color}`} />
}
