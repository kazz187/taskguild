import { Input, Textarea, Select, Checkbox } from '../atoms/index.ts'
import { FormField, EntityFormModal } from '../molecules/index.ts'
import { AVAILABLE_TOOLS, MODEL_OPTIONS, CONTEXT_OPTIONS, AGENT_OPTIONS } from '@/lib/constants.ts'
import { toggleArrayItem } from '@/lib/arrays.ts'
import type { SkillFormData } from './SkillListUtils.ts'

export function SkillFormModal({ formMode, form, setForm, onSubmit, onClose, isPending, error }: {
  formMode: 'create' | 'edit'
  form: SkillFormData
  setForm: React.Dispatch<React.SetStateAction<SkillFormData>>
  onSubmit: (e: React.FormEvent) => void
  onClose: () => void
  isPending: boolean
  error?: Error | null
}) {
  const toggleAllowedTool = (tool: string) => {
    setForm(prev => ({ ...prev, allowedTools: toggleArrayItem(prev.allowedTools, tool) }))
  }

  return (
    <EntityFormModal
      entityName="Skill"
      formMode={formMode}
      onSubmit={onSubmit}
      onClose={onClose}
      isPending={isPending}
      error={error}
      submitDisabled={!form.name || !form.content}
      submitClassName="bg-purple-600 hover:bg-purple-500"
    >
      {/* Name & Description row */}
      <div className="grid grid-cols-2 gap-3">
        <FormField label="Name *" hint="Lowercase with hyphens. Used as directory name in .claude/skills/">
          <Input
            type="text"
            required
            value={form.name}
            onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
            className="focus:border-purple-500"
            placeholder="e.g. explain-code"
          />
        </FormField>
        <FormField label="Description" hint="Claude uses this to decide when to load this skill">
          <Input
            type="text"
            value={form.description}
            onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))}
            className="focus:border-purple-500"
            placeholder="When to use this skill"
          />
        </FormField>
      </div>

      {/* Argument Hint */}
      <FormField label="Argument Hint" hint="Hint shown in autocomplete for expected arguments">
        <Input
          type="text"
          value={form.argumentHint}
          onChange={e => setForm(prev => ({ ...prev, argumentHint: e.target.value }))}
          className="focus:border-purple-500"
          placeholder="e.g. [issue-number] or [filename] [format]"
        />
      </FormField>

      {/* Content */}
      <FormField label="Content *" hint="Body of the SKILL.md file. Instructions Claude follows when this skill is invoked.">
        <Textarea
          required
          value={form.content}
          onChange={e => setForm(prev => ({ ...prev, content: e.target.value }))}
          mono
          className="focus:border-purple-500 min-h-[150px]"
          placeholder={"When explaining code, always include:\n1. Start with an analogy\n2. Draw a diagram using ASCII art\n3. Walk through the code step-by-step"}
        />
      </FormField>

      {/* Invocation Control */}
      <div>
        <label className="block text-xs text-gray-400 mb-2">Invocation Control</label>
        <div className="flex gap-6">
          <Checkbox
            label="Disable model invocation"
            color="purple"
            checked={form.disableModelInvocation}
            onChange={e => setForm(prev => ({ ...prev, disableModelInvocation: e.target.checked }))}
          />
          <Checkbox
            label="User invocable"
            color="purple"
            checked={form.userInvocable}
            onChange={e => setForm(prev => ({ ...prev, userInvocable: e.target.checked }))}
          />
        </div>
        <p className="text-[10px] text-gray-600 mt-1">
          "Disable model invocation" prevents Claude from auto-loading. "User invocable" controls /slash-command visibility.
        </p>
      </div>

      {/* Allowed Tools */}
      <div>
        <label className="block text-xs text-gray-400 mb-1">Allowed Tools</label>
        <div className="flex flex-wrap gap-1.5">
          {AVAILABLE_TOOLS.map(tool => (
            <button
              key={tool}
              type="button"
              onClick={() => toggleAllowedTool(tool)}
              className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                form.allowedTools.includes(tool)
                  ? 'bg-purple-500/20 text-purple-400 border border-purple-500/30'
                  : 'bg-slate-800 text-gray-500 border border-slate-700 hover:text-gray-300'
              }`}
            >
              {tool}
            </button>
          ))}
        </div>
        <p className="text-[10px] text-gray-600 mt-1">Tools Claude can use without asking when this skill is active. Leave empty for default.</p>
      </div>

      {/* Model, Context & Agent row */}
      <div className="grid grid-cols-3 gap-3">
        <FormField label="Model">
          <Select
            value={form.model}
            onChange={e => setForm(prev => ({ ...prev, model: e.target.value }))}
            selectSize="xs"
            className="rounded focus:border-purple-500"
          >
            {MODEL_OPTIONS.map(opt => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </Select>
        </FormField>
        <FormField label="Context">
          <Select
            value={form.context}
            onChange={e => setForm(prev => ({ ...prev, context: e.target.value }))}
            selectSize="xs"
            className="rounded focus:border-purple-500"
          >
            {CONTEXT_OPTIONS.map(opt => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </Select>
        </FormField>
        <FormField label="Agent" hint='Only used when context is "fork"'>
          <Select
            value={form.agent}
            onChange={e => setForm(prev => ({ ...prev, agent: e.target.value }))}
            disabled={form.context !== 'fork'}
            selectSize="xs"
            className="rounded focus:border-purple-500 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {AGENT_OPTIONS.map(opt => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </Select>
        </FormField>
      </div>
    </EntityFormModal>
  )
}
