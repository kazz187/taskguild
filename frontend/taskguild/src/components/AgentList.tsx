import { useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listAgents, createAgent, updateAgent, deleteAgent, syncAgentsFromDir } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
import type { AgentDefinition } from '@taskguild/proto/taskguild/v1/agent_pb.ts'
import { Bot, Plus, Trash2, Edit2, RefreshCw, X, Save, Cloud } from 'lucide-react'

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

const emptyForm: AgentFormData = {
  name: '',
  description: '',
  prompt: '',
  tools: [],
  disallowedTools: [],
  model: '',
  permissionMode: '',
  skills: [],
  memory: '',
}

function agentToForm(a: AgentDefinition): AgentFormData {
  return {
    name: a.name,
    description: a.description,
    prompt: a.prompt,
    tools: [...a.tools],
    disallowedTools: [...a.disallowedTools],
    model: a.model,
    permissionMode: a.permissionMode,
    skills: [...a.skills],
    memory: a.memory,
  }
}

export function AgentList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(listAgents, { projectId })
  const createMut = useMutation(createAgent)
  const updateMut = useMutation(updateAgent)
  const deleteMut = useMutation(deleteAgent)
  const syncMut = useMutation(syncAgentsFromDir)

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<AgentFormData>(emptyForm)
  const [skillInput, setSkillInput] = useState('')

  const agents = data?.agents ?? []

  const openCreate = () => {
    setFormMode('create')
    setEditingId(null)
    setForm(emptyForm)
    setSkillInput('')
  }

  const openEdit = (a: AgentDefinition) => {
    setFormMode('edit')
    setEditingId(a.id)
    setForm(agentToForm(a))
    setSkillInput('')
  }

  const closeForm = () => {
    setFormMode(null)
    setEditingId(null)
    setForm(emptyForm)
    setSkillInput('')
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
    if (!confirm('Delete this agent?')) return
    deleteMut.mutate({ id }, { onSuccess: () => refetch() })
  }

  const handleSync = () => {
    syncMut.mutate(
      { projectId, directory: '.' },
      { onSuccess: () => refetch() },
    )
  }

  const toggleTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      tools: prev.tools.includes(tool)
        ? prev.tools.filter(t => t !== tool)
        : [...prev.tools, tool],
    }))
  }

  const toggleDisallowedTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      disallowedTools: prev.disallowedTools.includes(tool)
        ? prev.disallowedTools.filter(t => t !== tool)
        : [...prev.disallowedTools, tool],
    }))
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

  const mutation = formMode === 'create' ? createMut : updateMut

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-2">
          <Bot className="w-5 h-5 text-cyan-400" />
          <h2 className="text-xl font-bold text-white">Agents</h2>
          <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5">
            {agents.length}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleSync}
            disabled={syncMut.isPending}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors disabled:opacity-50"
            title="Sync agents from .claude/agents/ directory"
          >
            <RefreshCw className={`w-4 h-4 ${syncMut.isPending ? 'animate-spin' : ''}`} />
            Sync from Repo
          </button>
          <button
            onClick={openCreate}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors"
          >
            <Plus className="w-4 h-4" />
            New Agent
          </button>
        </div>
      </div>

      {syncMut.isSuccess && (
        <div className="mb-4 px-3 py-2 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm">
          Synced: {syncMut.data?.created ?? 0} created, {syncMut.data?.updated ?? 0} updated
        </div>
      )}

      {/* Agent Form */}
      {formMode && (
        <form onSubmit={handleSubmit} className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-lg font-semibold text-white">
              {formMode === 'create' ? 'New Agent' : 'Edit Agent'}
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
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
                  placeholder="e.g. code-reviewer"
                />
                <p className="text-[10px] text-gray-600 mt-0.5">Lowercase with hyphens. Used as filename in .claude/agents/</p>
              </div>
              <div>
                <label className="block text-xs text-gray-400 mb-1">Description *</label>
                <input
                  type="text"
                  required
                  value={form.description}
                  onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
                  placeholder="When to delegate to this agent"
                />
                <p className="text-[10px] text-gray-600 mt-0.5">Claude uses this to decide when to delegate</p>
              </div>
            </div>

            {/* System Prompt */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">System Prompt *</label>
              <textarea
                required
                value={form.prompt}
                onChange={e => setForm(prev => ({ ...prev, prompt: e.target.value }))}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500 min-h-[150px] font-mono"
                placeholder="You are a code reviewer. Analyze code and provide specific, actionable feedback..."
              />
              <p className="text-[10px] text-gray-600 mt-0.5">Body of the .md file. This becomes the agent's system prompt.</p>
            </div>

            {/* Allowed Tools */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">Allowed Tools</label>
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
              <p className="text-[10px] text-gray-600 mt-1">Leave empty to inherit all tools from parent</p>
            </div>

            {/* Disallowed Tools */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">Disallowed Tools</label>
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
              <p className="text-[10px] text-gray-600 mt-1">Tools to deny, removed from inherited or specified list</p>
            </div>

            {/* Model & Permission Mode row */}
            <div className="grid grid-cols-3 gap-3">
              <div>
                <label className="block text-xs text-gray-400 mb-1">Model</label>
                <select
                  value={form.model}
                  onChange={e => setForm(prev => ({ ...prev, model: e.target.value }))}
                  className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
                >
                  {MODEL_OPTIONS.map(opt => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-gray-400 mb-1">Permission Mode</label>
                <select
                  value={form.permissionMode}
                  onChange={e => setForm(prev => ({ ...prev, permissionMode: e.target.value }))}
                  className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
                >
                  {PERMISSION_MODE_OPTIONS.map(opt => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-gray-400 mb-1">Memory</label>
                <select
                  value={form.memory}
                  onChange={e => setForm(prev => ({ ...prev, memory: e.target.value }))}
                  className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
                >
                  {MEMORY_OPTIONS.map(opt => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
              </div>
            </div>

            {/* Skills */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">Skills</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={skillInput}
                  onChange={e => setSkillInput(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addSkill() } }}
                  className="flex-1 px-3 py-1.5 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
                  placeholder="e.g. api-conventions"
                />
                <button
                  type="button"
                  onClick={addSkill}
                  className="px-3 py-1.5 text-xs bg-slate-800 border border-slate-700 rounded-lg text-gray-400 hover:text-white hover:border-slate-600 transition-colors"
                >
                  Add
                </button>
              </div>
              {form.skills.length > 0 && (
                <div className="flex flex-wrap gap-1.5 mt-2">
                  {form.skills.map(skill => (
                    <span
                      key={skill}
                      className="flex items-center gap-1 px-2 py-0.5 text-xs bg-purple-500/20 text-purple-400 border border-purple-500/30 rounded-lg"
                    >
                      {skill}
                      <button type="button" onClick={() => removeSkill(skill)} className="hover:text-purple-200">
                        <X className="w-3 h-3" />
                      </button>
                    </span>
                  ))}
                </div>
              )}
              <p className="text-[10px] text-gray-600 mt-1">Skills to preload into the agent's context at startup</p>
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
              disabled={mutation.isPending || !form.name || !form.description || !form.prompt}
              className="flex items-center gap-1.5 px-4 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
            >
              <Save className="w-3.5 h-3.5" />
              {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
            </button>
          </div>
        </form>
      )}

      {/* Agent Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading agents...</p>}

      <div className="space-y-3">
        {agents.map(agent => (
          <div
            key={agent.id}
            className="bg-slate-900 border border-slate-800 rounded-xl p-4 hover:border-slate-700 transition-colors"
          >
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3 flex-1 min-w-0">
                <Bot className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <h3 className="text-sm font-semibold text-white truncate">{agent.name}</h3>
                    {agent.isSynced && (
                      <span className="flex items-center gap-0.5 text-[10px] text-blue-400 bg-blue-500/10 border border-blue-500/20 rounded-full px-1.5 py-0.5 shrink-0">
                        <Cloud className="w-2.5 h-2.5" />
                        synced
                      </span>
                    )}
                    {agent.model && (
                      <span className="text-[10px] text-gray-500 bg-slate-800 rounded-full px-1.5 py-0.5 shrink-0">
                        {agent.model}
                      </span>
                    )}
                    {agent.memory && (
                      <span className="text-[10px] text-purple-400 bg-purple-500/10 border border-purple-500/20 rounded-full px-1.5 py-0.5 shrink-0">
                        memory: {agent.memory}
                      </span>
                    )}
                  </div>
                  <p className="text-xs text-gray-400 mb-2">{agent.description}</p>
                  {agent.tools.length > 0 && (
                    <div className="flex flex-wrap gap-1 mb-1">
                      {agent.tools.map(tool => (
                        <span key={tool} className="text-[10px] px-1.5 py-0.5 bg-slate-800 text-gray-500 rounded">
                          {tool}
                        </span>
                      ))}
                    </div>
                  )}
                  {agent.disallowedTools.length > 0 && (
                    <div className="flex flex-wrap gap-1 mb-1">
                      {agent.disallowedTools.map(tool => (
                        <span key={tool} className="text-[10px] px-1.5 py-0.5 bg-red-500/10 text-red-400 rounded">
                          -{tool}
                        </span>
                      ))}
                    </div>
                  )}
                  {agent.skills.length > 0 && (
                    <div className="flex flex-wrap gap-1 mb-1">
                      {agent.skills.map(skill => (
                        <span key={skill} className="text-[10px] px-1.5 py-0.5 bg-purple-500/10 text-purple-400 rounded">
                          {skill}
                        </span>
                      ))}
                    </div>
                  )}
                  {agent.prompt && (
                    <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                      {agent.prompt.slice(0, 120)}{agent.prompt.length > 120 ? '...' : ''}
                    </pre>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-1 shrink-0 ml-2">
                <button
                  onClick={() => openEdit(agent)}
                  className="p-1.5 text-gray-500 hover:text-cyan-400 transition-colors rounded-lg hover:bg-slate-800"
                  title="Edit"
                >
                  <Edit2 className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={() => handleDelete(agent.id)}
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

        {!isLoading && agents.length === 0 && !formMode && (
          <div className="text-center py-12 text-gray-500">
            <Bot className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No agents defined yet.</p>
            <p className="text-xs mt-1">Create agents or sync from your repository's .claude/agents/ directory.</p>
          </div>
        )}
      </div>
    </div>
  )
}
