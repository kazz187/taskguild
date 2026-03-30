import { useState } from 'react'
import { X, Save } from 'lucide-react'
import { Button, Input, Textarea, Select, Badge, MutationError } from '../atoms/index.ts'
import { Card, FormField } from '../molecules/index.ts'
import { AVAILABLE_TOOLS, MODEL_OPTIONS, PERMISSION_MODE_OPTIONS, MEMORY_OPTIONS } from '@/lib/constants.ts'
import { toggleArrayItem } from '@/lib/arrays.ts'
import type { AgentFormData } from './AgentListUtils.ts'

export function AgentFormModal({ formMode, form, setForm, onSubmit, onClose, isPending, error }: {
  formMode: 'create' | 'edit'
  form: AgentFormData
  setForm: React.Dispatch<React.SetStateAction<AgentFormData>>
  onSubmit: (e: React.FormEvent) => void
  onClose: () => void
  isPending: boolean
  error?: Error | null
}) {
  const [skillInput, setSkillInput] = useState('')

  const toggleTool = (tool: string) => {
    setForm(prev => ({ ...prev, tools: toggleArrayItem(prev.tools, tool) }))
  }

  const toggleDisallowedTool = (tool: string) => {
    setForm(prev => ({ ...prev, disallowedTools: toggleArrayItem(prev.disallowedTools, tool) }))
  }

  const addSkill = () => {
    const trimmed = skillInput.trim()
    if (trimmed && !form.skills.includes(trimmed)) {
      setForm(prev => ({ ...prev, skills: [...prev.skills, trimmed] }))
      setSkillInput('')
    }
  }

  const removeSkill = (skill: string) => {
    setForm(prev => ({ ...prev, skills: prev.skills.filter(s => s !== skill) }))
  }

  return (
    <form onSubmit={onSubmit} className="mb-4 md:mb-6">
      <Card className="md:p-5">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold text-white">
            {formMode === 'create' ? 'New Agent' : 'Edit Agent'}
          </h3>
          <button type="button" onClick={onClose} className="text-gray-500 hover:text-gray-300 transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="space-y-4">
          {/* Name & Description row */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <FormField label="Name *" hint="Lowercase with hyphens. Used as filename in .claude/agents/">
              <Input
                type="text"
                required
                value={form.name}
                onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
                placeholder="e.g. code-reviewer"
              />
            </FormField>
            <FormField label="Description *" hint="Claude uses this to decide when to delegate">
              <Input
                type="text"
                required
                value={form.description}
                onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))}
                placeholder="When to delegate to this agent"
              />
            </FormField>
          </div>

          {/* System Prompt */}
          <FormField label="System Prompt *" hint="Body of the .md file. This becomes the agent's system prompt.">
            <Textarea
              required
              value={form.prompt}
              onChange={e => setForm(prev => ({ ...prev, prompt: e.target.value }))}
              mono
              placeholder="You are a code reviewer. Analyze code and provide specific, actionable feedback..."
            />
          </FormField>

          {/* Allowed Tools */}
          <FormField label="Allowed Tools" hint="Leave empty to inherit all tools from parent">
            <div className="flex flex-wrap gap-1.5">
              {AVAILABLE_TOOLS.map(tool => (
                <button
                  key={tool}
                  type="button"
                  onClick={() => toggleTool(tool)}
                  className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                    form.tools.includes(tool)
                      ? 'bg-cyan-500/20 text-cyan-400 border border-cyan-500/30'
                      : 'bg-slate-800 text-gray-500 border border-slate-700 hover:text-gray-300'
                  }`}
                >
                  {tool}
                </button>
              ))}
            </div>
          </FormField>

          {/* Disallowed Tools */}
          <FormField label="Disallowed Tools" hint="Tools to deny, removed from inherited or specified list">
            <div className="flex flex-wrap gap-1.5">
              {AVAILABLE_TOOLS.map(tool => (
                <button
                  key={tool}
                  type="button"
                  onClick={() => toggleDisallowedTool(tool)}
                  className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                    form.disallowedTools.includes(tool)
                      ? 'bg-red-500/20 text-red-400 border border-red-500/30'
                      : 'bg-slate-800 text-gray-500 border border-slate-700 hover:text-gray-300'
                  }`}
                >
                  {tool}
                </button>
              ))}
            </div>
          </FormField>

          {/* Model & Permission Mode row */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <FormField label="Model">
              <Select
                selectSize="xs"
                value={form.model}
                onChange={e => setForm(prev => ({ ...prev, model: e.target.value }))}
                className="rounded"
              >
                {MODEL_OPTIONS.map(opt => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </Select>
            </FormField>
            <FormField label="Permission Mode">
              <Select
                selectSize="xs"
                value={form.permissionMode}
                onChange={e => setForm(prev => ({ ...prev, permissionMode: e.target.value }))}
                className="rounded"
              >
                {PERMISSION_MODE_OPTIONS.map(opt => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </Select>
            </FormField>
            <FormField label="Memory">
              <Select
                selectSize="xs"
                value={form.memory}
                onChange={e => setForm(prev => ({ ...prev, memory: e.target.value }))}
                className="rounded"
              >
                {MEMORY_OPTIONS.map(opt => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </Select>
            </FormField>
          </div>

          {/* Skills */}
          <FormField label="Skills" hint="Skills to preload into the agent's context at startup">
            <div className="flex gap-2">
              <Input
                inputSize="sm"
                value={skillInput}
                onChange={e => setSkillInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addSkill() } }}
                placeholder="e.g. api-conventions"
                className="flex-1"
              />
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={addSkill}
                className="border border-slate-700 hover:border-slate-600"
              >
                Add
              </Button>
            </div>
            {form.skills.length > 0 && (
              <div className="flex flex-wrap gap-1.5 mt-2">
                {form.skills.map(skill => (
                  <Badge
                    key={skill}
                    color="purple"
                    size="sm"
                    variant="outline"
                    className="border-purple-500/30 bg-purple-500/20"
                  >
                    {skill}
                    <button type="button" onClick={() => removeSkill(skill)} className="hover:text-purple-200">
                      <X className="w-3 h-3" />
                    </button>
                  </Badge>
                ))}
              </div>
            )}
          </FormField>
        </div>

        <MutationError error={error} />

        <div className="flex justify-end gap-2 mt-4">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            variant="primary"
            size="sm"
            icon={<Save className="w-3.5 h-3.5" />}
            disabled={isPending || !form.name || !form.description || !form.prompt}
          >
            {isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
          </Button>
        </div>
      </Card>
    </form>
  )
}
