import { Input, Textarea } from '../atoms/index.ts'
import { FormField, EntityFormModal } from '../molecules/index.ts'
import type { ScriptFormData } from './ScriptListUtils.ts'

export function ScriptFormModal({ formMode, form, setForm, onSubmit, onClose, isPending, error }: {
  formMode: 'create' | 'edit'
  form: ScriptFormData
  setForm: React.Dispatch<React.SetStateAction<ScriptFormData>>
  onSubmit: (e: React.FormEvent) => void
  onClose: () => void
  isPending: boolean
  error?: Error | null
}) {
  return (
    <EntityFormModal
      entityName="Script"
      formMode={formMode}
      onSubmit={onSubmit}
      onClose={onClose}
      isPending={isPending}
      error={error}
      submitDisabled={!form.name || !form.content}
      submitClassName="bg-green-600 hover:bg-green-500"
    >
      {/* Name & Description row */}
      <div className="grid grid-cols-2 gap-3">
        <FormField label="Name *" hint="Script name (used as identifier)">
          <Input
            type="text"
            required
            value={form.name}
            onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
            className="focus:border-green-500"
            placeholder="e.g. deploy"
          />
        </FormField>
        <FormField label="Description">
          <Input
            type="text"
            value={form.description}
            onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))}
            className="focus:border-green-500"
            placeholder="What this script does"
          />
        </FormField>
      </div>

      {/* Filename */}
      <FormField label="Filename" hint="Defaults to name.sh if empty">
        <Input
          type="text"
          value={form.filename}
          onChange={e => setForm(prev => ({ ...prev, filename: e.target.value }))}
          className="focus:border-green-500"
          placeholder={form.name ? `${form.name}.sh` : 'e.g. deploy.sh'}
        />
      </FormField>

      {/* Content */}
      <FormField label="Script Content *" hint="Shell script to execute on the agent-manager machine.">
        <Textarea
          required
          value={form.content}
          onChange={e => setForm(prev => ({ ...prev, content: e.target.value }))}
          mono
          className="focus:border-green-500 min-h-[200px]"
          placeholder={'#!/bin/bash\necho "Hello from script"'}
        />
      </FormField>
    </EntityFormModal>
  )
}
