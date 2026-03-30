import type { ReactNode } from 'react'
import { useMutation } from '@connectrpc/connect-query'
import { saveAsTemplate } from '@taskguild/proto/taskguild/v1/template-TemplateService_connectquery.ts'
import { Save } from 'lucide-react'
import { Button, Input } from '../atoms/index.ts'
import { MutationError } from '../atoms/MutationError.tsx'
import { Modal, FormField } from '../molecules/index.ts'

interface SaveDialogState {
  entityId: string
  name: string
  description: string
}

export interface SaveAsTemplateDialogProps {
  dialog: SaveDialogState | null
  setDialog: React.Dispatch<React.SetStateAction<SaveDialogState | null>>
  entityType: 'agent' | 'skill' | 'script'
  onSaved: () => void
  extraMutationParams?: Record<string, unknown>
  extraFields?: ReactNode
}

export function SaveAsTemplateDialog({
  dialog,
  setDialog,
  entityType,
  onSaved,
  extraMutationParams,
  extraFields,
}: SaveAsTemplateDialogProps) {
  const saveTemplateMut = useMutation(saveAsTemplate)

  const handleSubmit = () => {
    if (!dialog) return
    saveTemplateMut.mutate(
      {
        entityType,
        entityId: dialog.entityId,
        templateName: dialog.name,
        templateDescription: dialog.description,
        ...extraMutationParams,
      },
      {
        onSuccess: () => {
          setDialog(null)
          onSaved()
        },
      },
    )
  }

  const handleClose = () => setDialog(null)

  return (
    <Modal open={!!dialog} onClose={handleClose} size="sm">
      <Modal.Header onClose={handleClose}>
        <h3 className="text-lg font-semibold text-white">Save as Template</h3>
      </Modal.Header>
      <Modal.Body>
        <FormField label="Template Name">
          <Input
            type="text"
            value={dialog?.name ?? ''}
            onChange={e => setDialog(prev => prev ? { ...prev, name: e.target.value } : null)}
            className="focus:border-amber-500"
          />
        </FormField>
        <FormField label="Template Description">
          <Input
            type="text"
            value={dialog?.description ?? ''}
            onChange={e => setDialog(prev => prev ? { ...prev, description: e.target.value } : null)}
            className="focus:border-amber-500"
          />
        </FormField>
        {extraFields}
        <MutationError error={saveTemplateMut.error} />
        {saveTemplateMut.isSuccess && (
          <p className="text-green-400 text-sm mt-3">Template saved successfully!</p>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={handleClose}>Cancel</Button>
        <Button
          variant="danger"
          size="sm"
          onClick={handleSubmit}
          disabled={saveTemplateMut.isPending || !dialog?.name}
          icon={<Save className="w-3.5 h-3.5" />}
        >
          {saveTemplateMut.isPending ? 'Saving...' : 'Save Template'}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
