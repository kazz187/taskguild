import { useState } from 'react'
import { useMutation } from '@connectrpc/connect-query'
import { createTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { X } from 'lucide-react'

interface TaskCreateModalProps {
  projectId: string
  workflowId: string
  onCreated: () => void
  onClose: () => void
}

export function TaskCreateModal({ projectId, workflowId, onCreated, onClose }: TaskCreateModalProps) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const createMut = useMutation(createTask)

  const handleCreate = () => {
    if (!title.trim()) return
    createMut.mutate(
      { projectId, workflowId, title: title.trim(), description, metadata: {} },
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
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="bg-slate-900 border border-slate-700 rounded-xl w-full max-w-2xl max-h-[85vh] flex flex-col shadow-2xl">
        {/* Header */}
        <div className="flex items-start justify-between px-4 pt-4 pb-1">
          <div className="flex-1 min-w-0 mr-3">
            <input
              autoFocus
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) handleCreate() }}
              className="w-full px-2 py-1 bg-slate-800 border border-slate-600 rounded text-white text-lg font-semibold focus:outline-none focus:border-cyan-500"
              placeholder="Task title..."
            />
          </div>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-300 transition-colors shrink-0 mt-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500 min-h-[200px]"
            placeholder="Add description..."
          />
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
