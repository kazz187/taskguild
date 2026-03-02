import { AlertTriangle } from 'lucide-react'

interface ForceTransitionDialogProps {
  fromStatusName: string
  toStatusName: string
  onConfirm: () => void
  onCancel: () => void
}

export function ForceTransitionDialog({
  fromStatusName,
  toStatusName,
  onConfirm,
  onCancel,
}: ForceTransitionDialogProps) {
  return (
    <div
      className="fixed inset-0 z-[60] bg-black/60 flex items-center justify-center p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCancel()
      }}
    >
      <div className="bg-slate-900 border border-slate-700 rounded-xl w-full max-w-sm shadow-2xl">
        {/* Header */}
        <div className="flex items-center gap-2 px-5 pt-5 pb-2">
          <AlertTriangle className="w-5 h-5 text-amber-400 shrink-0" />
          <h3 className="text-base font-semibold text-white">Force status change</h3>
        </div>

        {/* Body */}
        <div className="px-5 pb-4">
          <p className="text-sm text-gray-300 leading-relaxed">
            The transition from{' '}
            <span className="font-semibold text-white">&ldquo;{fromStatusName}&rdquo;</span>
            {' '}to{' '}
            <span className="font-semibold text-white">&ldquo;{toStatusName}&rdquo;</span>
            {' '}is not defined in the workflow.
          </p>
          <p className="text-sm text-gray-400 mt-2">
            Do you want to force-move this task?
          </p>
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-2 px-5 pb-5">
          <button
            onClick={onCancel}
            className="px-4 py-2 text-sm text-gray-300 bg-slate-700 hover:bg-slate-600 rounded-lg transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="px-4 py-2 text-sm text-white bg-amber-600 hover:bg-amber-500 rounded-lg transition-colors font-medium"
          >
            Force move
          </button>
        </div>
      </div>
    </div>
  )
}
