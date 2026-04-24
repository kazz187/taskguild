import { useState, useEffect, useCallback, useMemo } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createTask, uploadTaskImage } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { requestWorktreeList, getWorktreeList } from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { TaskFormModal, TaskFormFields, type PendingImage } from '../molecules/index.ts'

interface TaskCreateModalProps {
  projectId: string
  workflowId: string
  defaultUseWorktree?: boolean
  onCreated: () => void
  onClose: () => void
}

export function TaskCreateModal({ projectId, workflowId, defaultUseWorktree, onCreated, onClose }: TaskCreateModalProps) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [effort, setEffort] = useState('')
  const [useWorktree, setUseWorktree] = useState(defaultUseWorktree ?? false)
  const [selectedWorktree, setSelectedWorktree] = useState('')
  const [pendingImages, setPendingImages] = useState<PendingImage[]>([])
  const createMut = useMutation(createTask)
  const uploadMut = useMutation(uploadTaskImage)
  const requestWtMut = useMutation(requestWorktreeList)

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
    const metadata: Record<string, string> = {}
    if (selectedWorktree) {
      metadata['worktree'] = selectedWorktree
    }

    try {
      const resp = await createMut.mutateAsync(
        { projectId, workflowId, title: title.trim(), description, metadata, useWorktree, effort },
      )

      // Upload pending images after task creation
      if (resp.task && pendingImages.length > 0) {
        const taskId = resp.task.id
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
        // Clean up preview URLs
        pendingImages.forEach(img => URL.revokeObjectURL(img.previewUrl))
      }

      onCreated()
      onClose()
    } catch {
      // createMut error state will show via isPending
    }
  }

  return (
    <TaskFormModal
      headerLabel="New Task"
      title={title}
      onTitleChange={setTitle}
      onClose={onClose}
      onSubmit={handleCreate}
      submitLabel="Create"
      submitPendingLabel="Creating..."
      isSubmitting={createMut.isPending || uploadMut.isPending}
      submitDisabled={!title.trim()}
    >
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
    </TaskFormModal>
  )
}
