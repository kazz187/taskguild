import { useState, useCallback, useEffect } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getTask, updateTask, updateTaskStatus, deleteTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { listInteractions, respondToInteraction } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { X, Bot, Clock, Loader, Trash2, ArrowRight, MessageSquare, Shield, Bell } from 'lucide-react'
import { MarkdownDescription } from './MarkdownDescription'

interface TaskDetailModalProps {
  taskId: string
  projectId: string
  statuses: WorkflowStatus[]
  currentStatusId: string
  onClose: () => void
  onChanged: () => void
}

export function TaskDetailModal({
  taskId,
  projectId,
  statuses,
  currentStatusId,
  onClose,
  onChanged,
}: TaskDetailModalProps) {
  const { data: taskData, refetch: refetchTask } = useQuery(getTask, { id: taskId })
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, { taskId })

  const updateMut = useMutation(updateTask)
  const statusMut = useMutation(updateTaskStatus)
  const deleteMut = useMutation(deleteTask)
  const respondMut = useMutation(respondToInteraction)

  const [titleDraft, setTitleDraft] = useState('')
  const [descDraft, setDescDraft] = useState('')
  const [permModeDraft, setPermModeDraft] = useState('')
  const [worktreeDraft, setWorktreeDraft] = useState(false)

  const task = taskData?.task
  const interactions = interactionsData?.interactions ?? []

  useEffect(() => {
    if (task) {
      setTitleDraft(task.title)
      setDescDraft(task.description)
      setPermModeDraft(task.permissionMode ?? '')
      setWorktreeDraft(task.useWorktree ?? false)
    }
  }, [task?.id])

  const onEvent = useCallback(() => {
    refetchTask()
    refetchInteractions()
  }, [refetchTask, refetchInteractions])

  useEventSubscription(
    [EventType.TASK_UPDATED, EventType.TASK_STATUS_CHANGED, EventType.AGENT_ASSIGNED, EventType.AGENT_STATUS_CHANGED, EventType.INTERACTION_CREATED, EventType.INTERACTION_RESPONDED],
    projectId,
    onEvent,
  )

  const currentStatus = statuses.find((s) => s.id === (task?.statusId ?? currentStatusId))
  const allowedTransitions = currentStatus?.transitionsTo ?? []

  const hasChanges = task ? (
    titleDraft !== task.title ||
    descDraft !== task.description ||
    permModeDraft !== (task.permissionMode ?? '') ||
    worktreeDraft !== (task.useWorktree ?? false)
  ) : false

  const handleSave = () => {
    if (!task || !titleDraft.trim() || !hasChanges) return
    updateMut.mutate(
      { id: task.id, title: titleDraft.trim(), description: descDraft, metadata: task.metadata, permissionMode: permModeDraft, useWorktree: worktreeDraft },
      { onSuccess: () => { refetchTask(); onChanged() } },
    )
  }

  const handleCancel = () => {
    if (task) {
      setTitleDraft(task.title)
      setDescDraft(task.description)
      setPermModeDraft(task.permissionMode ?? '')
      setWorktreeDraft(task.useWorktree ?? false)
    }
    onClose()
  }

  const handleStatusChange = (statusId: string) => {
    if (!task) return
    statusMut.mutate(
      { id: task.id, statusId },
      { onSuccess: () => { refetchTask(); onChanged() } },
    )
  }

  const handleDelete = () => {
    if (!task) return
    deleteMut.mutate(
      { id: task.id },
      { onSuccess: () => { onChanged(); onClose() } },
    )
  }

  const handleRespond = (interactionId: string, response: string) => {
    respondMut.mutate(
      { id: interactionId, response },
      { onSuccess: () => refetchInteractions() },
    )
  }

  if (!task) {
    return (
      <ModalBackdrop onClose={onClose}>
        <div className="p-8 text-gray-400">Loading...</div>
      </ModalBackdrop>
    )
  }

  return (
    <ModalBackdrop onClose={onClose}>
      <div className="bg-slate-900 border border-slate-700 rounded-xl w-full max-w-2xl max-h-[85vh] flex flex-col shadow-2xl">
        {/* Header */}
        <div className="flex items-start justify-between px-4 pt-4 pb-1">
          <div className="flex-1 min-w-0 mr-3">
            <input
              autoFocus
              value={titleDraft}
              onChange={(e) => setTitleDraft(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && !e.nativeEvent.isComposing) handleSave() }}
              className="w-full px-2 py-1 bg-slate-800 border border-slate-600 rounded text-white text-lg font-semibold focus:outline-none focus:border-cyan-500"
              placeholder="Task title..."
            />
          </div>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-300 transition-colors shrink-0 mt-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
          <textarea
            value={descDraft}
            onChange={(e) => setDescDraft(e.target.value)}
            className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500 min-h-[200px]"
            placeholder="Add description..."
          />

          {/* Agent settings */}
          <div className="flex items-center gap-3">
            <div className="flex-1">
              <label className="block text-xs text-gray-500 mb-1">Permission Mode</label>
              <select
                value={permModeDraft}
                onChange={(e) => setPermModeDraft(e.target.value)}
                className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
              >
                <option value="">Default (ask for permission)</option>
                <option value="acceptEdits">Accept Edits (auto-approve file changes)</option>
                <option value="bypassPermissions">Bypass Permissions (auto-approve all)</option>
              </select>
            </div>
            <label className="flex items-center gap-1.5 text-xs text-gray-400 cursor-pointer pt-4">
              <input
                type="checkbox"
                checked={worktreeDraft}
                onChange={(e) => setWorktreeDraft(e.target.checked)}
                className="accent-cyan-500"
              />
              Use Worktree
            </label>
          </div>

          {/* Status + Agent + Transitions row */}
          <div className="flex items-center gap-2 flex-wrap">
            <span className={`text-xs px-2.5 py-1 rounded-full font-medium ${
              currentStatus?.isInitial ? 'bg-blue-500/20 text-blue-400' :
              currentStatus?.isTerminal ? 'bg-green-500/20 text-green-400' :
              'bg-gray-500/20 text-gray-300'
            }`}>
              {currentStatus?.name ?? task.statusId}
            </span>
            {task.assignmentStatus === TaskAssignmentStatus.ASSIGNED ? (
              <span className="inline-flex items-center gap-1 text-xs bg-cyan-500/10 text-cyan-400 border border-cyan-500/20 rounded-full px-2.5 py-1">
                <Bot className="w-3 h-3" />
                {task.assignedAgentId.slice(0, 12)}
              </span>
            ) : task.assignmentStatus === TaskAssignmentStatus.PENDING ? (
              <span className="inline-flex items-center gap-1 text-xs bg-yellow-500/10 text-yellow-400 border border-yellow-500/20 rounded-full px-2.5 py-1">
                <Loader className="w-3 h-3" />
                Pending claim
              </span>
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
            <span className="text-[11px] text-gray-600 font-mono ml-auto">{task.id}</span>
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
        </div>

        {/* Footer */}
        <div className="border-t border-slate-800 px-4 py-2 flex justify-between items-center">
          <button
            onClick={handleDelete}
            disabled={deleteMut.isPending}
            className="flex items-center gap-1 text-xs text-gray-500 hover:text-red-400 transition-colors disabled:opacity-50"
          >
            <Trash2 className="w-3.5 h-3.5" />
            Delete
          </button>
          <div className="flex items-center gap-2">
            <button
              onClick={handleCancel}
              className="px-3 py-1.5 text-xs text-gray-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleSave}
              disabled={!hasChanges || !titleDraft.trim() || updateMut.isPending}
              className="px-4 py-1.5 text-xs bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg disabled:opacity-50 transition-colors"
            >
              {updateMut.isPending ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>
      </div>
    </ModalBackdrop>
  )
}

function ModalBackdrop({ onClose, children }: { onClose: () => void; children: React.ReactNode }) {
  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      {children}
    </div>
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
    <div className={`bg-slate-800 border rounded-lg p-3 ${isPendingStatus ? 'border-amber-500/30' : 'border-slate-700'}`}>
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
                    <button
                      key={opt.value}
                      onClick={() => onRespond(interaction.id, opt.value)}
                      disabled={isPending}
                      className="px-3 py-1 text-xs bg-slate-700 border border-slate-600 rounded text-gray-200 hover:border-cyan-500/50 hover:text-white transition-colors disabled:opacity-50"
                      title={opt.description}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
              ) : (
                <div className="flex gap-2">
                  <input
                    value={freeText}
                    onChange={(e) => setFreeText(e.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter' && !e.nativeEvent.isComposing && freeText.trim()) onRespond(interaction.id, freeText) }}
                    className="flex-1 px-2 py-1 bg-slate-900 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
                    placeholder="Type your response..."
                  />
                  <button
                    onClick={() => { if (freeText.trim()) onRespond(interaction.id, freeText) }}
                    disabled={isPending || !freeText.trim()}
                    className="px-2 py-1 text-xs bg-cyan-600 text-white rounded disabled:opacity-50"
                  >
                    Send
                  </button>
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
    </div>
  )
}
