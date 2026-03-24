import { useRef, useState } from 'react'
import { useDraggable } from '@dnd-kit/core'
import { useNavigate } from '@tanstack/react-router'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { Bot, Clock, GitBranch, Loader, Pencil, ArrowRight, AlertTriangle, CopyPlus, ArrowUpRight, Layers, Square, Play } from 'lucide-react'
import { shortId } from '@/lib/id'
import { Badge } from '../atoms/index.ts'
import { DropdownMenu } from '../molecules/index.ts'

interface TransitionTarget {
  id: string
  name: string
  isForce: boolean
}

interface TaskCardProps {
  task: Task
  onEdit?: (taskId: string) => void
  onCreateChild?: (taskId: string) => void
  isDragOverlay?: boolean
  /** Available status transitions for this task (mobile UI) */
  transitionTargets?: TransitionTarget[]
  /** Callback when a status transition is selected (mobile UI) */
  onTransition?: (taskId: string, targetStatusId: string, isForce: boolean) => void
  /** Number of child tasks for this task */
  childCount?: number
  /** Parent task title (when this task is a child) */
  parentTaskTitle?: string
  /** Callback to stop a running task */
  onStop?: (taskId: string) => void
  /** Callback to resume a stopped task */
  onResume?: (taskId: string) => void
  /** Whether the current status has an agent configured (for resume button) */
  statusHasAgent?: boolean
}

export function TaskCard({ task, onEdit, onCreateChild, isDragOverlay, transitionTargets, onTransition, childCount, parentTaskTitle, onStop, onResume, statusHasAgent }: TaskCardProps) {
  const navigate = useNavigate()
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: task.id,
    data: { task },
  })
  const [showTransitionMenu, setShowTransitionMenu] = useState(false)
  const transitionBtnRef = useRef<HTMLButtonElement>(null)

  const handleClick = () => {
    if (isDragging) return
    navigate({ to: '/projects/$projectId/tasks/$taskId', params: { projectId: task.projectId, taskId: task.id } })
  }

  const hasTransitions = transitionTargets && transitionTargets.length > 0

  // Only block force-move when agent is actively running (assigned).
  // Pending tasks (agent not yet started) are allowed to be force-moved.
  const isAgentRunning =
    task.assignmentStatus === TaskAssignmentStatus.ASSIGNED

  return (
    <div
      ref={setNodeRef}
      {...listeners}
      {...attributes}
      onClick={handleClick}
      className={`group bg-slate-800 border border-slate-700 rounded-lg p-3 hover:border-slate-600 transition-colors cursor-pointer ${
        isDragging && !isDragOverlay ? 'opacity-50' : ''
      }`}
    >
      <div className="flex items-start justify-between gap-2">
        <h4 className="text-sm font-medium text-white leading-snug flex-1 min-w-0">
          {task.title}
        </h4>
        <div className="flex items-center gap-1 shrink-0">
          {/* Stop button (visible when task is running) */}
          {isAgentRunning && onStop && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onStop(task.id)
              }}
              onPointerDown={(e) => e.stopPropagation()}
              className="opacity-100 md:opacity-0 md:group-hover:opacity-100 shrink-0 p-1 text-red-400 hover:text-red-300 hover:bg-red-500/10 rounded transition-all"
              title="Stop task"
            >
              <Square className="w-3.5 h-3.5" />
            </button>
          )}
          {/* Resume button (visible when task is stopped and status has agent) */}
          {!isAgentRunning && task.assignmentStatus === TaskAssignmentStatus.UNASSIGNED && statusHasAgent && onResume && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onResume(task.id)
              }}
              onPointerDown={(e) => e.stopPropagation()}
              className="opacity-100 md:opacity-0 md:group-hover:opacity-100 shrink-0 p-1 text-green-400 hover:text-green-300 hover:bg-green-500/10 rounded transition-all"
              title="Resume task"
            >
              <Play className="w-3.5 h-3.5" />
            </button>
          )}
          {/* Transition button (visible on mobile when transitions available) */}
          {hasTransitions && onTransition && (
            <>
              <button
                ref={transitionBtnRef}
                onClick={(e) => {
                  e.stopPropagation()
                  setShowTransitionMenu(!showTransitionMenu)
                }}
                onPointerDown={(e) => e.stopPropagation()}
                className="md:hidden p-1.5 text-cyan-400 bg-cyan-500/10 border border-cyan-500/20 hover:bg-cyan-500/20 rounded transition-all"
                title="Move to status"
              >
                <ArrowRight className="w-3.5 h-3.5" />
              </button>
              {/* Transition dropdown (rendered via portal by Floating UI) */}
              <DropdownMenu
                open={showTransitionMenu}
                onOpenChange={setShowTransitionMenu}
                align="right"
                triggerRef={transitionBtnRef}
              >
                <p className="px-3 py-1.5 text-[10px] text-gray-500 uppercase tracking-wider">Move to</p>
                {transitionTargets.map((target) => {
                  // Disable force targets when agent is running
                  const disabled = target.isForce && isAgentRunning
                  return (
                    <DropdownMenu.Item
                      key={target.id}
                      onClick={() => {
                        onTransition(task.id, target.id, target.isForce)
                        setShowTransitionMenu(false)
                      }}
                      disabled={disabled}
                      variant={target.isForce ? 'warning' : 'default'}
                      className={disabled ? '' : ''}
                    >
                      {target.isForce ? (
                        <AlertTriangle className="w-3 h-3 text-amber-500/70" />
                      ) : (
                        <ArrowRight className="w-3 h-3 text-cyan-400" />
                      )}
                      {target.name}
                    </DropdownMenu.Item>
                  )
                })}
              </DropdownMenu>
            </>
          )}
          {/* Create subtask button (always visible on mobile, hover on desktop) */}
          {onCreateChild && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onCreateChild(task.id)
              }}
              onPointerDown={(e) => e.stopPropagation()}
              className="opacity-100 md:opacity-0 md:group-hover:opacity-100 shrink-0 p-1 text-gray-500 hover:text-cyan-400 hover:bg-slate-700 rounded transition-all"
              title="Create subtask"
            >
              <CopyPlus className="w-3.5 h-3.5" />
            </button>
          )}
          {/* Edit button (always visible on mobile, hover on desktop) */}
          {onEdit && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onEdit(task.id)
              }}
              onPointerDown={(e) => e.stopPropagation()}
              className="opacity-100 md:opacity-0 md:group-hover:opacity-100 shrink-0 p-1 text-gray-500 hover:text-white hover:bg-slate-700 rounded transition-all"
              title="Edit task"
            >
              <Pencil className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
      </div>
      {task.description && (
        <p className="text-xs text-gray-400 mt-1 line-clamp-2">
          {task.description}
        </p>
      )}

      {/* Parent task link */}
      {parentTaskTitle && (
        <div className="mt-1.5 flex items-center gap-1 text-[11px] text-gray-500 truncate">
          <ArrowUpRight className="w-3 h-3 shrink-0 text-gray-600" />
          <span className="truncate">{parentTaskTitle}</span>
        </div>
      )}

      {/* Claude mode indicator */}
      {task.metadata?.['claude_mode'] && (
        <div className="mt-1.5 flex items-center gap-1 text-xs text-gray-500 truncate">
          <Play className="w-3 h-3 shrink-0" />
          <span className="truncate font-mono">{task.metadata['claude_mode']}</span>
        </div>
      )}

      {/* Worktree indicator */}
      {task.useWorktree && (
        <div className="mt-1.5 flex items-center gap-1 text-xs text-gray-500 truncate">
          <GitBranch className="w-3 h-3 shrink-0" />
          <span className="truncate font-mono">{task.metadata?.['worktree'] || 'worktree'}</span>
        </div>
      )}

      {/* Assignment status + child count + ID */}
      <div className="mt-2 flex items-end justify-between">
        <div className="flex items-center gap-1.5">
          {task.assignmentStatus === TaskAssignmentStatus.ASSIGNED ? (
            <Badge color="cyan" variant="outline" pill icon={<Bot className="w-3 h-3" />}>
              {shortId(task.assignedAgentId)}
            </Badge>
          ) : task.assignmentStatus === TaskAssignmentStatus.PENDING ? (
            <Badge color="yellow" variant="outline" pill icon={<Loader className="w-3 h-3" />}>
              Pending
            </Badge>
          ) : (
            <span className="inline-flex items-center gap-1 text-xs text-gray-500">
              <Clock className="w-3 h-3" />
              Unassigned
            </span>
          )}
          {/* Child task count badge */}
          {childCount != null && childCount > 0 && (
            <Badge color="purple" size="xs" variant="outline" pill icon={<Layers className="w-2.5 h-2.5" />}>
              {childCount}
            </Badge>
          )}
        </div>
        <span className="text-[10px] text-gray-600 font-mono">{shortId(task.id)}</span>
      </div>
    </div>
  )
}
