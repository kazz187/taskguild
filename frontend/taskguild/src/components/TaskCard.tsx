import type { Task } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { Bot, Clock } from 'lucide-react'

interface TaskCardProps {
  task: Task
}

export function TaskCard({ task }: TaskCardProps) {
  const hasAgent = !!task.assignedAgentId

  return (
    <div className="bg-slate-800 border border-slate-700 rounded-lg p-3 hover:border-slate-600 transition-colors">
      <h4 className="text-sm font-medium text-white leading-snug">
        {task.title}
      </h4>
      {task.description && (
        <p className="text-xs text-gray-400 mt-1 line-clamp-2">
          {task.description}
        </p>
      )}

      {/* Agent assignment badge */}
      <div className="mt-2">
        {hasAgent ? (
          <span className="inline-flex items-center gap-1 text-xs bg-cyan-500/10 text-cyan-400 border border-cyan-500/20 rounded-full px-2 py-0.5">
            <Bot className="w-3 h-3" />
            {task.assignedAgentId.slice(0, 8)}
          </span>
        ) : (
          <span className="inline-flex items-center gap-1 text-xs text-gray-500">
            <Clock className="w-3 h-3" />
            Unassigned
          </span>
        )}
      </div>

      {/* Task ID */}
      <p className="text-[10px] text-gray-600 mt-2 font-mono">
        {task.id.slice(0, 12)}
      </p>
    </div>
  )
}
