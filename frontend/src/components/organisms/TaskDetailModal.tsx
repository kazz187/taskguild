import { useState, useCallback, useEffect, useMemo } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getTask, updateTask, updateTaskStatus, deleteTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { requestWorktreeList, getWorktreeList } from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import type { WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { Bot, Clock, GitBranch, Loader, Trash2, ArrowRight, AlertTriangle, RefreshCw, CopyPlus, ArrowUpRight, Layers } from 'lucide-react'
import { ForceTransitionDialog } from './ForceTransitionDialog'
import { shortId } from '@/lib/id'
import { Button, Select, Checkbox, Badge, Tooltip } from '../atoms/index.ts'
import { pendingReasonText } from '@/lib/pendingReason'
import { Modal, TaskFormModal, Card, ImageUploadTextarea, FormField } from '../molecules/index.ts'
import { EFFORT_OPTIONS } from '@/lib/constants.ts'

const TASK_DETAIL_EVENT_TYPES = [
  EventType.TASK_UPDATED,
  EventType.TASK_STATUS_CHANGED,
  EventType.AGENT_ASSIGNED,
  EventType.AGENT_STATUS_CHANGED,
]

interface ChildTaskInfo {
  id: string
  title: string
  statusId: string
}

interface TaskDetailModalProps {
  taskId: string
  projectId: string
  statuses: WorkflowStatus[]
  currentStatusId: string
  onClose: () => void
  onChanged: () => void
  onDeleted?: () => void
  /** Callback to open the child task creation modal */
  onCreateChild?: (task: Task) => void
  /** Child tasks of this task */
  childTasks?: ChildTaskInfo[]
  /** Parent task info (if this task is a child) */
  parentTask?: { id: string; title: string } | null
  /** Callback when clicking on a related task (parent or child) */
  onNavigateTask?: (taskId: string) => void
}

export function TaskDetailModal({
  taskId,
  projectId,
  statuses,
  currentStatusId,
  onClose,
  onChanged,
  onDeleted,
  onCreateChild,
  childTasks,
  parentTask,
  onNavigateTask,
}: TaskDetailModalProps) {
  const { data: taskData, refetch: refetchTask } = useQuery(getTask, { id: taskId })

  const updateMut = useMutation(updateTask)
  const statusMut = useMutation(updateTaskStatus)
  const deleteMut = useMutation(deleteTask)
  const requestWtMut = useMutation(requestWorktreeList)

  const [titleDraft, setTitleDraft] = useState('')
  const [descDraft, setDescDraft] = useState('')
  const [worktreeDraft, setWorktreeDraft] = useState(false)
  const [selectedWorktree, setSelectedWorktree] = useState('')
  const [effortDraft, setEffortDraft] = useState('')

  const task = taskData?.task

  const isTaskLocked = task?.assignmentStatus === TaskAssignmentStatus.ASSIGNED || task?.assignmentStatus === TaskAssignmentStatus.PENDING
  // Force-move is only blocked when agent is actively running (assigned).
  // Pending tasks (agent not yet started) are allowed to be force-moved.
  const isForceMoveBlocked = task?.assignmentStatus === TaskAssignmentStatus.ASSIGNED

  // Query cached worktree list (only when worktree is enabled and task is not locked)
  const { data: wtData, refetch: refetchWorktrees } = useQuery(getWorktreeList, { projectId }, {
    enabled: worktreeDraft && !isTaskLocked,
  })
  const worktrees = wtData?.worktrees ?? []

  useEffect(() => {
    if (task) {
      setTitleDraft(task.title)
      setDescDraft(task.description)
      setWorktreeDraft(task.useWorktree ?? false)
      setSelectedWorktree(task.metadata?.['worktree'] ?? '')
      setEffortDraft(task.effort ?? '')
    }
  }, [task?.id])

  const onEvent = useCallback(() => {
    refetchTask()
  }, [refetchTask])

  useEventSubscription(
    TASK_DETAIL_EVENT_TYPES,
    projectId,
    onEvent,
  )

  // Subscribe to worktree list events to refetch when a scan completes
  const worktreeEventTypes = useMemo(() => [EventType.WORKTREE_LIST], [])
  const onWorktreeEvent = useCallback(() => {
    refetchWorktrees()
  }, [refetchWorktrees])
  useEventSubscription(worktreeEventTypes, projectId, onWorktreeEvent)

  // Request worktree scan when worktree checkbox is toggled on
  useEffect(() => {
    if (worktreeDraft && projectId && !isTaskLocked) {
      requestWtMut.mutate({ projectId })
    }
  }, [worktreeDraft, projectId, isTaskLocked])

  const currentStatus = statuses.find((s) => s.id === (task?.statusId ?? currentStatusId))
  const allowedTransitions = currentStatus?.transitionsTo ?? []

  // Force transition targets: all statuses except current and normal transitions
  const forceTransitions = useMemo(() => {
    if (!currentStatus) return []
    const normalSet = new Set(allowedTransitions)
    return statuses
      .filter((s) => s.id !== currentStatus.id && !normalSet.has(s.id))
      .map((s) => ({ id: s.id, name: s.name }))
  }, [currentStatus, allowedTransitions, statuses])

  // Force-transition confirmation dialog state
  const [forceTransitionTarget, setForceTransitionTarget] = useState<{ id: string; name: string } | null>(null)

  const hasChanges = task ? (
    titleDraft !== task.title ||
    descDraft !== task.description ||
    worktreeDraft !== (task.useWorktree ?? false) ||
    selectedWorktree !== (task.metadata?.['worktree'] ?? '') ||
    effortDraft !== (task.effort ?? '')
  ) : false

  const handleSave = () => {
    if (!task || !titleDraft.trim() || !hasChanges) return
    const metadata: Record<string, string> = { ...task.metadata }
    if (worktreeDraft) {
      metadata['worktree'] = selectedWorktree
    } else {
      metadata['worktree'] = ''
    }
    updateMut.mutate(
      { id: task.id, title: titleDraft.trim(), description: descDraft, metadata, useWorktree: worktreeDraft, effort: effortDraft },
      { onSuccess: () => { refetchTask(); onChanged() } },
    )
  }

  const handleCancel = () => {
    if (task) {
      setTitleDraft(task.title)
      setDescDraft(task.description)
      setWorktreeDraft(task.useWorktree ?? false)
      setSelectedWorktree(task.metadata?.['worktree'] ?? '')
      setEffortDraft(task.effort ?? '')
    }
    onClose()
  }

  const handleStatusChange = (statusId: string, force = false) => {
    if (!task) return
    statusMut.mutate(
      { id: task.id, statusId, force },
      { onSuccess: () => { refetchTask(); onChanged() } },
    )
  }

  const handleForceTransitionClick = (target: { id: string; name: string }) => {
    setForceTransitionTarget(target)
  }

  const handleForceConfirm = () => {
    if (!forceTransitionTarget) return
    handleStatusChange(forceTransitionTarget.id, true)
    setForceTransitionTarget(null)
  }

  const handleForceCancel = () => {
    setForceTransitionTarget(null)
  }

  const handleDelete = () => {
    if (!task) return
    deleteMut.mutate(
      { id: task.id },
      { onSuccess: () => { if (onDeleted) { onDeleted() } else { onChanged(); onClose() } } },
    )
  }

  if (!task) {
    return (
      <Modal open={true} onClose={onClose} size="full">
        <div className="p-8 text-gray-400">Loading...</div>
      </Modal>
    )
  }

  return (
    <>
      <TaskFormModal
        headerLabel="Edit Task"
        title={titleDraft}
        onTitleChange={setTitleDraft}
        onClose={handleCancel}
        onSubmit={handleSave}
        submitLabel="Save"
        submitPendingLabel="Saving..."
        isSubmitting={updateMut.isPending}
        submitDisabled={!hasChanges || !titleDraft.trim()}
        footerLeadingActions={
          <>
            <Button
              variant="ghost"
              size="sm"
              icon={<Trash2 className="w-3.5 h-3.5" />}
              onClick={handleDelete}
              disabled={deleteMut.isPending}
              className="!text-gray-500 hover:!text-red-400"
            >
              Delete
            </Button>
            {onCreateChild && task && (
              <Button
                variant="ghost"
                size="sm"
                icon={<CopyPlus className="w-3.5 h-3.5" />}
                onClick={() => onCreateChild(task)}
                className="!text-gray-500 hover:!text-cyan-400"
              >
                Subtask
              </Button>
            )}
          </>
        }
      >
        {/* Parent task link */}
        {parentTask && (
          <Card variant="nested" className="!rounded-lg !py-2 !px-3">
            <button
              onClick={() => onNavigateTask?.(parentTask.id)}
              className="flex items-center gap-2 text-xs text-gray-400 hover:text-white transition-colors w-full text-left"
            >
              <ArrowUpRight className="w-3.5 h-3.5 text-gray-500 shrink-0" />
              <span className="text-gray-500">Parent:</span>
              <span className="font-medium truncate">{parentTask.title}</span>
              <span className="text-gray-600 font-mono shrink-0">{shortId(parentTask.id)}</span>
            </button>
          </Card>
        )}

        <ImageUploadTextarea
          value={descDraft}
          onChange={setDescDraft}
          taskId={task?.id}
          textareaSize="md"
          placeholder="Add description..."
          disabled={isTaskLocked}
        />

        {/* Agent settings */}
        <FormField label="Effort" labelSize="xs">
          <Select
            value={effortDraft}
            onChange={(e) => setEffortDraft(e.target.value)}
            selectSize="xs"
            className="rounded"
            disabled={isTaskLocked}
          >
            {EFFORT_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </Select>
        </FormField>

        <Checkbox
          label="Use Worktree (isolate changes in a git worktree)"
          checked={worktreeDraft}
          onChange={(e) => setWorktreeDraft(e.target.checked)}
          disabled={isTaskLocked}
        />

        {/* Worktree selection / display */}
        {isTaskLocked && task.metadata?.['worktree'] ? (
          // Read-only display when task is assigned/pending
          <div className="flex items-center gap-1.5 text-xs text-gray-400 bg-slate-800 border border-slate-700 rounded px-2.5 py-1.5">
            <GitBranch className="w-3 h-3 text-gray-500 shrink-0" />
            <span className="font-mono truncate">{task.metadata['worktree']}</span>
          </div>
        ) : !isTaskLocked && worktreeDraft ? (
          // Editable dropdown when worktree is enabled and task is not locked
          <div className="pl-6">
            <div className="flex items-center gap-2 mb-1">
              <GitBranch className="w-3.5 h-3.5 text-gray-500" />
              <label className="text-xs text-gray-400">Worktree</label>
              <button
                type="button"
                onClick={() => requestWtMut.mutate({ projectId })}
                className="text-gray-500 hover:text-gray-300 transition-colors"
                title="Refresh worktree list"
              >
                <RefreshCw className={`w-3 h-3 ${requestWtMut.isPending ? 'animate-spin' : ''}`} />
              </button>
            </div>
            <Select
              value={selectedWorktree}
              onChange={(e) => setSelectedWorktree(e.target.value)}
            >
              <option value="">New worktree (auto-generated)</option>
              {worktrees.map((wt) => (
                <option key={wt.name} value={wt.name}>
                  {wt.name} ({wt.branch})
                </option>
              ))}
            </Select>
          </div>
        ) : null}

        {/* Status + Agent + Transitions row */}
        <div className="flex items-center gap-2 flex-wrap">
          <Badge
            color={
              currentStatus?.isInitial ? 'blue' :
              currentStatus?.isTerminal ? 'green' :
              'gray'
            }
            pill
          >
            {currentStatus?.name ?? task.statusId}
          </Badge>
          {task.assignmentStatus === TaskAssignmentStatus.ASSIGNED ? (
            <Badge color="cyan" variant="outline" pill icon={<Bot className="w-3 h-3" />}>
              {shortId(task.assignedAgentId)}
            </Badge>
          ) : task.assignmentStatus === TaskAssignmentStatus.PENDING ? (
            <Tooltip content={pendingReasonText(task.metadata)}>
              <Badge color="yellow" variant="outline" pill icon={<Loader className="w-3 h-3 animate-spin" />}>
                Pending
              </Badge>
            </Tooltip>
          ) : (
            <span className="inline-flex items-center gap-1 text-xs text-gray-500">
              <Clock className="w-3 h-3" />
              Unassigned
            </span>
          )}
          {allowedTransitions.map((toId) => {
            const toStatus = statuses.find((s) => s.id === toId)
            return (
              <button
                key={toId}
                onClick={() => handleStatusChange(toId)}
                disabled={statusMut.isPending}
                className="flex items-center gap-1 px-3 py-1 text-xs bg-slate-800 border border-slate-700 rounded-lg text-gray-300 hover:border-cyan-500/50 hover:text-white transition-colors disabled:opacity-50"
              >
                <ArrowRight className="w-3 h-3" />
                {toStatus?.name ?? toId}
              </button>
            )
          })}
          {!isForceMoveBlocked && forceTransitions.map((target) => (
            <button
              key={target.id}
              onClick={() => handleForceTransitionClick(target)}
              disabled={statusMut.isPending}
              className="flex items-center gap-1 px-3 py-1 text-xs bg-slate-800 border border-slate-700 rounded-lg text-gray-400 hover:border-amber-500/50 hover:text-amber-300 transition-colors disabled:opacity-50"
              title="Force move (not defined in workflow)"
            >
              <AlertTriangle className="w-3 h-3 text-amber-500/70" />
              {target.name}
            </button>
          ))}
          <span className="text-[11px] text-gray-600 font-mono ml-auto hidden sm:inline">{task.id}</span>
        </div>

        {/* Child tasks */}
        {childTasks && childTasks.length > 0 && (
          <div>
            <h4 className="text-xs text-gray-500 uppercase tracking-wide mb-2 flex items-center gap-1.5">
              <Layers className="w-3 h-3" />
              Subtasks ({childTasks.length})
            </h4>
            <div className="space-y-1">
              {childTasks.map((child) => {
                const childStatus = statuses.find((s) => s.id === child.statusId)
                return (
                  <button
                    key={child.id}
                    onClick={() => onNavigateTask?.(child.id)}
                    className="flex items-center gap-2 w-full text-left px-2.5 py-1.5 text-xs bg-slate-800/50 border border-slate-700/50 rounded-lg hover:border-slate-600 hover:bg-slate-800 transition-colors"
                  >
                    <span className="text-white font-medium truncate flex-1">{child.title}</span>
                    {childStatus && (
                      <Badge
                        color={childStatus.isInitial ? 'blue' : childStatus.isTerminal ? 'green' : 'gray'}
                        size="xs"
                        pill
                      >
                        {childStatus.name}
                      </Badge>
                    )}
                    <span className="text-[10px] text-gray-600 font-mono shrink-0">{shortId(child.id)}</span>
                  </button>
                )
              })}
            </div>
          </div>
        )}
      </TaskFormModal>

      {/* Force-transition confirmation dialog */}
      {forceTransitionTarget && currentStatus && (
        <ForceTransitionDialog
          fromStatusName={currentStatus.name}
          toStatusName={forceTransitionTarget.name}
          onConfirm={handleForceConfirm}
          onCancel={handleForceCancel}
        />
      )}
    </>
  )
}
