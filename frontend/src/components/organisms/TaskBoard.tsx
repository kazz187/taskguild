import { useCallback, useMemo, useRef, useState, type RefObject } from 'react'
import { createPortal } from 'react-dom'
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
import { TaskCard } from './TaskCard.tsx'
import { TaskDetailModal } from './TaskDetailModal.tsx'
import { TaskCreateModal } from './TaskCreateModal.tsx'
import { ChildTaskCreateModal } from './ChildTaskCreateModal.tsx'
import { ForceTransitionDialog } from './ForceTransitionDialog.tsx'
import { CleanTasksDialog } from './CleanTasksDialog.tsx'
import { ArchivedTaskList } from './ArchivedTaskList.tsx'
import { Plus, ChevronDown, ChevronRight, Sparkles } from 'lucide-react'
import { Badge } from '../atoms/index.ts'

interface TaskBoardProps {
  projectId: string
  workflow: Workflow
  /** Portal target in the page header for the Clean button */
  headerActionsRef?: RefObject<HTMLDivElement | null>
}

const TASK_EVENT_TYPES = [
  EventType.TASK_CREATED,
  EventType.TASK_UPDATED,
  EventType.TASK_STATUS_CHANGED,
  EventType.TASK_DELETED,
  EventType.AGENT_ASSIGNED,
  EventType.AGENT_STATUS_CHANGED,
  EventType.TASK_ARCHIVED,
  EventType.TASK_UNARCHIVED,
]

/** Pending force-move confirmation state */
interface ForceTransitionState {
  task: Task
  targetStatusId: string
  fromStatusName: string
  toStatusName: string
}

export function TaskBoard({ projectId, workflow, headerActionsRef }: TaskBoardProps) {
  const { data, refetch } = useQuery(listTasks, {
    projectId,
    workflowId: workflow.id,
  })
  const [editingTaskId, setEditingTaskId] = useState<string | null>(null)
  const [activeTask, setActiveTask] = useState<Task | null>(null)
  const [creatingChildForTask, setCreatingChildForTask] = useState<Task | null>(null)

  // Track whether the last drop was a successful status transition (for animation control)
  const lastDropWasSuccessful = useRef(false)
  // Optimistic moves: taskId -> targetStatusId (applied before API response)
  const [pendingMoves, setPendingMoves] = useState<Map<string, string>>(new Map())

  // Force-transition confirmation dialog state
  const [forceTransition, setForceTransition] = useState<ForceTransitionState | null>(null)

  // Clean (archive) dialog state
  const [showCleanDialog, setShowCleanDialog] = useState(false)

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

  // Compute parent-child relationships from task metadata
  const childTasksByParentId = useMemo(() => {
    const map = new Map<string, { id: string; title: string; statusId: string }[]>()
    for (const t of tasks) {
      const parentId = t.metadata?.['source_task_id']
      if (parentId) {
        if (!map.has(parentId)) map.set(parentId, [])
        map.get(parentId)!.push({ id: t.id, title: t.title, statusId: t.statusId })
      }
    }
    return map
  }, [tasks])

  // Map from task ID to parent task info
  const parentTaskById = useMemo(() => {
    const map = new Map<string, { id: string; title: string }>()
    for (const t of tasks) {
      const parentId = t.metadata?.['source_task_id']
      if (parentId) {
        const parent = tasks.find((p) => p.id === parentId)
        if (parent) {
          map.set(t.id, { id: parent.id, title: parent.title })
        }
      }
    }
    return map
  }, [tasks])

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

  // Terminal tasks eligible for archiving (for Clean button)
  const terminalTasks = useMemo(() => {
    return tasks.filter((t) => {
      const status = statusById.get(t.statusId)
      return status?.isTerminal === true
    })
  }, [tasks, statusById])

  // Allowed (normal) transition targets for the currently dragged task
  const normalTargetIds = useMemo(() => {
    if (!activeTask) return new Set<string>()
    const currentStatus = statusById.get(activeTask.statusId)
    return new Set(currentStatus?.transitionsTo ?? [])
  }, [activeTask, statusById])

  // Force-move targets: all statuses except the current one and normal targets
  const forceTargetIds = useMemo(() => {
    if (!activeTask) return new Set<string>()
    // Don't allow force targets if agent is actively running (assigned).
    // Pending tasks (agent not yet started) are allowed to be force-moved.
    if (activeTask.assignmentStatus === TaskAssignmentStatus.ASSIGNED) return new Set<string>()
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

      // Block force-move when agent is actively running (assigned).
      // Pending tasks (agent not yet started) are allowed to be force-moved.
      if (task.assignmentStatus === TaskAssignmentStatus.ASSIGNED) return

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

  /** Open child task creation modal from TaskCard or TaskDetailModal */
  const handleCreateChild = useCallback(
    (taskOrId: Task | string) => {
      if (typeof taskOrId === 'string') {
        // From TaskCard — find the task by ID
        const task = tasks.find((t) => t.id === taskOrId)
        if (task) setCreatingChildForTask(task)
      } else {
        // From TaskDetailModal — task object passed directly
        setCreatingChildForTask(taskOrId)
      }
    },
    [tasks],
  )

  /** Navigate to a related task (open its detail modal) */
  const handleNavigateTask = useCallback((taskId: string) => {
    setEditingTaskId(taskId)
  }, [])

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-col flex-1 overflow-hidden">
        {/* Portal Clean button into page header */}
        {headerActionsRef?.current && createPortal(
          <button
            onClick={() => setShowCleanDialog(true)}
            disabled={terminalTasks.length === 0}
            className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs md:text-sm md:px-3 text-gray-400 hover:text-amber-300 border border-slate-700 hover:border-amber-500/40 rounded-lg transition-colors disabled:opacity-50 disabled:hover:text-gray-400 disabled:hover:border-slate-700"
            title={terminalTasks.length > 0 ? `Archive ${terminalTasks.length} completed tasks` : 'No completed tasks to archive'}
          >
            <Sparkles className="w-4 h-4" />
            <span className="hidden sm:inline">Clean</span>
            {terminalTasks.length > 0 && (
              <Badge color="amber" size="xs" pill>
                {terminalTasks.length}
              </Badge>
            )}
          </button>,
          headerActionsRef.current,
        )}
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
                onCreateChild={handleCreateChild}
                onCreated={() => refetch()}
                isNormalTarget={normalTargetIds.has(status.id)}
                isForceTarget={forceTargetIds.has(status.id)}
                isDragging={activeTask !== null}
                transitionTargets={transitionTargetsByStatus.get(status.id) ?? []}
                onTransition={handleMobileTransition}
                defaultPermissionMode={workflow.defaultPermissionMode}
                defaultUseWorktree={workflow.defaultUseWorktree}
                childTasksByParentId={childTasksByParentId}
                parentTaskById={parentTaskById}
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
              onCreateChild={handleCreateChild}
              childTasks={childTasksByParentId.get(editingTaskId)}
              parentTask={parentTaskById.get(editingTaskId) ?? null}
              onNavigateTask={handleNavigateTask}
            />
          )}

          {/* Child task creation modal */}
          {creatingChildForTask && (
            <ChildTaskCreateModal
              parentTask={creatingChildForTask}
              projectId={projectId}
              workflowId={workflow.id}
              onCreated={() => refetch()}
              onClose={() => setCreatingChildForTask(null)}
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

          {/* Clean tasks dialog */}
          {showCleanDialog && (
            <CleanTasksDialog
              projectId={projectId}
              workflowId={workflow.id}
              terminalTasks={terminalTasks}
              statusById={statusById}
              onClose={() => setShowCleanDialog(false)}
              onArchived={() => refetch()}
            />
          )}
        </div>

        {/* Archived tasks section */}
        <ArchivedTaskList
          projectId={projectId}
          workflowId={workflow.id}
          statusById={statusById}
        />
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
  onCreateChild,
  onCreated,
  isNormalTarget,
  isForceTarget,
  isDragging,
  transitionTargets,
  onTransition,
  defaultPermissionMode,
  defaultUseWorktree,
  childTasksByParentId,
  parentTaskById,
}: {
  projectId: string
  workflowId: string
  status: WorkflowStatus
  tasks: Task[]
  agentConfigName?: string
  onEdit: (id: string) => void
  onCreateChild: (taskId: string) => void
  onCreated: () => void
  isNormalTarget: boolean
  isForceTarget: boolean
  isDragging: boolean
  transitionTargets: { id: string; name: string; isForce: boolean }[]
  onTransition: (taskId: string, targetStatusId: string, isForce: boolean) => void
  defaultPermissionMode?: string
  defaultUseWorktree?: boolean
  childTasksByParentId: Map<string, { id: string; title: string; statusId: string }[]>
  parentTaskById: Map<string, { id: string; title: string }>
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
            <Badge color="gray" size="xs" pill>
              {tasks.length}
            </Badge>
            {agentConfigName && (
              <Badge color="cyan" size="xs" variant="outline" pill className="truncate hidden sm:inline-flex">
                {agentConfigName}
              </Badge>
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
              onCreateChild={onCreateChild}
              transitionTargets={transitionTargets}
              onTransition={onTransition}
              childCount={childTasksByParentId.get(task.id)?.length}
              parentTaskTitle={parentTaskById.get(task.id)?.title}
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
