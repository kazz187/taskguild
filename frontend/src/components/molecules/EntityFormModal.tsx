import type { ReactNode, FormEvent } from 'react'
import { Save } from 'lucide-react'
import { Button, MutationError } from '../atoms/index.ts'
import { Modal } from './Modal.tsx'

export interface EntityFormModalProps {
  /** Entity type name for the header ("Agent", "Skill", "Script") */
  entityName: string
  /** Whether creating or editing */
  formMode: 'create' | 'edit'
  /** Form submit handler */
  onSubmit: (e: FormEvent) => void
  /** Close handler for modal, Cancel button, and X button */
  onClose: () => void
  /** Whether a mutation is in flight */
  isPending: boolean
  /** Mutation error to display */
  error?: Error | null
  /** Whether the submit button should be disabled (beyond isPending) */
  submitDisabled?: boolean
  /** Custom className for the submit button (entity color theme) */
  submitClassName?: string
  /** Form field content rendered inside Modal.Body */
  children: ReactNode
}

export function EntityFormModal({
  entityName,
  formMode,
  onSubmit,
  onClose,
  isPending,
  error,
  submitDisabled = false,
  submitClassName,
  children,
}: EntityFormModalProps) {
  return (
    <Modal open={true} onClose={onClose} size="lg">
      <Modal.Header onClose={onClose}>
        <h3 className="text-lg font-semibold text-white">
          {formMode === 'create' ? `New ${entityName}` : `Edit ${entityName}`}
        </h3>
      </Modal.Header>
      <form onSubmit={onSubmit}>
        <Modal.Body>
          <div className="space-y-4">
            {children}
          </div>
          <MutationError error={error} />
        </Modal.Body>
        <Modal.Footer>
          <Button type="button" variant="secondary" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            variant="primary"
            size="sm"
            icon={<Save className="w-3.5 h-3.5" />}
            disabled={isPending || submitDisabled}
            className={submitClassName}
          >
            {isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
          </Button>
        </Modal.Footer>
      </form>
    </Modal>
  )
}
