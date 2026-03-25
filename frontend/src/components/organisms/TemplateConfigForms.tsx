import { X } from 'lucide-react'
import { Input, Textarea, Select, Checkbox, Badge, Button } from '../atoms/index.ts'
import { FormField } from '../molecules/index.ts'
import { AVAILABLE_TOOLS, MODEL_OPTIONS, PERMISSION_MODE_OPTIONS, MEMORY_OPTIONS } from './agentConstants.ts'
import { CONTEXT_OPTIONS, AGENT_OPTIONS } from './TemplateListTypes.ts'
import type { TemplateFormData, AgentFormData, SkillFormData, ScriptFormData } from './TemplateListTypes.ts'

// --- Agent Config Form ---

export function AgentConfigForm({ form, setForm, skillInput, setSkillInput, toggleTool, toggleDisallowedTool, addSkill, removeSkill }: {
  form: TemplateFormData
  setForm: React.Dispatch<React.SetStateAction<TemplateFormData>>
  skillInput: string
  setSkillInput: React.Dispatch<React.SetStateAction<string>>
  toggleTool: (tool: string) => void
  toggleDisallowedTool: (tool: string) => void
  addSkill: () => void
  removeSkill: (skill: string) => void
}) {
  const cfg = form.agentConfig
  const setCfg = (update: Partial<AgentFormData>) => setForm(prev => ({ ...prev, agentConfig: { ...prev.agentConfig, ...update } }))

  return (
    <>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <FormField label="Agent Name *">
          <Input type="text" required value={cfg.name} onChange={e => setCfg({ name: e.target.value })}
            className="focus:border-cyan-500"
            placeholder="e.g. code-reviewer" />
        </FormField>
        <FormField label="Agent Description">
          <Input type="text" value={cfg.description} onChange={e => setCfg({ description: e.target.value })}
            className="focus:border-cyan-500"
            placeholder="When to delegate to this agent" />
        </FormField>
      </div>
      <FormField label="System Prompt">
        <Textarea value={cfg.prompt} onChange={e => setCfg({ prompt: e.target.value })}
          mono
          textareaSize="sm"
          className="focus:border-cyan-500 min-h-[120px]"
          placeholder="You are a code reviewer..." />
      </FormField>
      <div>
        <label className="block text-xs text-gray-400 mb-1">Allowed Tools</label>
        <div className="flex flex-wrap gap-1.5">
          {AVAILABLE_TOOLS.map(tool => (
            <button key={tool} type="button" onClick={() => toggleTool(tool)}
              className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                cfg.tools.includes(tool)
                  ? 'bg-cyan-500/20 text-cyan-400 border border-cyan-500/30'
                  : 'bg-slate-800 text-gray-500 border border-slate-700 hover:text-gray-300'
              }`}>{tool}</button>
          ))}
        </div>
      </div>
      <div>
        <label className="block text-xs text-gray-400 mb-1">Disallowed Tools</label>
        <div className="flex flex-wrap gap-1.5">
          {AVAILABLE_TOOLS.map(tool => (
            <button key={tool} type="button" onClick={() => toggleDisallowedTool(tool)}
              className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                cfg.disallowedTools.includes(tool)
                  ? 'bg-red-500/20 text-red-400 border border-red-500/30'
                  : 'bg-slate-800 text-gray-500 border border-slate-700 hover:text-gray-300'
              }`}>{tool}</button>
          ))}
        </div>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <FormField label="Model">
          <Select value={cfg.model} onChange={e => setCfg({ model: e.target.value })}
            selectSize="xs"
            className="rounded focus:border-cyan-500">
            {MODEL_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
          </Select>
        </FormField>
        <FormField label="Permission Mode">
          <Select value={cfg.permissionMode} onChange={e => setCfg({ permissionMode: e.target.value })}
            selectSize="xs"
            className="rounded focus:border-cyan-500">
            {PERMISSION_MODE_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
          </Select>
        </FormField>
        <FormField label="Memory">
          <Select value={cfg.memory} onChange={e => setCfg({ memory: e.target.value })}
            selectSize="xs"
            className="rounded focus:border-cyan-500">
            {MEMORY_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
          </Select>
        </FormField>
      </div>
      <div>
        <label className="block text-xs text-gray-400 mb-1">Skills</label>
        <div className="flex gap-2">
          <Input type="text" value={skillInput} onChange={e => setSkillInput(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addSkill() } }}
            inputSize="sm"
            className="focus:border-cyan-500"
            placeholder="e.g. api-conventions" />
          <Button type="button" variant="ghost" size="sm" onClick={addSkill}
            className="border border-slate-700 hover:border-slate-600 shrink-0">
            Add
          </Button>
        </div>
        {cfg.skills.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mt-2">
            {cfg.skills.map(skill => (
              <Badge key={skill} color="purple" size="sm" variant="outline" className="flex items-center gap-1 rounded-lg">
                {skill}
                <button type="button" onClick={() => removeSkill(skill)} className="hover:text-purple-200">
                  <X className="w-3 h-3" />
                </button>
              </Badge>
            ))}
          </div>
        )}
      </div>
    </>
  )
}

// --- Skill Config Form ---

export function SkillConfigForm({ form, setForm, toggleAllowedTool }: {
  form: TemplateFormData
  setForm: React.Dispatch<React.SetStateAction<TemplateFormData>>
  toggleAllowedTool: (tool: string) => void
}) {
  const cfg = form.skillConfig
  const setCfg = (update: Partial<SkillFormData>) => setForm(prev => ({ ...prev, skillConfig: { ...prev.skillConfig, ...update } }))

  return (
    <>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <FormField label="Skill Name *">
          <Input type="text" required value={cfg.name} onChange={e => setCfg({ name: e.target.value })}
            className="focus:border-purple-500"
            placeholder="e.g. explain-code" />
        </FormField>
        <FormField label="Description">
          <Input type="text" value={cfg.description} onChange={e => setCfg({ description: e.target.value })}
            className="focus:border-purple-500"
            placeholder="When to use this skill" />
        </FormField>
      </div>
      <FormField label="Argument Hint">
        <Input type="text" value={cfg.argumentHint} onChange={e => setCfg({ argumentHint: e.target.value })}
          className="focus:border-purple-500"
          placeholder="e.g. [issue-number]" />
      </FormField>
      <FormField label="Content *">
        <Textarea required value={cfg.content} onChange={e => setCfg({ content: e.target.value })}
          mono
          textareaSize="sm"
          className="focus:border-purple-500 min-h-[120px]"
          placeholder="Instructions for this skill..." />
      </FormField>
      <div>
        <label className="block text-xs text-gray-400 mb-2">Invocation Control</label>
        <div className="flex gap-6">
          <Checkbox
            label="Disable model invocation"
            color="purple"
            checked={cfg.disableModelInvocation}
            onChange={e => setCfg({ disableModelInvocation: e.target.checked })}
          />
          <Checkbox
            label="User invocable"
            color="purple"
            checked={cfg.userInvocable}
            onChange={e => setCfg({ userInvocable: e.target.checked })}
          />
        </div>
      </div>
      <div>
        <label className="block text-xs text-gray-400 mb-1">Allowed Tools</label>
        <div className="flex flex-wrap gap-1.5">
          {AVAILABLE_TOOLS.map(tool => (
            <button key={tool} type="button" onClick={() => toggleAllowedTool(tool)}
              className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                cfg.allowedTools.includes(tool)
                  ? 'bg-purple-500/20 text-purple-400 border border-purple-500/30'
                  : 'bg-slate-800 text-gray-500 border border-slate-700 hover:text-gray-300'
              }`}>{tool}</button>
          ))}
        </div>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <FormField label="Model">
          <Select value={cfg.model} onChange={e => setCfg({ model: e.target.value })}
            selectSize="xs"
            className="rounded focus:border-purple-500">
            {MODEL_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
          </Select>
        </FormField>
        <FormField label="Context">
          <Select value={cfg.context} onChange={e => setCfg({ context: e.target.value })}
            selectSize="xs"
            className="rounded focus:border-purple-500">
            {CONTEXT_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
          </Select>
        </FormField>
        <FormField label="Agent">
          <Select value={cfg.agent} onChange={e => setCfg({ agent: e.target.value })} disabled={cfg.context !== 'fork'}
            selectSize="xs"
            className="rounded focus:border-purple-500 disabled:opacity-40 disabled:cursor-not-allowed">
            {AGENT_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
          </Select>
        </FormField>
      </div>
    </>
  )
}

// --- Script Config Form ---

export function ScriptConfigForm({ form, setForm }: {
  form: TemplateFormData
  setForm: React.Dispatch<React.SetStateAction<TemplateFormData>>
}) {
  const cfg = form.scriptConfig
  const setCfg = (update: Partial<ScriptFormData>) => setForm(prev => ({ ...prev, scriptConfig: { ...prev.scriptConfig, ...update } }))

  return (
    <>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <FormField label="Script Name *">
          <Input type="text" required value={cfg.name} onChange={e => setCfg({ name: e.target.value })}
            className="focus:border-green-500"
            placeholder="e.g. deploy" />
        </FormField>
        <FormField label="Filename">
          <Input type="text" value={cfg.filename} onChange={e => setCfg({ filename: e.target.value })}
            className="focus:border-green-500"
            placeholder="e.g. deploy.sh" />
        </FormField>
      </div>
      <FormField label="Description">
        <Input type="text" value={cfg.description} onChange={e => setCfg({ description: e.target.value })}
          className="focus:border-green-500"
          placeholder="What this script does" />
      </FormField>
      <FormField label="Content *">
        <Textarea required value={cfg.content} onChange={e => setCfg({ content: e.target.value })}
          mono
          className="focus:border-green-500 min-h-[150px]"
          placeholder={"#!/bin/bash\necho 'Hello world'"} />
      </FormField>
    </>
  )
}
