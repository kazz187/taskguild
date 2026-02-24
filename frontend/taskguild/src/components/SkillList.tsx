import { useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listSkills, createSkill, updateSkill, deleteSkill, syncSkillsFromDir } from '@taskguild/proto/taskguild/v1/skill-SkillService_connectquery.ts'
import type { SkillDefinition } from '@taskguild/proto/taskguild/v1/skill_pb.ts'
import { Sparkles, Plus, Trash2, Edit2, RefreshCw, X, Save, Cloud } from 'lucide-react'

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

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<SkillFormData>(emptyForm)

  const skills = data?.skills ?? []

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
      { projectId, directory: '.' },
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
        <div className="flex items-center gap-2">
          <Sparkles className="w-5 h-5 text-purple-400" />
          <h2 className="text-xl font-bold text-white">Skills</h2>
          <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5">
            {skills.length}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleSync}
            disabled={syncMut.isPending}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors disabled:opacity-50"
            title="Sync skills from .claude/skills/ directory"
          >
            <RefreshCw className={`w-4 h-4 ${syncMut.isPending ? 'animate-spin' : ''}`} />
            Sync from Repo
          </button>
          <button
            onClick={openCreate}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-purple-600 hover:bg-purple-500 text-white rounded-lg transition-colors"
          >
            <Plus className="w-4 h-4" />
            New Skill
          </button>
        </div>
      </div>

      {syncMut.isSuccess && (
        <div className="mb-4 px-3 py-2 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm">
          Synced: {syncMut.data?.created ?? 0} created, {syncMut.data?.updated ?? 0} updated
        </div>
      )}

      {/* Skill Form */}
      {formMode && (
        <form onSubmit={handleSubmit} className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-lg font-semibold text-white">
              {formMode === 'create' ? 'New Skill' : 'Edit Skill'}
            </h3>
            <button type="button" onClick={closeForm} className="text-gray-500 hover:text-gray-300 transition-colors">
              <X className="w-5 h-5" />
            </button>
          </div>

          <div className="space-y-4">
            {/* Name & Description row */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs text-gray-400 mb-1">Name *</label>
                <input
                  type="text"
                  required
                  value={form.name}
                  onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-purple-500"
                  placeholder="e.g. explain-code"
                />
                <p className="text-[10px] text-gray-600 mt-0.5">Lowercase with hyphens. Used as directory name in .claude/skills/</p>
              </div>
              <div>
                <label className="block text-xs text-gray-400 mb-1">Description</label>
                <input
                  type="text"
                  value={form.description}
                  onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-purple-500"
                  placeholder="When to use this skill"
                />
                <p className="text-[10px] text-gray-600 mt-0.5">Claude uses this to decide when to load this skill</p>
              </div>
            </div>

            {/* Argument Hint */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">Argument Hint</label>
              <input
                type="text"
                value={form.argumentHint}
                onChange={e => setForm(prev => ({ ...prev, argumentHint: e.target.value }))}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-purple-500"
                placeholder="e.g. [issue-number] or [filename] [format]"
              />
              <p className="text-[10px] text-gray-600 mt-0.5">Hint shown in autocomplete for expected arguments</p>
            </div>

            {/* Content */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">Content *</label>
              <textarea
                required
                value={form.content}
                onChange={e => setForm(prev => ({ ...prev, content: e.target.value }))}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-purple-500 min-h-[150px] font-mono"
                placeholder="When explaining code, always include:&#10;1. Start with an analogy&#10;2. Draw a diagram using ASCII art&#10;3. Walk through the code step-by-step"
              />
              <p className="text-[10px] text-gray-600 mt-0.5">Body of the SKILL.md file. Instructions Claude follows when this skill is invoked.</p>
            </div>

            {/* Invocation Control */}
            <div>
              <label className="block text-xs text-gray-400 mb-2">Invocation Control</label>
              <div className="flex gap-6">
                <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={form.disableModelInvocation}
                    onChange={e => setForm(prev => ({ ...prev, disableModelInvocation: e.target.checked }))}
                    className="rounded border-slate-600 bg-slate-800 text-purple-500 focus:ring-purple-500"
                  />
                  <span>Disable model invocation</span>
                </label>
                <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={form.userInvocable}
                    onChange={e => setForm(prev => ({ ...prev, userInvocable: e.target.checked }))}
                    className="rounded border-slate-600 bg-slate-800 text-purple-500 focus:ring-purple-500"
                  />
                  <span>User invocable</span>
                </label>
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
              <div>
                <label className="block text-xs text-gray-400 mb-1">Model</label>
                <select
                  value={form.model}
                  onChange={e => setForm(prev => ({ ...prev, model: e.target.value }))}
                  className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-purple-500"
                >
                  {MODEL_OPTIONS.map(opt => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-gray-400 mb-1">Context</label>
                <select
                  value={form.context}
                  onChange={e => setForm(prev => ({ ...prev, context: e.target.value }))}
                  className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-purple-500"
                >
                  {CONTEXT_OPTIONS.map(opt => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-gray-400 mb-1">Agent</label>
                <select
                  value={form.agent}
                  onChange={e => setForm(prev => ({ ...prev, agent: e.target.value }))}
                  disabled={form.context !== 'fork'}
                  className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-purple-500 disabled:opacity-40 disabled:cursor-not-allowed"
                >
                  {AGENT_OPTIONS.map(opt => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
                <p className="text-[10px] text-gray-600 mt-0.5">Only used when context is "fork"</p>
              </div>
            </div>
          </div>

          {mutation.error && (
            <p className="text-red-400 text-sm mt-3">{mutation.error.message}</p>
          )}

          <div className="flex justify-end gap-2 mt-4">
            <button
              type="button"
              onClick={closeForm}
              className="px-3 py-1.5 text-sm text-gray-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={mutation.isPending || !form.name || !form.content}
              className="flex items-center gap-1.5 px-4 py-1.5 text-sm bg-purple-600 hover:bg-purple-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
            >
              <Save className="w-3.5 h-3.5" />
              {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
            </button>
          </div>
        </form>
      )}

      {/* Skill Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading skills...</p>}

      <div className="space-y-3">
        {skills.map(skill => (
          <div
            key={skill.id}
            className="bg-slate-900 border border-slate-800 rounded-xl p-4 hover:border-slate-700 transition-colors"
          >
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3 flex-1 min-w-0">
                <Sparkles className="w-5 h-5 text-purple-400 mt-0.5 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <h3 className="text-sm font-semibold text-white truncate">{skill.name}</h3>
                    {skill.isSynced && (
                      <span className="flex items-center gap-0.5 text-[10px] text-blue-400 bg-blue-500/10 border border-blue-500/20 rounded-full px-1.5 py-0.5 shrink-0">
                        <Cloud className="w-2.5 h-2.5" />
                        synced
                      </span>
                    )}
                    {skill.model && (
                      <span className="text-[10px] text-gray-500 bg-slate-800 rounded-full px-1.5 py-0.5 shrink-0">
                        {skill.model}
                      </span>
                    )}
                    {skill.context === 'fork' && (
                      <span className="text-[10px] text-orange-400 bg-orange-500/10 border border-orange-500/20 rounded-full px-1.5 py-0.5 shrink-0">
                        fork{skill.agent ? `: ${skill.agent}` : ''}
                      </span>
                    )}
                    {skill.disableModelInvocation && (
                      <span className="text-[10px] text-yellow-400 bg-yellow-500/10 border border-yellow-500/20 rounded-full px-1.5 py-0.5 shrink-0">
                        manual only
                      </span>
                    )}
                    {!skill.userInvocable && (
                      <span className="text-[10px] text-gray-400 bg-slate-700 rounded-full px-1.5 py-0.5 shrink-0">
                        model only
                      </span>
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
                        <span key={tool} className="text-[10px] px-1.5 py-0.5 bg-purple-500/10 text-purple-400 rounded">
                          {tool}
                        </span>
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
                <button
                  onClick={() => openEdit(skill)}
                  className="p-1.5 text-gray-500 hover:text-purple-400 transition-colors rounded-lg hover:bg-slate-800"
                  title="Edit"
                >
                  <Edit2 className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={() => handleDelete(skill.id)}
                  disabled={deleteMut.isPending}
                  className="p-1.5 text-gray-500 hover:text-red-400 transition-colors rounded-lg hover:bg-slate-800 disabled:opacity-50"
                  title="Delete"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          </div>
        ))}

        {!isLoading && skills.length === 0 && !formMode && (
          <div className="text-center py-12 text-gray-500">
            <Sparkles className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No skills defined yet.</p>
            <p className="text-xs mt-1">Create skills or sync from your repository's .claude/skills/ directory.</p>
          </div>
        )}
      </div>
    </div>
  )
}
