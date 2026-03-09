import { useState, useEffect, useCallback, useMemo } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { requestWorktreeList, getWorktreeList } from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { X, GitBranch, RefreshCw } from 'lucide-react'
import { Button, Input, Textarea, Select, Checkbox } from '../atoms/index.ts'
import { Modal } from '../molecules/index.ts'

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
  const [useWorktree, setUseWorktree] = useState(defaultUseWorktree ?? false)
  const [selectedWorktree, setSelectedWorktree] = useState('')
  const createMut = useMutation(createTask)
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

  const handleCreate = () => {
    if (!title.trim()) return
    const metadata: Record<string, string> = {}
    if (selectedWorktree) {
      metadata['worktree'] = selectedWorktree
    }
    createMut.mutate(
      { projectId, workflowId, title: title.trim(), description, metadata, useWorktree },
      {
        onSuccess: () => {
          onCreated()
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
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          textareaSize="md"
          placeholder="Add description..."
        />

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
            {createMut.isPending ? 'Creating...' : 'Create'}
          </Button>
        </div>
      </Modal.Body>
    </Modal>
  )
}
