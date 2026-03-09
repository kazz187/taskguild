import { useState } from 'react'
import { useMutation } from '@connectrpc/connect-query'
import { archiveTerminalTasks } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import type { WorkflowStatus } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { Sparkles, ChevronDown, ChevronRight, Loader2, CheckCircle2, AlertTriangle, Archive } from 'lucide-react'
import { Button } from '../atoms/index.ts'
import { Modal, Card } from '../molecules/index.ts'

interface CleanTasksDialogProps {
  projectId: string
  workflowId: string
  terminalTasks: Task[]
  statusById: Map<string, WorkflowStatus>
  onClose: () => void
  onArchived: () => void
}

export function CleanTasksDialog({
  projectId,
  workflowId,
  terminalTasks,
  statusById,
  onClose,
  onArchived,
}: CleanTasksDialogProps) {
  const [showSkipped, setShowSkipped] = useState(false)
  const archiveMut = useMutation(archiveTerminalTasks)
  const [result, setResult] = useState<{
    archivedCount: number
    skippedCount: number
    errors: string[]
  } | null>(null)

  // Split into archivable and skipped (agent actively running).
  // Pending tasks in a terminal status are safe to archive because no agent
  // will ever claim them — only actively assigned tasks should be skipped.
  const archivable = terminalTasks.filter(
    (t) => t.assignmentStatus !== TaskAssignmentStatus.ASSIGNED,
  )
  const skipped = terminalTasks.filter(
    (t) => t.assignmentStatus === TaskAssignmentStatus.ASSIGNED,
  )

  const handleArchive = () => {
    archiveMut.mutate(
      { projectId, workflowId },
      {
        onSuccess: (data) => {
          const archivedCount = data.archivedTasks?.length ?? 0
          const skippedCount = data.skippedTasks?.length ?? 0
          setResult({ archivedCount, skippedCount, errors: [] })
          onArchived()
        },
        onError: (err) => {
          setResult({
            archivedCount: 0,
            skippedCount: 0,
            errors: [err instanceof Error ? err.message : 'Unknown error'],
          })
        },
      },
    )
  }

  const isDone = result !== null
  const isRunning = archiveMut.isPending
  const hasErrors = result?.errors && result.errors.length > 0

  return (
    <Modal open={true} onClose={onClose} size="md" closeOnBackdrop={!isRunning}>
      <Modal.Header onClose={isRunning ? undefined : onClose}>
        <Sparkles className="w-5 h-5 text-amber-400" />
        <h3 className="text-lg font-semibold text-white">Archive Completed Tasks</h3>
      </Modal.Header>

      <Modal.Body>
        {/* Progress / Result */}
        {(isRunning || isDone) && (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              {isRunning && <Loader2 className="w-4 h-4 text-amber-400 animate-spin" />}
              {isDone && !hasErrors && (
                <CheckCircle2 className="w-4 h-4 text-emerald-400" />
              )}
              {isDone && hasErrors && (
                <AlertTriangle className="w-4 h-4 text-yellow-400" />
              )}
              <span className="text-sm text-gray-300">
                {isRunning
                  ? 'Archiving tasks...'
                  : hasErrors
                    ? `Archive failed`
                    : `${result!.archivedCount} task${result!.archivedCount !== 1 ? 's' : ''} archived successfully`}
              </span>
            </div>
            {isDone && result!.skippedCount > 0 && (
              <p className="text-xs text-gray-500">
                {result!.skippedCount} task{result!.skippedCount !== 1 ? 's' : ''} skipped
                (agent running)
              </p>
            )}
            {hasErrors && (
              <Card variant="error" className="space-y-1">
                {result!.errors.map((err, i) => (
                  <p key={i} className="text-[11px] text-red-300/70 font-mono">
                    {err}
                  </p>
                ))}
              </Card>
            )}
          </div>
        )}

        {/* Task list (before execution) */}
        {!isRunning && !isDone && (
          <>
            <p className="text-sm text-gray-400">
              The following{' '}
              <span className="text-white font-semibold">{archivable.length}</span> completed
              task{archivable.length !== 1 ? 's' : ''} will be archived:
            </p>

            <div className="space-y-2">
              {archivable.map((t) => {
                const statusName = statusById.get(t.statusId)?.name ?? t.statusId
                return (
                  <Card key={t.id} variant="nested" className="!rounded-lg">
                    <div className="flex items-center gap-2">
                      <Archive className="w-3.5 h-3.5 text-amber-400/60 shrink-0" />
                      <span className="text-sm text-white truncate">{t.title}</span>
                    </div>
                    <p className="text-[11px] text-gray-500 mt-1 pl-[22px]">{statusName}</p>
                  </Card>
                )
              })}
            </div>

            {/* Skipped tasks */}
            {skipped.length > 0 && (
              <div>
                <button
                  onClick={() => setShowSkipped(!showSkipped)}
                  className="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-300 transition-colors"
                >
                  {showSkipped ? (
                    <ChevronDown className="w-3.5 h-3.5" />
                  ) : (
                    <ChevronRight className="w-3.5 h-3.5" />
                  )}
                  {skipped.length} task{skipped.length !== 1 ? 's' : ''} skipped
                </button>
                {showSkipped && (
                  <div className="mt-2 space-y-1.5 pl-1">
                    {skipped.map((t) => (
                      <div key={t.id} className="flex items-start gap-2 text-[11px]">
                        <span className="text-gray-600 truncate shrink min-w-0">{t.title}</span>
                        <span className="text-gray-600 shrink-0">- Agent running</span>
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
          <Button variant="secondary" size="sm" onClick={onClose}>
            {isDone ? 'Close' : 'Cancel'}
          </Button>
        )}
        {!isRunning && !isDone && archivable.length > 0 && (
          <Button variant="danger" size="sm" onClick={handleArchive}>
            Archive {archivable.length} Task{archivable.length !== 1 ? 's' : ''}
          </Button>
        )}
      </Modal.Footer>
    </Modal>
  )
}
