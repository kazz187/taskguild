import { useState, useEffect, useCallback, useMemo } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { Link } from '@tanstack/react-router'
import {
  requestWorktreeList,
  getWorktreeList,
  requestWorktreeDelete,
  requestGitPullMain,
} from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import type { WorktreeInfo } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { listTasks } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { GitFork, GitBranch, RefreshCw, Trash2, AlertTriangle, FileText, Home, Download, CheckCircle2, XCircle, ClipboardList, Sparkles, ChevronDown, ChevronRight, Loader2 } from 'lucide-react'
import { Button, Badge } from '../atoms/index.ts'
import { Card, Modal, PageHeading } from '../molecules/index.ts'

// --- Git pull main result hook ---

interface PullResult {
  success: boolean
  output: string
  errorMessage: string
  timestamp: Date
}

function useGitPullMainResult(projectId: string): {
  result: PullResult | null
  clearResult: () => void
} {
  const [result, setResult] = useState<PullResult | null>(null)

  const gitPullEventTypes = useMemo(() => [EventType.GIT_PULL_MAIN_RESULT], [])
  const onGitPullEvent = useCallback((event: { type: EventType; metadata: { [k: string]: string } }) => {
    if (event.type !== EventType.GIT_PULL_MAIN_RESULT) return
    setResult({
      success: event.metadata['success'] === 'true',
      output: event.metadata['output'] || '',
      errorMessage: event.metadata['error_message'] || '',
      timestamp: new Date(),
    })
  }, [])
  useEventSubscription(gitPullEventTypes, projectId, onGitPullEvent)

  const clearResult = useCallback(() => setResult(null), [])
  return { result, clearResult }
}

export function WorktreeList({ projectId }: { projectId: string }) {
  const requestListMut = useMutation(requestWorktreeList)
  const requestDeleteMut = useMutation(requestWorktreeDelete)

  const { data, refetch, isLoading } = useQuery(getWorktreeList, { projectId })
  const worktrees = data?.worktrees ?? []

  // Fetch tasks and workflows for worktree-task association
  const { data: tasksData, refetch: refetchTasks } = useQuery(listTasks, {
    projectId,
    pagination: { limit: 10000 },
  })
  const tasks = tasksData?.tasks ?? []

  const { data: workflowsData } = useQuery(listWorkflows, { projectId })
  const workflows = workflowsData?.workflows ?? []

  // Build statusId -> { name, isInitial, isTerminal } map from all workflows
  const statusMap = useMemo(() => {
    const map = new Map<string, { name: string; isInitial: boolean; isTerminal: boolean }>()
    for (const wf of workflows) {
      for (const st of wf.statuses) {
        map.set(st.id, { name: st.name, isInitial: st.isInitial, isTerminal: st.isTerminal })
      }
    }
    return map
  }, [workflows])

  // Group tasks by worktree name
  const tasksByWorktree = useMemo(() => {
    const map = new Map<string, Task[]>()
    for (const t of tasks) {
      const wtName = t.metadata['worktree']
      if (!wtName) continue
      const arr = map.get(wtName)
      if (arr) {
        arr.push(t)
      } else {
        map.set(wtName, [t])
      }
    }
    return map
  }, [tasks])

  // Compute cleanable worktrees: no uncommitted changes AND (no tasks OR all tasks terminal)
  const cleanableWorktrees = useMemo(() => {
    return worktrees.filter((wt) => {
      if (wt.hasChanges) return false
      const associatedTasks = tasksByWorktree.get(wt.name) ?? []
      if (associatedTasks.length === 0) return true
      return associatedTasks.every((t) => statusMap.get(t.statusId)?.isTerminal)
    })
  }, [worktrees, tasksByWorktree, statusMap])

  // Compute skipped worktrees with reasons
  const skippedWorktrees = useMemo(() => {
    return worktrees
      .filter((wt) => !cleanableWorktrees.includes(wt))
      .map((wt) => {
        const reasons: string[] = []
        if (wt.hasChanges) reasons.push('Uncommitted changes')
        const associatedTasks = tasksByWorktree.get(wt.name) ?? []
        if (associatedTasks.length > 0) {
          const hasActiveTasks = associatedTasks.some((t) => !statusMap.get(t.statusId)?.isTerminal)
          if (hasActiveTasks) reasons.push('Has active tasks')
        }
        return { worktree: wt, reasons }
      })
  }, [worktrees, cleanableWorktrees, tasksByWorktree, statusMap])

  const [deleteTarget, setDeleteTarget] = useState<WorktreeInfo | null>(null)
  const [showCleanDialog, setShowCleanDialog] = useState(false)
  const [cleanProgress, setCleanProgress] = useState<{ current: number; total: number; errors: string[] } | null>(null)

  // Subscribe to worktree list events, worktree deleted events, AND task updates
  const eventTypes = useMemo(
    () => [
      EventType.WORKTREE_LIST,
      EventType.WORKTREE_DELETED,
      EventType.TASK_UPDATED,
      EventType.TASK_STATUS_CHANGED,
    ],
    [],
  )
  const onEvent = useCallback(() => {
    refetch()
    refetchTasks()
  }, [refetch, refetchTasks])
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

  const executeClean = async () => {
    const targets = [...cleanableWorktrees]
    if (targets.length === 0) return

    setCleanProgress({ current: 0, total: targets.length, errors: [] })

    const errors: string[] = []
    for (let i = 0; i < targets.length; i++) {
      setCleanProgress({ current: i + 1, total: targets.length, errors: [...errors] })
      try {
        await new Promise<void>((resolve, reject) => {
          requestDeleteMut.mutate(
            { projectId, worktreeName: targets[i].name, force: false },
            {
              onSuccess: () => resolve(),
              onError: (err) => reject(err),
            },
          )
        })
      } catch (err) {
        errors.push(`${targets[i].name}: ${err instanceof Error ? err.message : 'Unknown error'}`)
      }
    }

    if (errors.length === 0) {
      setShowCleanDialog(false)
      setCleanProgress(null)
    } else {
      setCleanProgress({ current: targets.length, total: targets.length, errors })
    }
  }

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <PageHeading icon={GitFork} title="Worktrees" iconColor="text-cyan-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {worktrees.length}
          </Badge>
        </PageHeading>
        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            icon={<Sparkles className="w-4 h-4" />}
            onClick={() => setShowCleanDialog(true)}
            disabled={cleanableWorktrees.length === 0 || cleanProgress !== null}
            title={cleanableWorktrees.length > 0 ? `Clean ${cleanableWorktrees.length} stale worktrees` : 'No worktrees to clean'}
            className="border border-slate-700 hover:text-amber-300 hover:border-amber-500/40 disabled:hover:text-gray-400 disabled:hover:border-slate-700"
          >
            <span className="hidden sm:inline">Clean</span>
            {cleanableWorktrees.length > 0 && (
              <Badge color="amber" size="xs" pill>
                {cleanableWorktrees.length}
              </Badge>
            )}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            icon={<RefreshCw className={`w-4 h-4 ${requestListMut.isPending ? 'animate-spin' : ''}`} />}
            onClick={handleRefresh}
            disabled={requestListMut.isPending}
            title="Refresh worktree list"
            className="border border-slate-700 hover:border-slate-600"
          >
            <span className="hidden sm:inline">Refresh</span>
          </Button>
        </div>
      </div>

      {/* Main Branch Section */}
      <MainBranchSection projectId={projectId} />

      {/* Worktree Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading worktrees...</p>}

      <div className="space-y-3">
        {worktrees.map((wt) => (
          <WorktreeCard
            key={wt.name}
            worktree={wt}
            projectId={projectId}
            tasks={tasksByWorktree.get(wt.name) ?? []}
            statusMap={statusMap}
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
      <DeleteWorktreeDialog
        worktree={deleteTarget}
        onConfirm={(force) => executeDelete(force)}
        onCancel={() => setDeleteTarget(null)}
        isPending={requestDeleteMut.isPending}
      />

      {/* Clean Confirmation Dialog */}
      <CleanWorktreeDialog
        open={showCleanDialog}
        cleanable={cleanableWorktrees}
        skipped={skippedWorktrees}
        tasksByWorktree={tasksByWorktree}
        statusMap={statusMap}
        progress={cleanProgress}
        onConfirm={executeClean}
        onCancel={() => { setShowCleanDialog(false); setCleanProgress(null) }}
      />
    </div>
  )
}

function MainBranchSection({ projectId }: { projectId: string }) {
  const pullMainMut = useMutation(requestGitPullMain)
  const { result, clearResult } = useGitPullMainResult(projectId)

  const handlePull = () => {
    clearResult()
    pullMainMut.mutate({ projectId })
  }

  return (
    <div className="mb-6">
      <div className="flex items-center gap-2 mb-3">
        <Home className="w-4 h-4 text-emerald-400" />
        <h3 className="text-sm font-semibold text-gray-300">Main Branch</h3>
      </div>
      <Card>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <GitBranch className="w-5 h-5 text-emerald-400 shrink-0" />
            <div>
              <h4 className="text-sm font-semibold text-white">main</h4>
              <p className="text-xs text-gray-500">git pull origin main</p>
            </div>
          </div>
          <Button
            variant="secondary"
            size="sm"
            icon={<Download className={`w-3.5 h-3.5 ${pullMainMut.isPending ? 'animate-bounce' : ''}`} />}
            onClick={handlePull}
            disabled={pullMainMut.isPending}
            className="text-emerald-400 hover:text-white border border-emerald-500/30 hover:border-emerald-500/60 hover:bg-emerald-500/10"
          >
            {pullMainMut.isPending ? 'Pulling...' : 'Pull Latest'}
          </Button>
        </div>

        {/* Result display */}
        {result && (
          <div className={`mt-3 rounded-lg p-3 ${
            result.success
              ? 'bg-emerald-500/10 border border-emerald-500/20'
              : 'bg-red-500/10 border border-red-500/20'
          }`}>
            <div className="flex items-center gap-1.5 mb-2">
              {result.success ? (
                <CheckCircle2 className="w-4 h-4 text-emerald-400" />
              ) : (
                <XCircle className="w-4 h-4 text-red-400" />
              )}
              <span className={`text-xs font-medium ${result.success ? 'text-emerald-400' : 'text-red-400'}`}>
                {result.success ? 'Pull successful' : 'Pull failed'}
              </span>
              <span className="text-[10px] text-gray-500 ml-auto">
                {result.timestamp.toLocaleTimeString()}
              </span>
            </div>
            {result.output && (
              <pre className="text-[11px] text-gray-400 font-mono whitespace-pre-wrap break-all max-h-48 overflow-y-auto">
                {result.output}
              </pre>
            )}
            {result.errorMessage && (
              <pre className="text-[11px] text-red-300/70 font-mono whitespace-pre-wrap break-all max-h-48 overflow-y-auto">
                {result.errorMessage}
              </pre>
            )}
          </div>
        )}
      </Card>
    </div>
  )
}

function WorktreeCard({
  worktree,
  projectId,
  tasks,
  statusMap,
  onDelete,
  isDeleting,
}: {
  worktree: WorktreeInfo
  projectId: string
  tasks: Task[]
  statusMap: Map<string, { name: string; isInitial: boolean; isTerminal: boolean }>
  onDelete: () => void
  isDeleting: boolean
}) {
  const [showFiles, setShowFiles] = useState(false)

  return (
    <Card className="hover:border-slate-700 transition-colors">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          <GitFork className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1 flex-wrap">
              <h3 className="text-sm font-semibold text-white truncate">{worktree.name}</h3>
              {worktree.hasChanges && (
                <Badge color="yellow" size="xs" pill variant="outline" icon={<AlertTriangle className="w-2.5 h-2.5" />}>
                  changes
                </Badge>
              )}
            </div>
            <div className="flex items-center gap-1.5 text-xs text-gray-400">
              <GitBranch className="w-3 h-3" />
              <span className="truncate">{worktree.branch}</span>
            </div>

            {/* Associated tasks */}
            {tasks.length > 0 && (
              <div className="mt-2">
                <div className="flex items-center gap-1 mb-1">
                  <ClipboardList className="w-3 h-3 text-gray-500" />
                  <span className="text-[11px] text-gray-500">Tasks ({tasks.length})</span>
                </div>
                <div className="space-y-1 pl-0.5">
                  {tasks.map((t) => {
                    const status = statusMap.get(t.statusId)
                    const statusName = status?.name ?? t.statusId
                    const badgeColor: 'blue' | 'green' | 'gray' = status?.isInitial
                      ? 'blue'
                      : status?.isTerminal
                        ? 'green'
                        : 'gray'
                    return (
                      <Link
                        key={t.id}
                        to="/projects/$projectId/tasks/$taskId"
                        params={{ projectId, taskId: t.id }}
                        className="flex items-center gap-1.5 group"
                      >
                        <Badge color={badgeColor} size="xs" pill>
                          {statusName}
                        </Badge>
                        <span className="text-[11px] text-gray-400 group-hover:text-cyan-400 transition-colors truncate">
                          {t.title}
                        </span>
                      </Link>
                    )
                  })}
                </div>
              </div>
            )}

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
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            icon={<Trash2 className="w-3.5 h-3.5" />}
            onClick={onDelete}
            disabled={isDeleting}
            title="Delete worktree"
            className="hover:text-red-400"
          />
        </div>
      </div>
    </Card>
  )
}

function DeleteWorktreeDialog({
  worktree,
  onConfirm,
  onCancel,
  isPending,
}: {
  worktree: WorktreeInfo | null
  onConfirm: (force: boolean) => void
  onCancel: () => void
  isPending: boolean
}) {
  const hasChanges = worktree?.hasChanges ?? false

  return (
    <Modal open={worktree !== null} onClose={onCancel} size="sm">
      <Modal.Header onClose={onCancel}>
        <h3 className="text-lg font-semibold text-white">Delete Worktree</h3>
      </Modal.Header>

      <Modal.Body>
        <p className="text-sm text-gray-400">
          Are you sure you want to delete worktree <span className="text-white font-mono">{worktree?.name}</span>?
        </p>
        <p className="text-sm text-gray-400">
          This will also delete branch <span className="text-white font-mono">{worktree?.branch}</span>.
        </p>

        {hasChanges && (
          <div className="bg-yellow-500/10 border border-yellow-500/20 rounded-lg p-3">
            <div className="flex items-center gap-1.5 mb-2">
              <AlertTriangle className="w-4 h-4 text-yellow-400" />
              <span className="text-sm font-medium text-yellow-400">Uncommitted changes detected</span>
            </div>
            <div className="space-y-0.5 max-h-40 overflow-y-auto">
              {worktree?.changedFiles.map((file) => (
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
      </Modal.Body>

      <Modal.Footer>
        <Button
          variant="secondary"
          size="xs"
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button
          variant="danger"
          size="xs"
          onClick={() => onConfirm(hasChanges)}
          disabled={isPending}
          className="bg-red-600 hover:bg-red-500"
        >
          {isPending ? 'Deleting...' : hasChanges ? 'Force Delete' : 'Delete'}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}

function CleanWorktreeDialog({
  open,
  cleanable,
  skipped,
  tasksByWorktree,
  statusMap,
  progress,
  onConfirm,
  onCancel,
}: {
  open: boolean
  cleanable: WorktreeInfo[]
  skipped: { worktree: WorktreeInfo; reasons: string[] }[]
  tasksByWorktree: Map<string, Task[]>
  statusMap: Map<string, { name: string; isInitial: boolean; isTerminal: boolean }>
  progress: { current: number; total: number; errors: string[] } | null
  onConfirm: () => void
  onCancel: () => void
}) {
  const [showSkipped, setShowSkipped] = useState(false)
  const isRunning = progress !== null && progress.current < progress.total
  const isDone = progress !== null && progress.current >= progress.total
  const hasErrors = progress !== null && progress.errors.length > 0

  return (
    <Modal open={open} onClose={onCancel} closeOnBackdrop={!isRunning}>
      <Modal.Header onClose={!isRunning ? onCancel : undefined}>
        <Sparkles className="w-5 h-5 text-amber-400" />
        <h3 className="text-lg font-semibold text-white">Clean Worktrees</h3>
      </Modal.Header>

      <Modal.Body>
        {/* Progress bar during execution */}
        {progress && (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              {isRunning && <Loader2 className="w-4 h-4 text-amber-400 animate-spin" />}
              {isDone && !hasErrors && <CheckCircle2 className="w-4 h-4 text-emerald-400" />}
              {isDone && hasErrors && <AlertTriangle className="w-4 h-4 text-yellow-400" />}
              <span className="text-sm text-gray-300">
                {isRunning
                  ? `Cleaning ${progress.current}/${progress.total}...`
                  : hasErrors
                    ? `Completed with ${progress.errors.length} error${progress.errors.length !== 1 ? 's' : ''}`
                    : 'All worktrees cleaned successfully'}
              </span>
            </div>
            <div className="w-full bg-slate-800 rounded-full h-1.5">
              <div
                className={`h-1.5 rounded-full transition-all duration-300 ${hasErrors ? 'bg-yellow-500' : 'bg-amber-400'}`}
                style={{ width: `${(progress.current / progress.total) * 100}%` }}
              />
            </div>
            {hasErrors && (
              <Card variant="error" className="space-y-1 p-2">
                {progress.errors.map((err, i) => (
                  <p key={i} className="text-[11px] text-red-300/70 font-mono">{err}</p>
                ))}
              </Card>
            )}
          </div>
        )}

        {/* Cleanable worktrees list */}
        {!progress && (
          <>
            <p className="text-sm text-gray-400">
              The following <span className="text-white font-semibold">{cleanable.length}</span> worktree{cleanable.length !== 1 ? 's' : ''} will be deleted:
            </p>

            <div className="space-y-2">
              {cleanable.map((wt) => {
                const associatedTasks = tasksByWorktree.get(wt.name) ?? []
                const hasNoTasks = associatedTasks.length === 0
                return (
                  <Card key={wt.name} variant="nested" className="border-slate-700/50">
                    <div className="flex items-center gap-2">
                      <Trash2 className="w-3.5 h-3.5 text-red-400/60 shrink-0" />
                      <span className="text-sm text-white font-mono truncate">{wt.name}</span>
                    </div>
                    <p className="text-[11px] text-gray-500 mt-1 ml-5.5 pl-[22px]">
                      {hasNoTasks ? 'No associated tasks' : `All tasks completed (${associatedTasks.map(t => statusMap.get(t.statusId)?.name ?? t.statusId).join(', ')})`}
                    </p>
                  </Card>
                )
              })}
            </div>

            {/* Skipped worktrees */}
            {skipped.length > 0 && (
              <div>
                <button
                  onClick={() => setShowSkipped(!showSkipped)}
                  className="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-300 transition-colors"
                >
                  {showSkipped
                    ? <ChevronDown className="w-3.5 h-3.5" />
                    : <ChevronRight className="w-3.5 h-3.5" />
                  }
                  {skipped.length} worktree{skipped.length !== 1 ? 's' : ''} skipped
                </button>
                {showSkipped && (
                  <div className="mt-2 space-y-1.5 pl-1">
                    {skipped.map(({ worktree: wt, reasons }) => (
                      <div key={wt.name} className="flex items-start gap-2 text-[11px]">
                        <span className="text-gray-600 font-mono truncate shrink min-w-0">{wt.name}</span>
                        <span className="text-gray-600 shrink-0">- {reasons.join(', ')}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </Modal.Body>

      <Modal.Footer>
        {!isRunning && (
          <Button
            variant="secondary"
            size="xs"
            onClick={onCancel}
          >
            {isDone ? 'Close' : 'Cancel'}
          </Button>
        )}
        {!progress && (
          <Button
            variant="danger"
            size="xs"
            onClick={onConfirm}
          >
            Clean {cleanable.length} Worktree{cleanable.length !== 1 ? 's' : ''}
          </Button>
        )}
      </Modal.Footer>
    </Modal>
  )
}
