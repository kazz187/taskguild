import { useState, useEffect, useCallback, useMemo } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { requestWorktreeList, getWorktreeList } from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { X, GitBranch, RefreshCw } from 'lucide-react'

interface TaskCreateModalProps {
  projectId: string
  workflowId: string
  defaultPermissionMode?: string
  defaultUseWorktree?: boolean
  onCreated: () => void
  onClose: () => void
}

export function TaskCreateModal({ projectId, workflowId, defaultPermissionMode, defaultUseWorktree, onCreated, onClose }: TaskCreateModalProps) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [permissionMode, setPermissionMode] = useState(defaultPermissionMode ?? '')
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
      { projectId, workflowId, title: title.trim(), description, metadata, permissionMode, useWorktree },
      {
        onSuccess: () => {
          onCreated()
          onClose()
        },
      },
    )
  }

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-0 md:p-4"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="bg-slate-900 border border-slate-700 rounded-none md:rounded-xl w-full h-full md:h-auto md:max-w-2xl md:max-h-[85vh] flex flex-col shadow-2xl">
        {/* Header */}
        <div className="flex items-start justify-between px-4 pt-4 pb-1">
          <div className="flex-1 min-w-0 mr-3">
            <input
              autoFocus
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) handleCreate() }}
              className="w-full px-2 py-1 bg-slate-800 border border-slate-600 rounded text-white text-base md:text-lg font-semibold focus:outline-none focus:border-cyan-500"
              placeholder="Task title..."
            />
          </div>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-300 transition-colors shrink-0 mt-1 p-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500 min-h-[150px] md:min-h-[200px]"
            placeholder="Add description..."
          />
          <div>
            <label className="block text-xs text-gray-400 mb-1">Permission Mode</label>
            <select
              value={permissionMode}
              onChange={(e) => setPermissionMode(e.target.value)}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
            >
              <option value="">Default (ask for permission)</option>
              <option value="acceptEdits">Accept Edits (auto-approve file changes)</option>
              <option value="bypassPermissions">Bypass Permissions (auto-approve all)</option>
            </select>
          </div>
          <label className="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
            <input
              type="checkbox"
              checked={useWorktree}
              onChange={(e) => setUseWorktree(e.target.checked)}
              className="accent-cyan-500"
            />
            Use Worktree (isolate changes in a git worktree)
          </label>

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
              <select
                value={selectedWorktree}
                onChange={(e) => setSelectedWorktree(e.target.value)}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
              >
                <option value="">New worktree (auto-generated)</option>
                {worktrees.map((wt) => (
                  <option key={wt.name} value={wt.name}>
                    {wt.name} ({wt.branch})
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="border-t border-slate-800 px-4 py-2 flex justify-end items-center gap-2">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-xs text-gray-400 hover:text-white transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleCreate}
            disabled={createMut.isPending || !title.trim()}
            className="px-4 py-1.5 text-xs bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg disabled:opacity-50 transition-colors"
          >
            {createMut.isPending ? 'Creating...' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  )
}
