import { useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTemplates, createTemplate, updateTemplate, deleteTemplate } from '@taskguild/proto/taskguild/v1/template-TemplateService_connectquery.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { Layers, Plus, Trash2, Edit2, X, Save, Bot, Sparkles, Terminal } from 'lucide-react'
import { Button, Input, Textarea, Select, Checkbox, Badge } from '../atoms/index.ts'
import { Card, FormField } from '../molecules/index.ts'

const AVAILABLE_TOOLS = [
  'Read', 'Write', 'Edit', 'Glob', 'Grep', 'Bash',
  'WebSearch', 'WebFetch', 'Task', 'NotebookEdit',
]

const MODEL_OPTIONS = [
  { value: '', label: 'Inherit (default)' },
  { value: 'sonnet', label: 'Sonnet' },
  { value: 'opus', label: 'Opus' },
  { value: 'haiku', label: 'Haiku' },
]

const PERMISSION_MODE_OPTIONS = [
  { value: '', label: 'None (inherit)' },
  { value: 'default', label: 'Default (ask for permission)' },
  { value: 'acceptEdits', label: 'Accept Edits' },
  { value: 'dontAsk', label: "Don't Ask (auto-deny unpermitted)" },
  { value: 'bypassPermissions', label: 'Bypass Permissions' },
  { value: 'plan', label: 'Plan (read-only exploration)' },
]

const MEMORY_OPTIONS = [
  { value: '', label: 'None' },
  { value: 'user', label: 'User (~/.claude/agent-memory/)' },
  { value: 'project', label: 'Project (.claude/agent-memory/)' },
  { value: 'local', label: 'Local (.claude/agent-memory-local/)' },
]

const CONTEXT_OPTIONS = [
  { value: '', label: 'Inline (default)' },
  { value: 'fork', label: 'Fork (run in sub-agent)' },
]

const AGENT_OPTIONS = [
  { value: '', label: 'general-purpose (default)' },
  { value: 'Explore', label: 'Explore' },
  { value: 'Plan', label: 'Plan' },
  { value: 'general-purpose', label: 'General Purpose' },
]

type EntityType = 'agent' | 'skill' | 'script'

const TABS: { type: EntityType; label: string; icon: typeof Bot; color: string }[] = [
  { type: 'agent', label: 'Agents', icon: Bot, color: 'cyan' },
  { type: 'skill', label: 'Skills', icon: Sparkles, color: 'purple' },
  { type: 'script', label: 'Scripts', icon: Terminal, color: 'green' },
]

// --- Agent Form ---

interface AgentFormData {
  name: string
  description: string
  prompt: string
  tools: string[]
  disallowedTools: string[]
  model: string
  permissionMode: string
  skills: string[]
  memory: string
}

const emptyAgentForm: AgentFormData = {
  name: '', description: '', prompt: '', tools: [], disallowedTools: [],
  model: '', permissionMode: '', skills: [], memory: '',
}

// --- Skill Form ---

interface SkillFormData {
  name: string
  description: string
  content: string
  disableModelInvocation: boolean
  userInvocable: boolean
  allowedTools: string[]
  model: string
  context: string
  agent: string
  argumentHint: string
}

const emptySkillForm: SkillFormData = {
  name: '', description: '', content: '', disableModelInvocation: false,
  userInvocable: true, allowedTools: [], model: '', context: '', agent: '', argumentHint: '',
}

// --- Script Form ---

interface ScriptFormData {
  name: string
  description: string
  filename: string
  content: string
}

const emptyScriptForm: ScriptFormData = {
  name: '', description: '', filename: '', content: '',
}

// --- Template Form (wraps config forms) ---

interface TemplateFormData {
  templateName: string
  templateDescription: string
  entityType: EntityType
  agentConfig: AgentFormData
  skillConfig: SkillFormData
  scriptConfig: ScriptFormData
}

const emptyTemplateForm = (entityType: EntityType): TemplateFormData => ({
  templateName: '',
  templateDescription: '',
  entityType,
  agentConfig: { ...emptyAgentForm },
  skillConfig: { ...emptySkillForm },
  scriptConfig: { ...emptyScriptForm },
})

function templateToForm(t: Template): TemplateFormData {
  const form = emptyTemplateForm(t.entityType as EntityType)
  form.templateName = t.name
  form.templateDescription = t.description

  if (t.entityType === 'agent' && t.agentConfig) {
    form.agentConfig = {
      name: t.agentConfig.name,
      description: t.agentConfig.description,
      prompt: t.agentConfig.prompt,
      tools: [...(t.agentConfig.tools ?? [])],
      disallowedTools: [...(t.agentConfig.disallowedTools ?? [])],
      model: t.agentConfig.model,
      permissionMode: t.agentConfig.permissionMode,
      skills: [...(t.agentConfig.skills ?? [])],
      memory: t.agentConfig.memory,
    }
  } else if (t.entityType === 'skill' && t.skillConfig) {
    form.skillConfig = {
      name: t.skillConfig.name,
      description: t.skillConfig.description,
      content: t.skillConfig.content,
      disableModelInvocation: t.skillConfig.disableModelInvocation,
      userInvocable: t.skillConfig.userInvocable,
      allowedTools: [...(t.skillConfig.allowedTools ?? [])],
      model: t.skillConfig.model,
      context: t.skillConfig.context,
      agent: t.skillConfig.agent,
      argumentHint: t.skillConfig.argumentHint,
    }
  } else if (t.entityType === 'script' && t.scriptConfig) {
    form.scriptConfig = {
      name: t.scriptConfig.name,
      description: t.scriptConfig.description,
      filename: t.scriptConfig.filename,
      content: t.scriptConfig.content,
    }
  }
  return form
}

export function TemplateList() {
  const [activeTab, setActiveTab] = useState<EntityType>('agent')
  const { data, refetch, isLoading } = useQuery(listTemplates, { entityType: activeTab })
  const createMut = useMutation(createTemplate)
  const updateMut = useMutation(updateTemplate)
  const deleteMut = useMutation(deleteTemplate)

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<TemplateFormData>(emptyTemplateForm('agent'))
  const [skillInput, setSkillInput] = useState('')

  const templates = data?.templates ?? []

  const openCreate = () => {
    setFormMode('create')
    setEditingId(null)
    setForm(emptyTemplateForm(activeTab))
    setSkillInput('')
  }

  const openEdit = (t: Template) => {
    setFormMode('edit')
    setEditingId(t.id)
    setForm(templateToForm(t))
    setSkillInput('')
  }

  const closeForm = () => {
    setFormMode(null)
    setEditingId(null)
    setForm(emptyTemplateForm(activeTab))
    setSkillInput('')
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const base = {
      name: form.templateName,
      description: form.templateDescription,
    }

    const configPayload = (() => {
      switch (form.entityType) {
        case 'agent': return { agentConfig: form.agentConfig }
        case 'skill': return { skillConfig: form.skillConfig }
        case 'script': return { scriptConfig: form.scriptConfig }
      }
    })()

    if (formMode === 'create') {
      createMut.mutate(
        { ...base, entityType: form.entityType, ...configPayload },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    } else if (formMode === 'edit' && editingId) {
      updateMut.mutate(
        { id: editingId, ...base, ...configPayload },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    }
  }

  const handleDelete = (id: string) => {
    if (!confirm('Delete this template?')) return
    deleteMut.mutate({ id }, { onSuccess: () => refetch() })
  }

  const handleTabChange = (type: EntityType) => {
    closeForm()
    setActiveTab(type)
  }

  const mutation = formMode === 'create' ? createMut : updateMut
  const activeTabInfo = TABS.find(t => t.type === activeTab)!

  const toggleTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: {
        ...prev.agentConfig,
        tools: prev.agentConfig.tools.includes(tool)
          ? prev.agentConfig.tools.filter(t => t !== tool)
          : [...prev.agentConfig.tools, tool],
      },
    }))
  }

  const toggleDisallowedTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: {
        ...prev.agentConfig,
        disallowedTools: prev.agentConfig.disallowedTools.includes(tool)
          ? prev.agentConfig.disallowedTools.filter(t => t !== tool)
          : [...prev.agentConfig.disallowedTools, tool],
      },
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
      skillConfig: {
        ...prev.skillConfig,
        allowedTools: prev.skillConfig.allowedTools.includes(tool)
          ? prev.skillConfig.allowedTools.filter(t => t !== tool)
          : [...prev.skillConfig.allowedTools, tool],
      },
    }))
  }

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <div className="flex items-center gap-2">
          <Layers className="w-5 h-5 text-amber-400" />
          <h2 className="text-lg md:text-xl font-bold text-white">Templates</h2>
        </div>
        <Button
          variant="danger"
          size="sm"
          onClick={openCreate}
          icon={<Plus className="w-4 h-4" />}
          className="text-xs md:text-sm md:px-3"
        >
          <span className="hidden sm:inline">New Template</span>
          <span className="sm:hidden">New</span>
        </Button>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 mb-4 md:mb-6 bg-slate-900 rounded-lg p-1 border border-slate-800">
        {TABS.map(tab => {
          const Icon = tab.icon
          return (
            <button
              key={tab.type}
              onClick={() => handleTabChange(tab.type)}
              className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-xs md:text-sm rounded-md transition-colors ${
                activeTab === tab.type
                  ? `bg-slate-800 text-white`
                  : 'text-gray-500 hover:text-gray-300'
              }`}
            >
              <Icon className="w-3.5 h-3.5" />
              {tab.label}
            </button>
          )
        })}
      </div>

      {/* Form */}
      {formMode && (
        <form onSubmit={handleSubmit}>
          <Card className="p-4 md:p-5 mb-4 md:mb-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">
                {formMode === 'create' ? 'New' : 'Edit'} {activeTabInfo.label.slice(0, -1)} Template
              </h3>
              <Button variant="ghost" size="sm" iconOnly onClick={closeForm} type="button" icon={<X className="w-5 h-5" />} />
            </div>

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
              {activeTab === 'agent' && <AgentConfigForm form={form} setForm={setForm} skillInput={skillInput} setSkillInput={setSkillInput} toggleTool={toggleTool} toggleDisallowedTool={toggleDisallowedTool} addSkill={addSkill} removeSkill={removeSkill} />}
              {activeTab === 'skill' && <SkillConfigForm form={form} setForm={setForm} toggleAllowedTool={toggleAllowedTool} />}
              {activeTab === 'script' && <ScriptConfigForm form={form} setForm={setForm} />}
            </div>

            {mutation.error && (
              <p className="text-red-400 text-sm mt-3">{mutation.error.message}</p>
            )}

            <div className="flex justify-end gap-2 mt-4">
              <Button type="button" variant="secondary" size="sm" onClick={closeForm}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant="danger"
                size="sm"
                disabled={mutation.isPending || !form.templateName}
                icon={<Save className="w-3.5 h-3.5" />}
              >
                {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
              </Button>
            </div>
          </Card>
        </form>
      )}

      {/* Template Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading templates...</p>}

      <div className="space-y-3">
        {templates.map(tmpl => (
          <TemplateCard
            key={tmpl.id}
            template={tmpl}
            onEdit={() => openEdit(tmpl)}
            onDelete={() => handleDelete(tmpl.id)}
            isDeleting={deleteMut.isPending}
          />
        ))}

        {!isLoading && templates.length === 0 && !formMode && (
          <div className="text-center py-12 text-gray-500">
            <Layers className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No {activeTabInfo.label.toLowerCase()} templates yet.</p>
            <p className="text-xs mt-1">Save entities as templates or create one from scratch.</p>
          </div>
        )}
      </div>
    </div>
  )
}

// --- Template Card ---

function TemplateCard({ template: tmpl, onEdit, onDelete, isDeleting }: {
  template: Template
  onEdit: () => void
  onDelete: () => void
  isDeleting: boolean
}) {
  const tabInfo = TABS.find(t => t.type === tmpl.entityType) ?? TABS[0]
  const Icon = tabInfo.icon

  const configName = (() => {
    if (tmpl.entityType === 'agent' && tmpl.agentConfig) return tmpl.agentConfig.name
    if (tmpl.entityType === 'skill' && tmpl.skillConfig) return tmpl.skillConfig.name
    if (tmpl.entityType === 'script' && tmpl.scriptConfig) return tmpl.scriptConfig.name
    return ''
  })()

  const configPreview = (() => {
    if (tmpl.entityType === 'agent' && tmpl.agentConfig) return tmpl.agentConfig.prompt
    if (tmpl.entityType === 'skill' && tmpl.skillConfig) return tmpl.skillConfig.content
    if (tmpl.entityType === 'script' && tmpl.scriptConfig) return tmpl.scriptConfig.content
    return ''
  })()

  return (
    <Card className="hover:border-slate-700 transition-colors">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          <Icon className={`w-5 h-5 mt-0.5 shrink-0 ${
            tmpl.entityType === 'agent' ? 'text-cyan-400' :
            tmpl.entityType === 'skill' ? 'text-purple-400' : 'text-green-400'
          }`} />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <h3 className="text-sm font-semibold text-white truncate">{tmpl.name}</h3>
              {configName && configName !== tmpl.name && (
                <Badge color="gray" size="xs" pill>
                  {configName}
                </Badge>
              )}
              <Badge
                color={
                  tmpl.entityType === 'agent' ? 'cyan' :
                  tmpl.entityType === 'skill' ? 'purple' : 'green'
                }
                size="xs"
                variant="outline"
                pill
              >
                {tmpl.entityType}
              </Badge>
            </div>
            {tmpl.description && (
              <p className="text-xs text-gray-400 mb-2">{tmpl.description}</p>
            )}

            {/* Agent-specific details */}
            {tmpl.entityType === 'agent' && tmpl.agentConfig && (
              <>
                {tmpl.agentConfig.tools && tmpl.agentConfig.tools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-1">
                    {tmpl.agentConfig.tools.map(tool => (
                      <Badge key={tool} color="gray" size="xs" className="bg-slate-800 text-gray-500">
                        {tool}
                      </Badge>
                    ))}
                  </div>
                )}
                {tmpl.agentConfig.skills && tmpl.agentConfig.skills.length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-1">
                    {tmpl.agentConfig.skills.map(skill => (
                      <Badge key={skill} color="purple" size="xs">
                        {skill}
                      </Badge>
                    ))}
                  </div>
                )}
              </>
            )}

            {/* Skill-specific details */}
            {tmpl.entityType === 'skill' && tmpl.skillConfig && (
              <>
                {tmpl.skillConfig.allowedTools && tmpl.skillConfig.allowedTools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-1">
                    {tmpl.skillConfig.allowedTools.map(tool => (
                      <Badge key={tool} color="purple" size="xs">
                        {tool}
                      </Badge>
                    ))}
                  </div>
                )}
              </>
            )}

            {configPreview && (
              <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                {configPreview.slice(0, 120)}{configPreview.length > 120 ? '...' : ''}
              </pre>
            )}
          </div>
        </div>
        <div className="flex items-center gap-1 shrink-0 ml-2">
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            onClick={onEdit}
            title="Edit"
            className="hover:text-amber-400"
            icon={<Edit2 className="w-3.5 h-3.5" />}
          />
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            onClick={onDelete}
            disabled={isDeleting}
            title="Delete"
            className="hover:text-red-400"
            icon={<Trash2 className="w-3.5 h-3.5" />}
          />
        </div>
      </div>
    </Card>
  )
}

// --- Agent Config Form ---

function AgentConfigForm({ form, setForm, skillInput, setSkillInput, toggleTool, toggleDisallowedTool, addSkill, removeSkill }: {
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

function SkillConfigForm({ form, setForm, toggleAllowedTool }: {
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

function ScriptConfigForm({ form, setForm }: {
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
