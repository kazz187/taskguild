import { AlertTriangle } from 'lucide-react'
import { Button } from '../atoms/index.ts'
import { Modal } from '../molecules/index.ts'

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
    <Modal open={true} onClose={onCancel} size="sm" zIndex={60}>
      <Modal.Header onClose={onCancel}>
        <AlertTriangle className="w-5 h-5 text-amber-400 shrink-0" />
        <h3 className="text-base font-semibold text-white">Force status change</h3>
      </Modal.Header>

      <Modal.Body>
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
      </Modal.Body>

      <Modal.Footer>
        <Button variant="secondary" size="md" onClick={onCancel}>
          Cancel
        </Button>
        <Button variant="danger" size="md" onClick={onConfirm}>
          Force move
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
