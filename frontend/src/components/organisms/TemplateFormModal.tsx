import type { Dispatch, FormEvent, SetStateAction } from 'react'
import { Save } from 'lucide-react'
import { Button, Input, MutationError } from '../atoms/index.ts'
import { Modal, FormField } from '../molecules/index.ts'
import { toggleArrayItem } from '@/lib/arrays.ts'
import type { EntityType, TemplateFormData } from './TemplateListTypes.ts'
import { AgentConfigForm, SkillConfigForm, ScriptConfigForm } from './TemplateConfigForms.tsx'

const ENTITY_LABEL: Record<EntityType, string> = {
  agent: 'Agent',
  skill: 'Skill',
  script: 'Script',
}

export interface TemplateFormModalProps {
  formMode: 'create' | 'edit'
  entityType: EntityType
  form: TemplateFormData
  setForm: Dispatch<SetStateAction<TemplateFormData>>
  skillInput: string
  setSkillInput: Dispatch<SetStateAction<string>>
  onSubmit: (e: FormEvent) => void
  onClose: () => void
  isPending: boolean
  error?: Error | null
}

export function TemplateFormModal({
  formMode,
  entityType,
  form,
  setForm,
  skillInput,
  setSkillInput,
  onSubmit,
  onClose,
  isPending,
  error,
}: TemplateFormModalProps) {
  const toggleTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: { ...prev.agentConfig, tools: toggleArrayItem(prev.agentConfig.tools, tool) },
    }))
  }

  const toggleDisallowedTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: { ...prev.agentConfig, disallowedTools: toggleArrayItem(prev.agentConfig.disallowedTools, tool) },
    }))
  }

  const addSkill = () => {
    const trimmed = skillInput.trim()
    if (trimmed && !form.agentConfig.skills.includes(trimmed)) {
      setForm(prev => ({
        ...prev,
        agentConfig: { ...prev.agentConfig, skills: [...prev.agentConfig.skills, trimmed] },
      }))
      setSkillInput('')
    }
  }

  const removeSkill = (skill: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: { ...prev.agentConfig, skills: prev.agentConfig.skills.filter(s => s !== skill) },
    }))
  }

  const toggleAllowedTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      skillConfig: { ...prev.skillConfig, allowedTools: toggleArrayItem(prev.skillConfig.allowedTools, tool) },
    }))
  }

  return (
    <Modal open={true} onClose={onClose} size="lg">
      <Modal.Header onClose={onClose}>
        <h3 className="text-lg font-semibold text-white">
          {formMode === 'create' ? 'New' : 'Edit'} {ENTITY_LABEL[entityType]} Template
        </h3>
      </Modal.Header>
      <form onSubmit={onSubmit}>
        <Modal.Body>
          <div className="space-y-4">
            {/* Template Name & Description */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <FormField label="Template Name *">
                <Input
                  type="text"
                  required
                  value={form.templateName}
                  onChange={e => setForm(prev => ({ ...prev, templateName: e.target.value }))}
                  className="focus:border-amber-500"
                  placeholder="e.g. My Code Reviewer Template"
                />
              </FormField>
              <FormField label="Template Description">
                <Input
                  type="text"
                  value={form.templateDescription}
                  onChange={e => setForm(prev => ({ ...prev, templateDescription: e.target.value }))}
                  className="focus:border-amber-500"
                  placeholder="Description of this template"
                />
              </FormField>
            </div>

            <div className="border-t border-slate-800 pt-4">
              <p className="text-xs text-gray-500 mb-3">Configuration</p>
            </div>

            {/* Entity-specific config form */}
            {entityType === 'agent' && <AgentConfigForm form={form} setForm={setForm} skillInput={skillInput} setSkillInput={setSkillInput} toggleTool={toggleTool} toggleDisallowedTool={toggleDisallowedTool} addSkill={addSkill} removeSkill={removeSkill} />}
            {entityType === 'skill' && <SkillConfigForm form={form} setForm={setForm} toggleAllowedTool={toggleAllowedTool} />}
            {entityType === 'script' && <ScriptConfigForm form={form} setForm={setForm} />}
          </div>
          <MutationError error={error} />
        </Modal.Body>
        <Modal.Footer>
          <Button type="button" variant="secondary" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            variant="danger"
            size="sm"
            disabled={isPending || !form.templateName}
            icon={<Save className="w-3.5 h-3.5" />}
          >
            {isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
          </Button>
        </Modal.Footer>
      </form>
    </Modal>
  )
}
