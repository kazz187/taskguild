import { useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listSkills, createSkill, updateSkill, deleteSkill, syncSkillsFromDir } from '@taskguild/proto/taskguild/v1/skill-SkillService_connectquery.ts'
import { saveAsTemplate, listTemplates } from '@taskguild/proto/taskguild/v1/template-TemplateService_connectquery.ts'
import type { SkillDefinition } from '@taskguild/proto/taskguild/v1/skill_pb.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { Sparkles, Plus, Trash2, Edit2, RefreshCw, X, Save, Cloud, Layers, Copy } from 'lucide-react'
import { Button } from '../atoms/index.ts'
import { Input, Textarea, Select, Checkbox, Badge } from '../atoms/index.ts'
import { Card, FormField, PageHeading } from '../molecules/index.ts'

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

const emptyForm: SkillFormData = {
  name: '',
  description: '',
  content: '',
  disableModelInvocation: false,
  userInvocable: true,
  allowedTools: [],
  model: '',
  context: '',
  agent: '',
  argumentHint: '',
}

function skillToForm(s: SkillDefinition): SkillFormData {
  return {
    name: s.name,
    description: s.description,
    content: s.content,
    disableModelInvocation: s.disableModelInvocation,
    userInvocable: s.userInvocable,
    allowedTools: [...(s.allowedTools ?? [])],
    model: s.model,
    context: s.context,
    agent: s.agent,
    argumentHint: s.argumentHint,
  }
}

export function SkillList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(listSkills, { projectId })
  const createMut = useMutation(createSkill)
  const updateMut = useMutation(updateSkill)
  const deleteMut = useMutation(deleteSkill)
  const syncMut = useMutation(syncSkillsFromDir)
  const saveTemplateMut = useMutation(saveAsTemplate)
  const { data: templatesData, refetch: refetchTemplates } = useQuery(listTemplates, { entityType: 'skill' })

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<SkillFormData>(emptyForm)

  // Template dialog state
  const [saveTemplateDialog, setSaveTemplateDialog] = useState<{ skillId: string; name: string; description: string } | null>(null)
  const [templatePickerOpen, setTemplatePickerOpen] = useState(false)

  const skills = data?.skills ?? []
  const skillTemplates = templatesData?.templates ?? []

  const openCreate = () => {
    setFormMode('create')
    setEditingId(null)
    setForm(emptyForm)
  }

  const openEdit = (s: SkillDefinition) => {
    setFormMode('edit')
    setEditingId(s.id)
    setForm(skillToForm(s))
  }

  const closeForm = () => {
    setFormMode(null)
    setEditingId(null)
    setForm(emptyForm)
  }

  const handleSaveAsTemplate = (skill: SkillDefinition) => {
    setSaveTemplateDialog({ skillId: skill.id, name: skill.name, description: skill.description })
  }

  const handleSaveTemplateSubmit = () => {
    if (!saveTemplateDialog) return
    saveTemplateMut.mutate(
      { entityType: 'skill', entityId: saveTemplateDialog.skillId, templateName: saveTemplateDialog.name, templateDescription: saveTemplateDialog.description },
      { onSuccess: () => { setSaveTemplateDialog(null); refetchTemplates() } },
    )
  }

  const handleCreateFromTemplate = (tmpl: Template) => {
    if (!tmpl.skillConfig) return
    setTemplatePickerOpen(false)
    setFormMode('create')
    setEditingId(null)
    setForm({
      name: tmpl.skillConfig.name,
      description: tmpl.skillConfig.description,
      content: tmpl.skillConfig.content,
      disableModelInvocation: tmpl.skillConfig.disableModelInvocation,
      userInvocable: tmpl.skillConfig.userInvocable,
      allowedTools: [...(tmpl.skillConfig.allowedTools ?? [])],
      model: tmpl.skillConfig.model,
      context: tmpl.skillConfig.context,
      agent: tmpl.skillConfig.agent,
      argumentHint: tmpl.skillConfig.argumentHint,
    })
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (formMode === 'create') {
      createMut.mutate(
        { projectId, ...form },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    } else if (formMode === 'edit' && editingId) {
      updateMut.mutate(
        { id: editingId, ...form },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    }
  }

  const handleDelete = (id: string) => {
    if (!confirm('Delete this skill?')) return
    deleteMut.mutate({ id }, { onSuccess: () => refetch() })
  }

  const handleSync = () => {
    syncMut.mutate(
      { projectId },
      { onSuccess: () => refetch() },
    )
  }

  const toggleAllowedTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      allowedTools: prev.allowedTools.includes(tool)
        ? prev.allowedTools.filter(t => t !== tool)
        : [...prev.allowedTools, tool],
    }))
  }

  const mutation = formMode === 'create' ? createMut : updateMut

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <PageHeading icon={Sparkles} title="Skills" iconColor="text-purple-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {skills.length}
          </Badge>
        </PageHeading>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleSync}
            disabled={syncMut.isPending}
            icon={<RefreshCw className={`w-4 h-4 ${syncMut.isPending ? 'animate-spin' : ''}`} />}
            title="Sync skills from .claude/skills/ directory"
            className="border border-slate-700 hover:border-slate-600"
          >
            Sync from Repo
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => { refetchTemplates(); setTemplatePickerOpen(true) }}
            icon={<Layers className="w-4 h-4" />}
            title="Create skill from template"
            className="border border-slate-700 hover:border-slate-600"
          >
            From Template
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={openCreate}
            icon={<Plus className="w-4 h-4" />}
            className="bg-purple-600 hover:bg-purple-500"
          >
            New Skill
          </Button>
        </div>
      </div>

      {syncMut.isSuccess && (
        <div className="mb-4 px-3 py-2 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm">
          Synced: {syncMut.data?.created ?? 0} created, {syncMut.data?.updated ?? 0} updated
        </div>
      )}

      {/* Skill Form */}
      {formMode && (
        <form onSubmit={handleSubmit}>
          <Card className="p-5 mb-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">
                {formMode === 'create' ? 'New Skill' : 'Edit Skill'}
              </h3>
              <Button variant="ghost" size="sm" iconOnly onClick={closeForm} type="button" icon={<X className="w-5 h-5" />} />
            </div>

            <div className="space-y-4">
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
            </div>

            {mutation.error && (
              <p className="text-red-400 text-sm mt-3">{mutation.error.message}</p>
            )}

            <div className="flex justify-end gap-2 mt-4">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={closeForm}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                variant="primary"
                size="sm"
                disabled={mutation.isPending || !form.name || !form.content}
                icon={<Save className="w-3.5 h-3.5" />}
                className="bg-purple-600 hover:bg-purple-500"
              >
                {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
              </Button>
            </div>
          </Card>
        </form>
      )}

      {/* Skill Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading skills...</p>}

      <div className="space-y-3">
        {skills.map(skill => (
          <Card
            key={skill.id}
            className="hover:border-slate-700 transition-colors"
          >
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3 flex-1 min-w-0">
                <Sparkles className="w-5 h-5 text-purple-400 mt-0.5 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <h3 className="text-sm font-semibold text-white truncate">{skill.name}</h3>
                    {skill.isSynced && (
                      <Badge color="blue" size="xs" variant="outline" pill icon={<Cloud className="w-2.5 h-2.5" />}>
                        synced
                      </Badge>
                    )}
                    {skill.model && (
                      <Badge color="gray" size="xs" pill>
                        {skill.model}
                      </Badge>
                    )}
                    {skill.context === 'fork' && (
                      <Badge color="orange" size="xs" variant="outline" pill>
                        fork{skill.agent ? `: ${skill.agent}` : ''}
                      </Badge>
                    )}
                    {skill.disableModelInvocation && (
                      <Badge color="yellow" size="xs" variant="outline" pill>
                        manual only
                      </Badge>
                    )}
                    {!skill.userInvocable && (
                      <Badge color="gray" size="xs" pill className="bg-slate-700 text-gray-400">
                        model only
                      </Badge>
                    )}
                  </div>
                  {skill.description && (
                    <p className="text-xs text-gray-400 mb-2">{skill.description}</p>
                  )}
                  {skill.argumentHint && (
                    <p className="text-[10px] text-gray-500 mb-1">
                      <span className="text-gray-600">Usage:</span> /{skill.name} {skill.argumentHint}
                    </p>
                  )}
                  {skill.allowedTools?.length > 0 && (
                    <div className="flex flex-wrap gap-1 mb-1">
                      {skill.allowedTools.map(tool => (
                        <Badge key={tool} color="purple" size="xs">
                          {tool}
                        </Badge>
                      ))}
                    </div>
                  )}
                  {skill.content && (
                    <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                      {skill.content.slice(0, 120)}{skill.content.length > 120 ? '...' : ''}
                    </pre>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-1 shrink-0 ml-2">
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  onClick={() => handleSaveAsTemplate(skill)}
                  title="Save as Template"
                  className="hover:text-amber-400"
                  icon={<Copy className="w-3.5 h-3.5" />}
                />
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  onClick={() => openEdit(skill)}
                  title="Edit"
                  className="hover:text-purple-400"
                  icon={<Edit2 className="w-3.5 h-3.5" />}
                />
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  onClick={() => handleDelete(skill.id)}
                  disabled={deleteMut.isPending}
                  title="Delete"
                  className="hover:text-red-400"
                  icon={<Trash2 className="w-3.5 h-3.5" />}
                />
              </div>
            </div>
          </Card>
        ))}

        {!isLoading && skills.length === 0 && !formMode && (
          <div className="text-center py-12 text-gray-500">
            <Sparkles className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No skills defined yet.</p>
            <p className="text-xs mt-1">Create skills or sync from your repository's .claude/skills/ directory.</p>
          </div>
        )}
      </div>

      {/* Template Picker Dialog */}
      {templatePickerOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={() => setTemplatePickerOpen(false)}>
          <div className="bg-slate-900 border border-slate-700 rounded-xl p-5 max-w-md w-full mx-4 max-h-[70vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">Select Skill Template</h3>
              <button onClick={() => setTemplatePickerOpen(false)} className="text-gray-500 hover:text-gray-300"><X className="w-5 h-5" /></button>
            </div>
            {skillTemplates.length === 0 ? (
              <p className="text-gray-500 text-sm text-center py-6">No skill templates available.</p>
            ) : (
              <div className="space-y-2">
                {skillTemplates.map(tmpl => (
                  <button key={tmpl.id} onClick={() => handleCreateFromTemplate(tmpl)} className="w-full text-left p-3 bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors">
                    <div className="flex items-center gap-2 mb-1">
                      <Sparkles className="w-4 h-4 text-purple-400" />
                      <span className="text-sm font-medium text-white">{tmpl.name}</span>
                    </div>
                    {tmpl.description && <p className="text-xs text-gray-400 ml-6">{tmpl.description}</p>}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Save as Template Dialog */}
      {saveTemplateDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={() => setSaveTemplateDialog(null)}>
          <div className="bg-slate-900 border border-slate-700 rounded-xl p-5 max-w-md w-full mx-4" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">Save as Template</h3>
              <button onClick={() => setSaveTemplateDialog(null)} className="text-gray-500 hover:text-gray-300"><X className="w-5 h-5" /></button>
            </div>
            <div className="space-y-3">
              <FormField label="Template Name">
                <Input type="text" value={saveTemplateDialog.name}
                  onChange={e => setSaveTemplateDialog(prev => prev ? { ...prev, name: e.target.value } : null)}
                  className="focus:border-amber-500" />
              </FormField>
              <FormField label="Template Description">
                <Input type="text" value={saveTemplateDialog.description}
                  onChange={e => setSaveTemplateDialog(prev => prev ? { ...prev, description: e.target.value } : null)}
                  className="focus:border-amber-500" />
              </FormField>
            </div>
            {saveTemplateMut.error && <p className="text-red-400 text-sm mt-3">{saveTemplateMut.error.message}</p>}
            {saveTemplateMut.isSuccess && <p className="text-green-400 text-sm mt-3">Template saved successfully!</p>}
            <div className="flex justify-end gap-2 mt-4">
              <Button variant="secondary" size="sm" onClick={() => setSaveTemplateDialog(null)}>Cancel</Button>
              <Button
                variant="danger"
                size="sm"
                onClick={handleSaveTemplateSubmit}
                disabled={saveTemplateMut.isPending || !saveTemplateDialog.name}
                icon={<Save className="w-3.5 h-3.5" />}
              >
                {saveTemplateMut.isPending ? 'Saving...' : 'Save Template'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
