import { useState } from 'react'
import type { TaskLog } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import { MarkdownDescription } from './MarkdownDescription.tsx'
import { ChevronDown, ChevronRight } from 'lucide-react'

interface DescriptionHistoryProps {
  /** Description RESULT logs sorted newest-first */
  versions: TaskLog[]
  currentDescription: string
  taskId: string
  onClose: () => void
}

export function DescriptionHistory({ versions, currentDescription, taskId, onClose }: DescriptionHistoryProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs text-gray-400">
          {versions.length} previous version{versions.length !== 1 ? 's' : ''}
        </span>
        <button
          onClick={onClose}
          className="text-[10px] text-gray-500 hover:text-gray-300 transition-colors"
        >
          Hide history
        </button>
      </div>

      {/* Current version */}
      <div className="border-l-2 border-cyan-500/30 pl-3 py-1">
        <span className="text-[10px] font-medium uppercase text-cyan-400">Current</span>
        <MarkdownDescription content={currentDescription} className="text-sm text-gray-300 mt-1" taskId={taskId} />
      </div>

      {/* Historical versions */}
      {versions.map((log) => {
        const fullText = log.metadata['full_text'] ?? log.message
        const isExpanded = expandedId === log.id
        const ts = log.createdAt
          ? new Date(Number(log.createdAt.seconds) * 1000).toLocaleString()
          : ''

        return (
          <div key={log.id} className="border-l-2 border-slate-700 pl-3 py-1">
            <button
              onClick={() => setExpandedId(isExpanded ? null : log.id)}
              className="flex items-center gap-1 text-[10px] text-gray-500 hover:text-gray-300 transition-colors"
            >
              {isExpanded
                ? <ChevronDown className="w-3 h-3" />
                : <ChevronRight className="w-3 h-3" />
              }
              <span>{ts}</span>
              {log.metadata['source'] && (
                <span className="text-gray-600">({log.metadata['source']})</span>
              )}
            </button>
            {isExpanded && (
              <MarkdownDescription content={fullText} className="text-sm text-gray-400 mt-1" taskId={taskId} />
            )}
          </div>
        )
      })}
    </div>
  )
}
