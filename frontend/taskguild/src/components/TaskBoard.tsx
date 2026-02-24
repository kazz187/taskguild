import { useCallback, useMemo, useRef, useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTasks, updateTaskStatus } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { listAgents } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
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
import { Plus, ChevronDown, ChevronRight } from 'lucide-react'

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

  // Track whether the last drop was a successful status transition (for animation control)
  const lastDropWasSuccessful = useRef(false)
  // Optimistic moves: taskId -> targetStatusId (applied before API response)
  const [pendingMoves, setPendingMoves] = useState<Map<string, string>>(new Map())

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

  // Fetch project agents to resolve agent names from status agent_id.
  const { data: agentsData } = useQuery(listAgents, { projectId })
  const projectAgents = agentsData?.agents ?? []

  const agentConfigByStatusId = useMemo(() => {
    const map = new Map<string, string>()
    // Build agent ID -> name map for quick lookup.
    const agentNameById = new Map<string, string>()
    for (const ag of projectAgents) {
      agentNameById.set(ag.id, ag.name)
    }
    // First, check status-level agent_id (new approach).
    for (const st of workflow.statuses) {
      if (st.agentId) {
        const agentName = agentNameById.get(st.agentId)
        if (agentName) {
          map.set(st.id, agentName)
        }
      }
    }
    // Fall back to legacy AgentConfig list for statuses without agent_id.
    for (const cfg of workflow.agentConfigs) {
      if (!map.has(cfg.workflowStatusId)) {
        map.set(cfg.workflowStatusId, cfg.name)
      }
    }
    return map
  }, [workflow.agentConfigs, workflow.statuses, projectAgents])

  const tasksByStatus = useMemo(() => {
    const map = new Map<string, Task[]>()
    for (const s of sortedStatuses) {
      map.set(s.id, [])
    }
    for (const t of tasks) {
      // Apply optimistic move if present, otherwise use actual statusId
      const effectiveStatusId = pendingMoves.get(t.id) ?? t.statusId
      const arr = map.get(effectiveStatusId)
      if (arr) arr.push(t)
    }
    return map
  }, [tasks, sortedStatuses, pendingMoves])

  const allowedStatusIds = useMemo(() => {
    if (!activeTask) return new Set<string>()
    const currentStatus = statusById.get(activeTask.statusId)
    return new Set(currentStatus?.transitionsTo ?? [])
  }, [activeTask, statusById])

  // Build transition targets for each status (for mobile transition buttons)
  const transitionTargetsByStatus = useMemo(() => {
    const map = new Map<string, { id: string; name: string }[]>()
    for (const s of sortedStatuses) {
      const targets = (s.transitionsTo ?? [])
        .map((toId) => {
          const toStatus = statusById.get(toId)
          return toStatus ? { id: toStatus.id, name: toStatus.name } : null
        })
        .filter((t): t is { id: string; name: string } => t !== null)
      map.set(s.id, targets)
    }
    return map
  }, [sortedStatuses, statusById])

  const handleDragStart = useCallback((event: DragStartEvent) => {
    lastDropWasSuccessful.current = false
    const task = event.active.data.current?.task as Task | undefined
    if (task) setActiveTask(task)
  }, [])

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event

      if (!over) {
        lastDropWasSuccessful.current = false
        setActiveTask(null)
        return
      }

      const task = active.data.current?.task as Task | undefined
      const targetStatusId = over.id as string
      if (!task || task.statusId === targetStatusId) {
        lastDropWasSuccessful.current = false
        setActiveTask(null)
        return
      }

      const currentStatus = statusById.get(task.statusId)
      if (!currentStatus?.transitionsTo.includes(targetStatusId)) {
        lastDropWasSuccessful.current = false
        setActiveTask(null)
        return
      }

      // Valid drop — mark as successful so DragOverlay skips the snap-back animation
      lastDropWasSuccessful.current = true

      // Optimistic update: immediately show the task in the target column
      setPendingMoves((prev) => new Map(prev).set(task.id, targetStatusId))

      statusMut.mutate(
        { id: task.id, statusId: targetStatusId },
        {
          onSettled: () => {
            // Clear the optimistic move and sync with server state
            setPendingMoves((prev) => {
              const next = new Map(prev)
              next.delete(task.id)
              return next
            })
            refetch()
          },
        },
      )

      setActiveTask(null)
    },
    [statusById, statusMut, refetch],
  )

  // Handle mobile transition button tap
  const handleMobileTransition = useCallback(
    (taskId: string, targetStatusId: string) => {
      // Optimistic update
      setPendingMoves((prev) => new Map(prev).set(taskId, targetStatusId))

      statusMut.mutate(
        { id: taskId, statusId: targetStatusId },
        {
          onSettled: () => {
            setPendingMoves((prev) => {
              const next = new Map(prev)
              next.delete(taskId)
              return next
            })
            refetch()
          },
        },
      )
    },
    [statusMut, refetch],
  )

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className="flex-1 overflow-x-auto p-4 md:p-6">
        {/* Desktop: horizontal flex; Mobile: vertical stack */}
        <div className="flex flex-col md:flex-row gap-4 md:h-full md:min-w-max">
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
              transitionTargets={transitionTargetsByStatus.get(status.id) ?? []}
              onTransition={handleMobileTransition}
              defaultPermissionMode={workflow.defaultPermissionMode}
              defaultUseWorktree={workflow.defaultUseWorktree}
            />
          ))}
        </div>

        <DragOverlay
          dropAnimation={lastDropWasSuccessful.current ? null : undefined}
        >
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
  transitionTargets,
  onTransition,
  defaultPermissionMode,
  defaultUseWorktree,
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
  transitionTargets: { id: string; name: string }[]
  onTransition: (taskId: string, targetStatusId: string) => void
  defaultPermissionMode?: string
  defaultUseWorktree?: boolean
}) {
  const { setNodeRef, isOver } = useDroppable({ id: status.id })
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [collapsed, setCollapsed] = useState(false)

  let borderClass = 'border-slate-800'
  if (isDragging && isDropTarget) {
    borderClass = isOver ? 'border-cyan-400' : 'border-cyan-500/50'
  }

  return (
    <div
      ref={setNodeRef}
      className={`md:w-72 md:shrink-0 flex flex-col bg-slate-900/50 rounded-xl border transition-colors ${borderClass}`}
    >
      {/* Column header */}
      <div className="px-4 py-3 border-b border-slate-800">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 min-w-0 flex-1">
            {/* Mobile collapse toggle */}
            <button
              onClick={() => setCollapsed(!collapsed)}
              className="md:hidden text-gray-500 hover:text-gray-300 transition-colors shrink-0"
            >
              {collapsed ? (
                <ChevronRight className="w-4 h-4" />
              ) : (
                <ChevronDown className="w-4 h-4" />
              )}
            </button>
            <StatusDot status={status} />
            <h3 className="text-sm font-semibold text-white truncate">{status.name}</h3>
            <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5 shrink-0">
              {tasks.length}
            </span>
            {agentConfigName && (
              <span className="text-[10px] text-cyan-400/70 bg-cyan-500/10 border border-cyan-500/20 rounded-full px-1.5 py-0.5 truncate shrink-0 hidden sm:inline-block">
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

      {/* Task list - collapsible on mobile */}
      {!collapsed && (
        <div className="flex-1 overflow-y-auto p-2 space-y-2 md:max-h-none max-h-[60vh]">
          {tasks.map((task) => (
            <TaskCard
              key={task.id}
              task={task}
              onEdit={onEdit}
              transitionTargets={transitionTargets}
              onTransition={onTransition}
            />
          ))}
          {tasks.length === 0 && (
            <p className="text-center text-gray-600 text-xs py-4">No tasks</p>
          )}
        </div>
      )}

      {showCreateModal && (
        <TaskCreateModal
          projectId={projectId}
          workflowId={workflowId}
          defaultPermissionMode={defaultPermissionMode}
          defaultUseWorktree={defaultUseWorktree}
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
