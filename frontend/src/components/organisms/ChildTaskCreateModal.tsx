import { useState, useEffect, useCallback, useMemo } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createTask, uploadTaskImage } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { requestWorktreeList, getWorktreeList } from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { AlertTriangle, ArrowUpRight } from 'lucide-react'
import { shortId } from '@/lib/id'
import { Checkbox, Badge } from '../atoms/index.ts'
import { TaskFormModal, Card, TaskFormFields, type PendingImage } from '../molecules/index.ts'

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
  const [effort, setEffort] = useState(parentTask.effort ?? '')
  const [useWorktree, setUseWorktree] = useState(parentTask.useWorktree ?? false)
  const [selectedWorktree, setSelectedWorktree] = useState(parentTask.metadata?.['worktree'] ?? '')
  const [inheritSession, setInheritSession] = useState(false)
  const [pendingImages, setPendingImages] = useState<PendingImage[]>([])

  const createMut = useMutation(createTask)
  const uploadMut = useMutation(uploadTaskImage)
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

  const handleCreate = async () => {
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
    try {
      const res = await createMut.mutateAsync({
        projectId,
        workflowId,
        title: title.trim(),
        description,
        metadata,
        useWorktree,
        effort,
      })

      // Upload pending images after task creation
      if (res.task && pendingImages.length > 0) {
        const taskId = res.task.id
        for (const img of pendingImages) {
          try {
            const arrayBuffer = await img.file.arrayBuffer()
            const data = new Uint8Array(arrayBuffer)
            await uploadMut.mutateAsync({
              taskId,
              filename: img.file.name,
              mediaType: img.file.type,
              data,
            })
          } catch (err) {
            console.error('Failed to upload pending image:', err)
          }
        }
        pendingImages.forEach(img => URL.revokeObjectURL(img.previewUrl))
      }

      onCreated(res.task?.id ?? '')
      onClose()
    } catch {
      // error handled by mutation state
    }
  }

  return (
    <TaskFormModal
      headerLabel="New Subtask"
      title={title}
      onTitleChange={setTitle}
      titlePlaceholder="Subtask title..."
      onClose={onClose}
      onSubmit={handleCreate}
      submitLabel="Create Subtask"
      submitPendingLabel="Creating..."
      isSubmitting={createMut.isPending}
      submitDisabled={!title.trim()}
    >
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

      <TaskFormFields
        description={description}
        onDescriptionChange={setDescription}
        pendingImages={pendingImages}
        onPendingImagesChange={setPendingImages}
        effort={effort}
        onEffortChange={setEffort}
        useWorktree={useWorktree}
        onUseWorktreeChange={setUseWorktree}
        selectedWorktree={selectedWorktree}
        onSelectedWorktreeChange={setSelectedWorktree}
        worktrees={worktrees}
        onRequestWorktrees={() => requestWtMut.mutate({ projectId })}
        worktreeRequestPending={requestWtMut.isPending}
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
    </TaskFormModal>
  )
}
