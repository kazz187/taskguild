import { useDraggable } from '@dnd-kit/core'
import { useNavigate } from '@tanstack/react-router'
import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { Bot, Clock, Loader, Pencil } from 'lucide-react'

interface TaskCardProps {
  task: Task
  onEdit?: (taskId: string) => void
  isDragOverlay?: boolean
}

export function TaskCard({ task, onEdit, isDragOverlay }: TaskCardProps) {
  const navigate = useNavigate()
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: task.id,
    data: { task },
  })

  const handleClick = () => {
    if (isDragging) return
    navigate({ to: '/projects/$projectId/tasks/$taskId', params: { projectId: task.projectId, taskId: task.id } })
  }

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
        {onEdit && (
          <button
            onClick={(e) => {
              e.stopPropagation()
              onEdit(task.id)
            }}
            onPointerDown={(e) => e.stopPropagation()}
            className="opacity-0 group-hover:opacity-100 shrink-0 p-1 text-gray-500 hover:text-white hover:bg-slate-700 rounded transition-all"
            title="Edit task"
          >
            <Pencil className="w-3.5 h-3.5" />
          </button>
        )}
      </div>
      {task.description && (
        <p className="text-xs text-gray-400 mt-1 line-clamp-2">
          {task.description}
        </p>
      )}

      {/* Assignment status + ID */}
      <div className="mt-2 flex items-end justify-between">
        {task.assignmentStatus === TaskAssignmentStatus.ASSIGNED ? (
          <span className="inline-flex items-center gap-1 text-xs bg-cyan-500/10 text-cyan-400 border border-cyan-500/20 rounded-full px-2 py-0.5">
            <Bot className="w-3 h-3" />
            {task.assignedAgentId.slice(0, 8)}
          </span>
        ) : task.assignmentStatus === TaskAssignmentStatus.PENDING ? (
          <span className="inline-flex items-center gap-1 text-xs bg-yellow-500/10 text-yellow-400 border border-yellow-500/20 rounded-full px-2 py-0.5">
            <Loader className="w-3 h-3" />
            Pending
          </span>
        ) : (
          <span className="inline-flex items-center gap-1 text-xs text-gray-500">
            <Clock className="w-3 h-3" />
            Unassigned
          </span>
        )}
        <span className="text-[10px] text-gray-600 font-mono">{task.id.slice(0, 12)}</span>
      </div>
    </div>
  )
}
