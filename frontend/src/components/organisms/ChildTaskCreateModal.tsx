import { useState, useEffect, useCallback, useMemo } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { requestWorktreeList, getWorktreeList } from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { X, GitBranch, RefreshCw, AlertTriangle, ArrowUpRight } from 'lucide-react'
import { shortId } from '@/lib/id'
import { Button, Input, Textarea, Select, Checkbox, Badge } from '../atoms/index.ts'
import { Modal, Card } from '../molecules/index.ts'

interface ChildTaskCreateModalProps {
  parentTask: Task
  projectId: string
  workflowId: string
  onCreated: (taskId: string) => void
  onClose: () => void
}

export function ChildTaskCreateModal({
  parentTask,
  projectId,
  workflowId,
  onCreated,
  onClose,
}: ChildTaskCreateModalProps) {
  const parentRef = `> Parent: ${parentTask.title} (${shortId(parentTask.id)})\n\n`

  const [title, setTitle] = useState('')
  const [description, setDescription] = useState(parentRef)
  const [useWorktree, setUseWorktree] = useState(parentTask.useWorktree ?? false)
  const [selectedWorktree, setSelectedWorktree] = useState(parentTask.metadata?.['worktree'] ?? '')
  const [inheritSession, setInheritSession] = useState(false)

  const createMut = useMutation(createTask)
  const requestWtMut = useMutation(requestWorktreeList)

  const isParentAgentRunning = parentTask.assignmentStatus === TaskAssignmentStatus.ASSIGNED

  // Use the parent's current status to look up the correct per-status session ID.
  const parentSessionKey = parentTask.statusId ? `session_id_${parentTask.statusId}` : ''
  const parentSessionId = parentSessionKey ? (parentTask.metadata?.[parentSessionKey] ?? '') : ''

  // Query cached worktree list
  const { data: wtData, refetch: refetchWorktrees } = useQuery(getWorktreeList, { projectId }, {
    enabled: useWorktree,
  })

  // Subscribe to worktree list events to refetch when a scan completes
  const worktreeEventTypes = useMemo(() => [EventType.WORKTREE_LIST], [])
  const onWorktreeEvent = useCallback(() => {
    refetchWorktrees()
  }, [refetchWorktrees])
  useEventSubscription(worktreeEventTypes, projectId, onWorktreeEvent)

  // Request worktree scan when worktree checkbox is toggled on
  useEffect(() => {
    if (useWorktree && projectId) {
      requestWtMut.mutate({ projectId })
    }
  }, [useWorktree, projectId])

  const worktrees = wtData?.worktrees ?? []

  const handleCreate = () => {
    if (!title.trim()) return
    const metadata: Record<string, string> = {
      source_task_id: parentTask.id,
    }
    if (selectedWorktree && useWorktree) {
      metadata['worktree'] = selectedWorktree
    }
    if (inheritSession && parentSessionId && parentSessionKey) {
      metadata[parentSessionKey] = parentSessionId
    }
    createMut.mutate(
      {
        projectId,
        workflowId,
        title: title.trim(),
        description,
        metadata,
        useWorktree,
      },
      {
        onSuccess: (res) => {
          onCreated(res.task?.id ?? '')
          onClose()
        },
      },
    )
  }

  return (
    <Modal open={true} onClose={onClose} size="full">
      {/* Header */}
      <div className="flex items-start justify-between px-4 pt-4 pb-1">
        <div className="flex-1 min-w-0 mr-3">
          <Input
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) handleCreate() }}
            className="!border-slate-600 text-base md:text-lg font-semibold !rounded"
            placeholder="Subtask title..."
          />
        </div>
        <button onClick={onClose} className="text-gray-500 hover:text-gray-300 transition-colors shrink-0 mt-1 p-1">
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Body */}
      <Modal.Body>
        {/* Parent task reference */}
        <Card variant="nested" className="!rounded-lg !py-2 !px-3">
          <div className="flex items-center gap-2 text-xs text-gray-400">
            <ArrowUpRight className="w-3.5 h-3.5 text-gray-500 shrink-0" />
            <span className="text-gray-500">Parent:</span>
            <span className="text-white font-medium truncate">{parentTask.title}</span>
            <span className="text-gray-600 font-mono shrink-0">{shortId(parentTask.id)}</span>
            {isParentAgentRunning && (
              <Badge color="cyan" size="xs" variant="outline" pill>Running</Badge>
            )}
          </div>
        </Card>

        <Textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          textareaSize="md"
          placeholder="Add description..."
        />

        {/* Inherit session option */}
        <div>
          <Checkbox
            label="Inherit Session (resume parent's conversation context)"
            checked={inheritSession}
            onChange={(e) => setInheritSession(e.target.checked)}
            disabled={!parentSessionId}
          />
          {!parentSessionId && (
            <p className="ml-6 mt-0.5 text-[11px] text-gray-600">
              No session available (parent has not been run yet)
            </p>
          )}
          {inheritSession && isParentAgentRunning && (
            <div className="ml-6 mt-1.5 flex items-start gap-1.5 text-xs text-amber-400 bg-amber-500/10 border border-amber-500/20 rounded px-2.5 py-1.5">
              <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
              <span>
                Parent agent is currently running. Resuming an active session may cause unexpected behavior
                if both tasks use the same session simultaneously.
              </span>
            </div>
          )}
        </div>

        {/* Agent settings */}
        <Checkbox
          label="Use Worktree (isolate changes in a git worktree)"
          checked={useWorktree}
          onChange={(e) => setUseWorktree(e.target.checked)}
        />

        {/* Worktree selection */}
        {useWorktree && (
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
        )}

        {/* Action buttons */}
        <div className="border-t border-slate-800 mt-4 pt-3 flex justify-end items-center gap-2">
          <Button variant="secondary" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={handleCreate}
            disabled={createMut.isPending || !title.trim()}
          >
            {createMut.isPending ? 'Creating...' : 'Create Subtask'}
          </Button>
        </div>
      </Modal.Body>
    </Modal>
  )
}
