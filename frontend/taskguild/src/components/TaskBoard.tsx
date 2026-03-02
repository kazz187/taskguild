import { useCallback, useMemo, useRef, useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTasks, updateTaskStatus } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { listAgents } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
import type { Workflow, WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
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
import { ForceTransitionDialog } from './ForceTransitionDialog'
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

/** Pending force-move confirmation state */
interface ForceTransitionState {
  task: Task
  targetStatusId: string
  fromStatusName: string
  toStatusName: string
}

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

  // Force-transition confirmation dialog state
  const [forceTransition, setForceTransition] = useState<ForceTransitionState | null>(null)

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
    // Sort tasks within each status: newest first (reverse chronological by created_at)
    for (const arr of map.values()) {
      arr.sort((a, b) => {
        const aSeconds = Number(a.createdAt?.seconds ?? 0n)
        const bSeconds = Number(b.createdAt?.seconds ?? 0n)
        if (bSeconds !== aSeconds) return bSeconds - aSeconds
        return (b.createdAt?.nanos ?? 0) - (a.createdAt?.nanos ?? 0)
      })
    }
    return map
  }, [tasks, sortedStatuses, pendingMoves])

  // Allowed (normal) transition targets for the currently dragged task
  const normalTargetIds = useMemo(() => {
    if (!activeTask) return new Set<string>()
    const currentStatus = statusById.get(activeTask.statusId)
    return new Set(currentStatus?.transitionsTo ?? [])
  }, [activeTask, statusById])

  // Force-move targets: all statuses except the current one and normal targets
  const forceTargetIds = useMemo(() => {
    if (!activeTask) return new Set<string>()
    const isAgentRunning =
      activeTask.assignmentStatus === TaskAssignmentStatus.ASSIGNED ||
      activeTask.assignmentStatus === TaskAssignmentStatus.PENDING
    // Don't allow force targets if agent is running
    if (isAgentRunning) return new Set<string>()
    const set = new Set<string>()
    for (const s of sortedStatuses) {
      if (s.id !== activeTask.statusId && !normalTargetIds.has(s.id)) {
        set.add(s.id)
      }
    }
    return set
  }, [activeTask, sortedStatuses, normalTargetIds])

  // Build transition targets for each status (for mobile transition buttons)
  // Includes both normal and force targets
  const transitionTargetsByStatus = useMemo(() => {
    const map = new Map<string, { id: string; name: string; isForce: boolean }[]>()
    for (const s of sortedStatuses) {
      const normalIds = new Set(s.transitionsTo ?? [])
      const targets: { id: string; name: string; isForce: boolean }[] = []
      // Normal transitions first
      for (const toId of normalIds) {
        const toStatus = statusById.get(toId)
        if (toStatus) {
          targets.push({ id: toStatus.id, name: toStatus.name, isForce: false })
        }
      }
      // Force transitions (all other statuses)
      for (const other of sortedStatuses) {
        if (other.id !== s.id && !normalIds.has(other.id)) {
          targets.push({ id: other.id, name: other.name, isForce: true })
        }
      }
      map.set(s.id, targets)
    }
    return map
  }, [sortedStatuses, statusById])

  const handleDragStart = useCallback((event: DragStartEvent) => {
    lastDropWasSuccessful.current = false
    const task = event.active.data.current?.task as Task | undefined
    if (task) setActiveTask(task)
  }, [])

  /** Execute a status transition (normal or force) */
  const executeTransition = useCallback(
    (taskId: string, targetStatusId: string, force: boolean) => {
      // Optimistic update: immediately show the task in the target column
      setPendingMoves((prev) => new Map(prev).set(taskId, targetStatusId))

      statusMut.mutate(
        { id: taskId, statusId: targetStatusId, force },
        {
          onSettled: () => {
            // Clear the optimistic move and sync with server state
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
      const isNormalTransition = currentStatus?.transitionsTo.includes(targetStatusId) ?? false

      if (isNormalTransition) {
        // Valid normal drop — proceed immediately
        lastDropWasSuccessful.current = true
        executeTransition(task.id, targetStatusId, false)
        setActiveTask(null)
        return
      }

      // Check if this is a valid force-move target
      if (forceTargetIds.has(targetStatusId)) {
        // Force drop — show confirmation dialog
        lastDropWasSuccessful.current = true
        const fromName = currentStatus?.name ?? task.statusId
        const toName = statusById.get(targetStatusId)?.name ?? targetStatusId
        setForceTransition({
          task,
          targetStatusId,
          fromStatusName: fromName,
          toStatusName: toName,
        })
        setActiveTask(null)
        return
      }

      // Invalid drop (e.g., agent running blocks force targets)
      lastDropWasSuccessful.current = false
      setActiveTask(null)
    },
    [statusById, forceTargetIds, executeTransition],
  )

  /** Confirm force transition from dialog */
  const handleForceConfirm = useCallback(() => {
    if (!forceTransition) return
    executeTransition(forceTransition.task.id, forceTransition.targetStatusId, true)
    setForceTransition(null)
  }, [forceTransition, executeTransition])

  /** Cancel force transition dialog */
  const handleForceCancel = useCallback(() => {
    setForceTransition(null)
  }, [])

  // Handle mobile transition button tap
  const handleMobileTransition = useCallback(
    (taskId: string, targetStatusId: string, isForce: boolean) => {
      if (!isForce) {
        // Normal transition — execute immediately
        executeTransition(taskId, targetStatusId, false)
        return
      }

      // Force transition — find task and show confirmation dialog
      const task = tasks.find((t) => t.id === taskId)
      if (!task) return

      // Block force-move when agent is running
      const isAgentRunning =
        task.assignmentStatus === TaskAssignmentStatus.ASSIGNED ||
        task.assignmentStatus === TaskAssignmentStatus.PENDING
      if (isAgentRunning) return

      const fromName = statusById.get(task.statusId)?.name ?? task.statusId
      const toName = statusById.get(targetStatusId)?.name ?? targetStatusId
      setForceTransition({
        task,
        targetStatusId,
        fromStatusName: fromName,
        toStatusName: toName,
      })
    },
    [executeTransition, tasks, statusById],
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
              isNormalTarget={normalTargetIds.has(status.id)}
              isForceTarget={forceTargetIds.has(status.id)}
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

        {/* Force-transition confirmation dialog */}
        {forceTransition && (
          <ForceTransitionDialog
            fromStatusName={forceTransition.fromStatusName}
            toStatusName={forceTransition.toStatusName}
            onConfirm={handleForceConfirm}
            onCancel={handleForceCancel}
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
  isNormalTarget,
  isForceTarget,
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
  isNormalTarget: boolean
  isForceTarget: boolean
  isDragging: boolean
  transitionTargets: { id: string; name: string; isForce: boolean }[]
  onTransition: (taskId: string, targetStatusId: string, isForce: boolean) => void
  defaultPermissionMode?: string
  defaultUseWorktree?: boolean
}) {
  const { setNodeRef, isOver } = useDroppable({ id: status.id })
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [collapsed, setCollapsed] = useState(false)

  const isDropTarget = isNormalTarget || isForceTarget
  let borderClass = 'border-slate-800'
  if (isDragging && isDropTarget) {
    if (isNormalTarget) {
      borderClass = isOver ? 'border-cyan-400' : 'border-cyan-500/50'
    } else if (isForceTarget) {
      borderClass = isOver ? 'border-amber-400' : 'border-amber-500/30'
    }
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
