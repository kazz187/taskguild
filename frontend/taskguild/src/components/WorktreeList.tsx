import { useState, useEffect, useCallback, useMemo } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  requestWorktreeList,
  getWorktreeList,
  requestWorktreeDelete,
} from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import type { WorktreeInfo } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { GitFork, GitBranch, RefreshCw, Trash2, AlertTriangle, X, FileText } from 'lucide-react'

export function WorktreeList({ projectId }: { projectId: string }) {
  const requestListMut = useMutation(requestWorktreeList)
  const requestDeleteMut = useMutation(requestWorktreeDelete)

  const { data, refetch, isLoading } = useQuery(getWorktreeList, { projectId })
  const worktrees = data?.worktrees ?? []

  const [deleteTarget, setDeleteTarget] = useState<WorktreeInfo | null>(null)

  // Subscribe to worktree list events AND worktree deleted events
  const eventTypes = useMemo(
    () => [EventType.WORKTREE_LIST, EventType.WORKTREE_DELETED],
    [],
  )
  const onEvent = useCallback(() => {
    refetch()
  }, [refetch])
  useEventSubscription(eventTypes, projectId, onEvent)

  // Request worktree scan on mount
  useEffect(() => {
    if (projectId) {
      requestListMut.mutate({ projectId })
    }
  }, [projectId]) // eslint-disable-line react-hooks/exhaustive-deps

  const handleRefresh = () => {
    requestListMut.mutate({ projectId })
  }

  const handleDelete = (wt: WorktreeInfo) => {
    setDeleteTarget(wt)
  }

  const executeDelete = (force: boolean) => {
    if (!deleteTarget) return
    requestDeleteMut.mutate(
      { projectId, worktreeName: deleteTarget.name, force },
      {
        onSuccess: () => {
          setDeleteTarget(null)
        },
      },
    )
  }

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <div className="flex items-center gap-2">
          <GitFork className="w-5 h-5 text-cyan-400" />
          <h2 className="text-lg md:text-xl font-bold text-white">Worktrees</h2>
          <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5">
            {worktrees.length}
          </span>
        </div>
        <button
          onClick={handleRefresh}
          disabled={requestListMut.isPending}
          className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs md:text-sm md:px-3 text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors disabled:opacity-50"
          title="Refresh worktree list"
        >
          <RefreshCw className={`w-4 h-4 ${requestListMut.isPending ? 'animate-spin' : ''}`} />
          <span className="hidden sm:inline">Refresh</span>
        </button>
      </div>

      {/* Worktree Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading worktrees...</p>}

      <div className="space-y-3">
        {worktrees.map((wt) => (
          <WorktreeCard
            key={wt.name}
            worktree={wt}
            onDelete={() => handleDelete(wt)}
            isDeleting={requestDeleteMut.isPending}
          />
        ))}

        {!isLoading && worktrees.length === 0 && (
          <div className="text-center py-12 text-gray-500">
            <GitFork className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No worktrees found.</p>
            <p className="text-xs mt-1">Worktrees are created when tasks use the worktree option.</p>
          </div>
        )}
      </div>

      {/* Delete Confirmation Dialog */}
      {deleteTarget && (
        <DeleteWorktreeDialog
          worktree={deleteTarget}
          onConfirm={(force) => executeDelete(force)}
          onCancel={() => setDeleteTarget(null)}
          isPending={requestDeleteMut.isPending}
        />
      )}
    </div>
  )
}

function WorktreeCard({
  worktree,
  onDelete,
  isDeleting,
}: {
  worktree: WorktreeInfo
  onDelete: () => void
  isDeleting: boolean
}) {
  const [showFiles, setShowFiles] = useState(false)

  return (
    <div className="bg-slate-900 border border-slate-800 rounded-xl p-4 hover:border-slate-700 transition-colors">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          <GitFork className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1 flex-wrap">
              <h3 className="text-sm font-semibold text-white truncate">{worktree.name}</h3>
              {worktree.hasChanges && (
                <span className="flex items-center gap-0.5 text-[10px] text-yellow-400 bg-yellow-500/10 border border-yellow-500/20 rounded-full px-1.5 py-0.5 shrink-0">
                  <AlertTriangle className="w-2.5 h-2.5" />
                  changes
                </span>
              )}
              {worktree.taskId && (
                <span className="text-[10px] text-gray-500 bg-slate-800 rounded-full px-1.5 py-0.5 shrink-0">
                  task: {worktree.taskId.slice(-6)}
                </span>
              )}
            </div>
            <div className="flex items-center gap-1.5 text-xs text-gray-400">
              <GitBranch className="w-3 h-3" />
              <span className="truncate">{worktree.branch}</span>
            </div>

            {/* Changed files toggle */}
            {worktree.hasChanges && worktree.changedFiles.length > 0 && (
              <div className="mt-2">
                <button
                  onClick={() => setShowFiles(!showFiles)}
                  className="text-[11px] text-yellow-400 hover:text-yellow-300 transition-colors"
                >
                  {showFiles ? 'Hide' : 'Show'} {worktree.changedFiles.length} changed file{worktree.changedFiles.length !== 1 ? 's' : ''}
                </button>
                {showFiles && (
                  <div className="mt-1 pl-2 border-l border-slate-700 space-y-0.5">
                    {worktree.changedFiles.map((file) => (
                      <div key={file} className="flex items-center gap-1 text-[11px] text-gray-500 font-mono">
                        <FileText className="w-3 h-3 shrink-0" />
                        <span className="truncate">{file}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Delete button */}
        <div className="flex items-center gap-1 shrink-0 ml-2">
          <button
            onClick={onDelete}
            disabled={isDeleting}
            className="p-1.5 text-gray-500 hover:text-red-400 transition-colors rounded-lg hover:bg-slate-800 disabled:opacity-50"
            title="Delete worktree"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  )
}

function DeleteWorktreeDialog({
  worktree,
  onConfirm,
  onCancel,
  isPending,
}: {
  worktree: WorktreeInfo
  onConfirm: (force: boolean) => void
  onCancel: () => void
  isPending: boolean
}) {
  const hasChanges = worktree.hasChanges

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onCancel() }}
    >
      <div className="bg-slate-900 border border-slate-700 rounded-xl w-full max-w-md shadow-2xl">
        <div className="flex items-center justify-between px-4 pt-4 pb-2">
          <h3 className="text-lg font-semibold text-white">Delete Worktree</h3>
          <button onClick={onCancel} className="text-gray-500 hover:text-gray-300 transition-colors p-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="px-4 pb-4 space-y-3">
          <p className="text-sm text-gray-400">
            Are you sure you want to delete worktree <span className="text-white font-mono">{worktree.name}</span>?
          </p>
          <p className="text-sm text-gray-400">
            This will also delete branch <span className="text-white font-mono">{worktree.branch}</span>.
          </p>

          {hasChanges && (
            <div className="bg-yellow-500/10 border border-yellow-500/20 rounded-lg p-3">
              <div className="flex items-center gap-1.5 mb-2">
                <AlertTriangle className="w-4 h-4 text-yellow-400" />
                <span className="text-sm font-medium text-yellow-400">Uncommitted changes detected</span>
              </div>
              <div className="space-y-0.5 max-h-40 overflow-y-auto">
                {worktree.changedFiles.map((file) => (
                  <div key={file} className="text-[11px] text-yellow-300/70 font-mono truncate">
                    {file}
                  </div>
                ))}
              </div>
              <p className="text-xs text-yellow-400/70 mt-2">
                These changes will be permanently lost.
              </p>
            </div>
          )}
        </div>

        <div className="border-t border-slate-800 px-4 py-3 flex justify-end gap-2">
          <button
            onClick={onCancel}
            className="px-3 py-1.5 text-xs text-gray-400 hover:text-white transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={() => onConfirm(hasChanges)}
            disabled={isPending}
            className="px-4 py-1.5 text-xs bg-red-600 hover:bg-red-500 text-white rounded-lg disabled:opacity-50 transition-colors"
          >
            {isPending ? 'Deleting...' : hasChanges ? 'Force Delete' : 'Delete'}
          </button>
        </div>
      </div>
    </div>
  )
}
