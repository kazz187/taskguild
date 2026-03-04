import { useState, useCallback, useEffect, useMemo, useRef } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getTask, updateTask, updateTaskStatus, deleteTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { requestWorktreeList, getWorktreeList } from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import { listInteractions, respondToInteraction } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { X, Bot, Clock, GitBranch, Loader, Trash2, ArrowRight, AlertTriangle, MessageSquare, Shield, Bell, RefreshCw } from 'lucide-react'
import { MarkdownDescription } from './MarkdownDescription'
import { ForceTransitionDialog } from './ForceTransitionDialog'
import { shortId } from '@/lib/id'
import { Button, Input, Textarea, Select, Checkbox, Badge } from '../atoms/index.ts'
import { Modal, FormField, Card } from '../molecules/index.ts'

const TASK_DETAIL_EVENT_TYPES = [
  EventType.TASK_UPDATED,
  EventType.TASK_STATUS_CHANGED,
  EventType.AGENT_ASSIGNED,
  EventType.AGENT_STATUS_CHANGED,
  EventType.INTERACTION_CREATED,
  EventType.INTERACTION_RESPONDED,
]

interface TaskDetailModalProps {
  taskId: string
  projectId: string
  statuses: WorkflowStatus[]
  currentStatusId: string
  onClose: () => void
  onChanged: () => void
  onDeleted?: () => void
}

export function TaskDetailModal({
  taskId,
  projectId,
  statuses,
  currentStatusId,
  onClose,
  onChanged,
  onDeleted,
}: TaskDetailModalProps) {
  const { data: taskData, refetch: refetchTask } = useQuery(getTask, { id: taskId })
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, { taskId })

  const updateMut = useMutation(updateTask)
  const statusMut = useMutation(updateTaskStatus)
  const deleteMut = useMutation(deleteTask)
  const respondMut = useMutation(respondToInteraction)
  const requestWtMut = useMutation(requestWorktreeList)

  const [titleDraft, setTitleDraft] = useState('')
  const [descDraft, setDescDraft] = useState('')
  const [permModeDraft, setPermModeDraft] = useState('')
  const [worktreeDraft, setWorktreeDraft] = useState(false)
  const [selectedWorktree, setSelectedWorktree] = useState('')

  const task = taskData?.task
  const interactions = interactionsData?.interactions ?? []

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
      setPermModeDraft(task.permissionMode ?? '')
      setWorktreeDraft(task.useWorktree ?? false)
      setSelectedWorktree(task.metadata?.['worktree'] ?? '')
    }
  }, [task?.id])

  const onEvent = useCallback(() => {
    refetchTask()
    refetchInteractions()
  }, [refetchTask, refetchInteractions])

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
    permModeDraft !== (task.permissionMode ?? '') ||
    worktreeDraft !== (task.useWorktree ?? false) ||
    selectedWorktree !== (task.metadata?.['worktree'] ?? '')
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
      { id: task.id, title: titleDraft.trim(), description: descDraft, metadata, permissionMode: permModeDraft, useWorktree: worktreeDraft },
      { onSuccess: () => { refetchTask(); onChanged() } },
    )
  }

  const handleCancel = () => {
    if (task) {
      setTitleDraft(task.title)
      setDescDraft(task.description)
      setPermModeDraft(task.permissionMode ?? '')
      setWorktreeDraft(task.useWorktree ?? false)
      setSelectedWorktree(task.metadata?.['worktree'] ?? '')
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

  // Synchronous guard to prevent duplicate responses (survives across renders before mutation state propagates)
  const respondedIdsRef = useRef<Set<string>>(new Set())

  const handleRespond = useCallback((interactionId: string, response: string) => {
    if (respondedIdsRef.current.has(interactionId)) return
    respondedIdsRef.current.add(interactionId)
    respondMut.mutate(
      { id: interactionId, response },
      {
        onSuccess: () => refetchInteractions(),
        onError: () => {
          // Allow retry on failure
          respondedIdsRef.current.delete(interactionId)
        },
      },
    )
  }, [respondMut, refetchInteractions])

  if (!task) {
    return (
      <Modal open={true} onClose={onClose} size="full">
        <div className="p-8 text-gray-400">Loading...</div>
      </Modal>
    )
  }

  return (
    <Modal open={true} onClose={onClose} size="full">
      {/* Header */}
      <div className="flex items-start justify-between px-4 pt-4 pb-1">
        <div className="flex-1 min-w-0 mr-3">
          <Input
            autoFocus
            value={titleDraft}
            onChange={(e) => setTitleDraft(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !e.nativeEvent.isComposing) handleSave() }}
            className="!border-slate-600 text-base md:text-lg font-semibold !rounded"
            placeholder="Task title..."
          />
        </div>
        <button onClick={onClose} className="text-gray-500 hover:text-gray-300 transition-colors shrink-0 mt-1 p-1">
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Body */}
      <Modal.Body>
        <Textarea
          value={descDraft}
          onChange={(e) => setDescDraft(e.target.value)}
          textareaSize="md"
          placeholder="Add description..."
        />

        {/* Agent settings */}
        <div className="flex flex-col sm:flex-row items-start sm:items-center gap-3">
          <FormField label="Permission Mode" className="flex-1 w-full sm:w-auto" labelSize="xs">
            <Select
              value={permModeDraft}
              onChange={(e) => setPermModeDraft(e.target.value)}
              selectSize="xs"
              className="!rounded"
            >
              <option value="">Default (ask for permission)</option>
              <option value="acceptEdits">Accept Edits (auto-approve file changes)</option>
              <option value="bypassPermissions">Bypass Permissions (auto-approve all)</option>
            </Select>
          </FormField>
          <Checkbox
            label="Use Worktree"
            checked={worktreeDraft}
            onChange={(e) => setWorktreeDraft(e.target.checked)}
            disabled={isTaskLocked}
            className="!text-xs sm:pt-4"
          />
        </div>

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
            <Badge color="yellow" variant="outline" pill icon={<Loader className="w-3 h-3" />}>
              Pending claim
            </Badge>
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

        {/* Interactions */}
        {interactions.length > 0 && (
          <div>
            <h4 className="text-xs text-gray-500 uppercase tracking-wide mb-2">Interactions</h4>
            <div className="space-y-2">
              {interactions.map((interaction) => (
                <InteractionItem
                  key={interaction.id}
                  interaction={interaction}
                  onRespond={handleRespond}
                  isPending={respondMut.isPending}
                />
              ))}
            </div>
          </div>
        )}

        {/* Action buttons */}
        <div className="border-t border-slate-800 mt-4 pt-3 flex justify-between items-center">
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
          <div className="flex items-center gap-2">
            <Button variant="secondary" size="sm" onClick={handleCancel}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={handleSave}
              disabled={!hasChanges || !titleDraft.trim() || updateMut.isPending}
            >
              {updateMut.isPending ? 'Saving...' : 'Save'}
            </Button>
          </div>
        </div>
      </Modal.Body>

      {/* Force-transition confirmation dialog */}
      {forceTransitionTarget && currentStatus && (
        <ForceTransitionDialog
          fromStatusName={currentStatus.name}
          toStatusName={forceTransitionTarget.name}
          onConfirm={handleForceConfirm}
          onCancel={handleForceCancel}
        />
      )}
    </Modal>
  )
}

export function InteractionItem({
  interaction,
  onRespond,
  isPending,
}: {
  interaction: { id: string; type: InteractionType; status: InteractionStatus; title: string; description: string; options: { label: string; value: string; description: string }[]; response: string }
  onRespond: (id: string, response: string) => void
  isPending: boolean
}) {
  const [freeText, setFreeText] = useState('')
  const isPendingStatus = interaction.status === InteractionStatus.PENDING

  const icon = interaction.type === InteractionType.PERMISSION_REQUEST
    ? <Shield className="w-4 h-4 text-amber-400" />
    : interaction.type === InteractionType.QUESTION
    ? <MessageSquare className="w-4 h-4 text-blue-400" />
    : <Bell className="w-4 h-4 text-gray-400" />

  return (
    <Card variant="nested" className={`!rounded-lg ${isPendingStatus ? '!border-amber-500/30' : ''}`}>
      <div className="flex items-start gap-2">
        <div className="mt-0.5">{icon}</div>
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium text-white">{interaction.title}</p>
          {interaction.description && (
            <MarkdownDescription content={interaction.description} className="mt-1" />
          )}

          {isPendingStatus ? (
            <div className="mt-2 space-y-2">
              {interaction.options.length > 0 ? (
                <div className="flex gap-2 flex-wrap">
                  {interaction.options.map((opt) => (
                    <Button
                      key={opt.value}
                      variant="ghost"
                      size="sm"
                      onClick={() => onRespond(interaction.id, opt.value)}
                      disabled={isPending}
                      className="!bg-slate-700 !border !border-slate-600 !text-gray-200 hover:!border-cyan-500/50 hover:!text-white"
                      title={opt.description}
                    >
                      {opt.label}
                    </Button>
                  ))}
                </div>
              ) : (
                <div className="flex gap-2">
                  <Input
                    value={freeText}
                    onChange={(e) => setFreeText(e.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter' && !e.nativeEvent.isComposing && freeText.trim()) onRespond(interaction.id, freeText) }}
                    inputSize="xs"
                    className="flex-1 !bg-slate-900 !rounded"
                    placeholder="Type your response..."
                  />
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => { if (freeText.trim()) onRespond(interaction.id, freeText) }}
                    disabled={isPending || !freeText.trim()}
                    className="!rounded"
                  >
                    Send
                  </Button>
                </div>
              )}
            </div>
          ) : (
            interaction.response && (
              <p className="text-xs text-green-400 mt-1">Response: {interaction.response}</p>
            )
          )}
        </div>
      </div>
    </Card>
  )
}
