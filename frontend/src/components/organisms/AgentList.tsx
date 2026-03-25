import { useState, useMemo, useCallback, useEffect, useRef } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listAgents, createAgent, updateAgent, deleteAgent, syncAgentsFromDir } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
import { saveAsTemplate, listTemplates } from '@taskguild/proto/taskguild/v1/template-TemplateService_connectquery.ts'
import {
  requestAgentComparison,
  getAgentComparison,
  resolveAgentConflict,
} from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import type { AgentDefinition } from '@taskguild/proto/taskguild/v1/agent_pb.ts'
import type { AgentDiff } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { AgentDiffType, AgentResolutionChoice } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { Bot, Plus, Trash2, Edit2, RefreshCw, X, Save, Cloud, Layers, Copy, AlertTriangle, Server, Monitor } from 'lucide-react'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { Button } from '../atoms/index.ts'
import { Input, Textarea, Select, Badge, Checkbox } from '../atoms/index.ts'
import { Card, FormField, Modal, PageHeading } from '../molecules/index.ts'

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
    tools: [...(a.tools ?? [])],
    disallowedTools: [...(a.disallowedTools ?? [])],
    model: a.model,
    permissionMode: a.permissionMode,
    skills: [...(a.skills ?? [])],
    memory: a.memory,
  }
}

function diffTypeLabel(dt: AgentDiffType): string {
  switch (dt) {
    case AgentDiffType.MODIFIED: return 'Modified'
    case AgentDiffType.AGENT_ONLY: return 'Agent Only'
    case AgentDiffType.SERVER_ONLY: return 'Server Only'
    default: return 'Unknown'
  }
}

export function AgentList({ projectId, editAgentId, mode }: { projectId: string; editAgentId?: string; mode?: 'create' }) {
  const navigate = useNavigate()
  const { data, refetch, isLoading } = useQuery(listAgents, { projectId })
  const createMut = useMutation(createAgent)
  const updateMut = useMutation(updateAgent)
  const deleteMut = useMutation(deleteAgent)
  const syncMut = useMutation(syncAgentsFromDir)
  const saveTemplateMut = useMutation(saveAsTemplate)
  const { data: templatesData, refetch: refetchTemplates } = useQuery(listTemplates, { entityType: 'agent' })

  // Agent comparison
  const requestComparisonMut = useMutation(requestAgentComparison)
  const { data: comparisonData, refetch: refetchComparison } = useQuery(getAgentComparison, { projectId })
  const resolveConflictMut = useMutation(resolveAgentConflict)

  // Derive form mode from URL search params
  const formMode: 'create' | 'edit' | null = mode === 'create' ? 'create' : editAgentId ? 'edit' : null
  const editingId: string | null = editAgentId ?? null

  const [form, setForm] = useState<AgentFormData>(emptyForm)
  const [skillInput, setSkillInput] = useState('')

  // Template dialog state
  const [saveTemplateDialog, setSaveTemplateDialog] = useState<{ agentId: string; name: string; description: string; includeSkills: boolean } | null>(null)
  const [templatePickerOpen, setTemplatePickerOpen] = useState(false)

  // Diff resolution dialog state
  const [diffDialog, setDiffDialog] = useState<AgentDiff | null>(null)

  const agentTemplates = templatesData?.templates ?? []
  const diffs = comparisonData?.diffs ?? []

  // Build a lookup map for diffs by agent_id.
  const diffByAgentId = useMemo(() => {
    const map = new Map<string, AgentDiff>()
    for (const d of diffs) {
      if (d.agentId) map.set(d.agentId, d)
    }
    return map
  }, [diffs])

  // Subscribe to AGENT_COMPARISON events to refetch diffs when comparison completes.
  const comparisonEventTypes = useMemo(() => [EventType.AGENT_COMPARISON], [])
  const onComparisonEvent = useCallback(() => {
    refetchComparison()
    refetch()
  }, [refetchComparison, refetch])
  useEventSubscription(comparisonEventTypes, projectId, onComparisonEvent)

  const openCreate = () => {
    navigate({ to: '/projects/$projectId/agents', params: { projectId }, search: { mode: 'create' } })
  }

  const openEdit = (a: AgentDefinition) => {
    navigate({ to: '/projects/$projectId/agents', params: { projectId }, search: { edit: a.id } })
  }

  const closeForm = () => {
    navigate({ to: '/projects/$projectId/agents', params: { projectId }, search: {} })
  }

  // Sync form state when URL params or agents data change
  const agents = data?.agents ?? []
  const skipFormResetRef = useRef(false)
  useEffect(() => {
    if (skipFormResetRef.current) {
      skipFormResetRef.current = false
      return
    }
    if (mode === 'create') {
      setForm(emptyForm)
      setSkillInput('')
    } else if (editAgentId) {
      const agent = agents.find(a => a.id === editAgentId)
      if (agent) {
        setForm(agentToForm(agent))
        setSkillInput('')
      }
    } else {
      setForm(emptyForm)
      setSkillInput('')
    }
  }, [mode, editAgentId, agents])

  // Template handlers
  const handleSaveAsTemplate = (agent: AgentDefinition) => {
    setSaveTemplateDialog({
      agentId: agent.id,
      name: agent.name,
      description: agent.description,
      includeSkills: agent.skills?.length > 0,
    })
  }

  const handleSaveTemplateSubmit = () => {
    if (!saveTemplateDialog) return
    saveTemplateMut.mutate(
      {
        entityType: 'agent',
        entityId: saveTemplateDialog.agentId,
        templateName: saveTemplateDialog.name,
        templateDescription: saveTemplateDialog.description,
        includeDependentSkills: saveTemplateDialog.includeSkills,
      },
      {
        onSuccess: () => {
          setSaveTemplateDialog(null)
          refetchTemplates()
        },
      },
    )
  }

  const handleCreateFromTemplate = (tmpl: Template) => {
    if (!tmpl.agentConfig) return
    setTemplatePickerOpen(false)
    // Pre-fill form before navigating to create mode
    setForm({
      name: tmpl.agentConfig.name,
      description: tmpl.agentConfig.description,
      prompt: tmpl.agentConfig.prompt,
      tools: [...(tmpl.agentConfig.tools ?? [])],
      disallowedTools: [...(tmpl.agentConfig.disallowedTools ?? [])],
      model: tmpl.agentConfig.model,
      permissionMode: tmpl.agentConfig.permissionMode,
      skills: [...(tmpl.agentConfig.skills ?? [])],
      memory: tmpl.agentConfig.memory,
    })
    setSkillInput('')
    skipFormResetRef.current = true
    navigate({ to: '/projects/$projectId/agents', params: { projectId }, search: { mode: 'create' } })
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
      { projectId },
      {
        onSuccess: () => {
          refetch()
          // After syncing from repo, automatically trigger comparison with agent.
          requestComparisonMut.mutate({ projectId })
        },
      },
    )
  }

  const handleResolveConflict = (diff: AgentDiff, choice: AgentResolutionChoice) => {
    resolveConflictMut.mutate(
      {
        projectId,
        agentId: diff.agentId,
        agentName: diff.agentName,
        filename: diff.filename,
        choice,
        agentContent: choice === AgentResolutionChoice.AGENT ? diff.agentContent : '',
      },
      {
        onSuccess: () => {
          refetchComparison()
          refetch()
          setDiffDialog(null)
        },
      },
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

  // Agent-only diffs (agents that exist only on agent, not in server DB).
  const agentOnlyDiffs = diffs.filter(d => d.diffType === AgentDiffType.AGENT_ONLY)

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <PageHeading icon={Bot} title="Agents" iconColor="text-cyan-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {agents.length}
          </Badge>
          {diffs.length > 0 && (
            <Badge color="amber" size="xs" variant="outline" pill icon={<AlertTriangle className="w-2.5 h-2.5" />}>
              {diffs.length} diff{diffs.length > 1 ? 's' : ''}
            </Badge>
          )}
        </PageHeading>
        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            icon={<RefreshCw className={`w-4 h-4 ${(syncMut.isPending || requestComparisonMut.isPending) ? 'animate-spin' : ''}`} />}
            onClick={handleSync}
            disabled={syncMut.isPending || requestComparisonMut.isPending}
            title="Sync agents from .claude/agents/ directory and compare with agent"
            className="border border-slate-700 hover:border-slate-600"
          >
            <span className="hidden sm:inline">Sync from Repo</span>
            <span className="sm:hidden">Sync</span>
          </Button>
          <Button
            variant="secondary"
            size="sm"
            icon={<Layers className="w-4 h-4" />}
            onClick={() => { refetchTemplates(); setTemplatePickerOpen(true) }}
            title="Create agent from template"
            className="border border-slate-700 hover:border-slate-600"
          >
            <span className="hidden sm:inline">From Template</span>
            <span className="sm:hidden">Tmpl</span>
          </Button>
          <Button
            variant="primary"
            size="sm"
            icon={<Plus className="w-4 h-4" />}
            onClick={openCreate}
          >
            <span className="hidden sm:inline">New Agent</span>
            <span className="sm:hidden">New</span>
          </Button>
        </div>
      </div>

      {syncMut.isSuccess && (
        <Card variant="success" className="mb-4 text-sm">
          Synced: {syncMut.data?.created ?? 0} created, {syncMut.data?.updated ?? 0} updated
        </Card>
      )}

      {/* Agent Form */}
      {formMode && (
        <form onSubmit={handleSubmit} className="mb-4 md:mb-6">
          <Card className="md:p-5">
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
                icon={<Save className="w-3.5 h-3.5" />}
                disabled={mutation.isPending || !form.name || !form.description || !form.prompt}
              >
                {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
              </Button>
            </div>
          </Card>
        </form>
      )}

      {/* Agent Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading agents...</p>}

      <div className="space-y-3">
        {agents.map(agent => {
          const diff = diffByAgentId.get(agent.id)
          return (
            <Card
              key={agent.id}
              className={`hover:border-slate-700 transition-colors ${diff ? 'border-amber-500/30' : ''}`}
            >
              <div className="flex items-start justify-between">
                <div className="flex items-start gap-3 flex-1 min-w-0">
                  <Bot className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <h3
                        className="text-sm font-semibold text-white truncate cursor-pointer hover:text-cyan-400 transition-colors"
                        onClick={() => openEdit(agent)}
                      >{agent.name}</h3>
                      {agent.isSynced && (
                        <Badge color="blue" size="xs" pill variant="outline" icon={<Cloud className="w-2.5 h-2.5" />}>
                          synced
                        </Badge>
                      )}
                      {diff && (
                        <Badge
                          color="amber"
                          size="xs"
                          pill
                          variant="outline"
                          icon={<AlertTriangle className="w-2.5 h-2.5" />}
                          className="cursor-pointer hover:bg-amber-500/20"
                          onClick={() => setDiffDialog(diff)}
                        >
                          {diffTypeLabel(diff.diffType)}
                        </Badge>
                      )}
                      {agent.model && (
                        <Badge color="gray" size="xs" pill variant="outline">
                          {agent.model}
                        </Badge>
                      )}
                      {agent.memory && (
                        <Badge color="purple" size="xs" pill variant="outline">
                          memory: {agent.memory}
                        </Badge>
                      )}
                    </div>
                    <p className="text-xs text-gray-400 mb-2">{agent.description}</p>
                    {agent.tools?.length > 0 && (
                      <div className="flex flex-wrap gap-1 mb-1">
                        {agent.tools.map(tool => (
                          <Badge key={tool} color="gray" size="xs">
                            {tool}
                          </Badge>
                        ))}
                      </div>
                    )}
                    {agent.disallowedTools?.length > 0 && (
                      <div className="flex flex-wrap gap-1 mb-1">
                        {agent.disallowedTools.map(tool => (
                          <Badge key={tool} color="red" size="xs">
                            -{tool}
                          </Badge>
                        ))}
                      </div>
                    )}
                    {agent.skills?.length > 0 && (
                      <div className="flex flex-wrap gap-1 mb-1">
                        {agent.skills.map(skill => (
                          <Badge key={skill} color="purple" size="xs">
                            {skill}
                          </Badge>
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
                  <Button
                    variant="ghost"
                    size="sm"
                    iconOnly
                    icon={<Copy className="w-3.5 h-3.5" />}
                    onClick={() => handleSaveAsTemplate(agent)}
                    title="Save as Template"
                    className="hover:text-amber-400"
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    iconOnly
                    icon={<Edit2 className="w-3.5 h-3.5" />}
                    onClick={() => openEdit(agent)}
                    title="Edit"
                    className="hover:text-cyan-400"
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    iconOnly
                    icon={<Trash2 className="w-3.5 h-3.5" />}
                    onClick={() => handleDelete(agent.id)}
                    disabled={deleteMut.isPending}
                    title="Delete"
                    className="hover:text-red-400"
                  />
                </div>
              </div>
            </Card>
          )
        })}

        {/* Agent-only diffs (exist on agent but not in server DB) */}
        {agentOnlyDiffs.map(diff => (
          <Card
            key={`agent-only-${diff.filename}`}
            className="border-amber-500/30 hover:border-amber-500/50 transition-colors cursor-pointer"
            onClick={() => setDiffDialog(diff)}
          >
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3 flex-1 min-w-0">
                <Bot className="w-5 h-5 text-amber-400 mt-0.5 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <h3 className="text-sm font-semibold text-white truncate">{diff.agentName}</h3>
                    <Badge color="amber" size="xs" pill variant="outline" icon={<AlertTriangle className="w-2.5 h-2.5" />}>
                      Agent Only
                    </Badge>
                    <Badge color="gray" size="xs" className="font-mono">{diff.filename}</Badge>
                  </div>
                  <p className="text-xs text-gray-400">
                    This agent exists on the local agent but not in the server database. Click to resolve.
                  </p>
                </div>
              </div>
            </div>
          </Card>
        ))}

        {!isLoading && agents.length === 0 && agentOnlyDiffs.length === 0 && !formMode && (
          <div className="text-center py-12 text-gray-500">
            <Bot className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No agents defined yet.</p>
            <p className="text-xs mt-1">Create agents or sync from your repository's .claude/agents/ directory.</p>
          </div>
        )}
      </div>

      {/* Diff Resolution Dialog */}
      <Modal open={!!diffDialog} onClose={() => setDiffDialog(null)} size="lg">
        <Modal.Header onClose={() => setDiffDialog(null)}>
          <div className="flex items-center gap-2">
            <AlertTriangle className="w-5 h-5 text-amber-400" />
            <h3 className="text-lg font-semibold text-white">Agent Conflict</h3>
          </div>
        </Modal.Header>
        <Modal.Body>
          {diffDialog && (
            <div className="space-y-4">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-gray-400">Agent:</span>
                <span className="text-white font-medium">{diffDialog.agentName}</span>
                <Badge color="gray" size="xs" className="font-mono">{diffDialog.filename}</Badge>
                <Badge color="amber" size="xs" variant="outline">{diffTypeLabel(diffDialog.diffType)}</Badge>
              </div>

              <div className="grid grid-cols-2 gap-3">
                {/* Server version */}
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Server className="w-4 h-4 text-blue-400" />
                    <span className="text-sm font-medium text-blue-400">Server Version</span>
                  </div>
                  <pre className="text-xs text-gray-300 font-mono bg-slate-900 rounded p-3 whitespace-pre-wrap max-h-[400px] overflow-y-auto border border-slate-700">
                    {diffDialog.serverContent || <span className="text-gray-600 italic">No server version</span>}
                  </pre>
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => handleResolveConflict(diffDialog, AgentResolutionChoice.SERVER)}
                    disabled={resolveConflictMut.isPending || diffDialog.diffType === AgentDiffType.AGENT_ONLY}
                    icon={<Server className="w-3.5 h-3.5" />}
                    className="w-full bg-blue-600 hover:bg-blue-500"
                  >
                    {resolveConflictMut.isPending ? 'Resolving...' : 'Use Server Version'}
                  </Button>
                </div>

                {/* Agent version */}
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Monitor className="w-4 h-4 text-green-400" />
                    <span className="text-sm font-medium text-green-400">Agent Version</span>
                  </div>
                  <pre className="text-xs text-gray-300 font-mono bg-slate-900 rounded p-3 whitespace-pre-wrap max-h-[400px] overflow-y-auto border border-slate-700">
                    {diffDialog.agentContent || <span className="text-gray-600 italic">No agent version</span>}
                  </pre>
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => handleResolveConflict(diffDialog, AgentResolutionChoice.AGENT)}
                    disabled={resolveConflictMut.isPending || diffDialog.diffType === AgentDiffType.SERVER_ONLY}
                    icon={<Monitor className="w-3.5 h-3.5" />}
                    className="w-full bg-green-600 hover:bg-green-500"
                  >
                    {resolveConflictMut.isPending ? 'Resolving...' : 'Use Agent Version'}
                  </Button>
                </div>
              </div>

              {resolveConflictMut.error && (
                <p className="text-red-400 text-sm">{resolveConflictMut.error.message}</p>
              )}
            </div>
          )}
        </Modal.Body>
      </Modal>

      {/* Template Picker Dialog */}
      {templatePickerOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={() => setTemplatePickerOpen(false)}>
          <div className="bg-slate-900 border border-slate-700 rounded-xl p-5 max-w-md w-full mx-4 max-h-[70vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">Select Agent Template</h3>
              <button onClick={() => setTemplatePickerOpen(false)} className="text-gray-500 hover:text-gray-300">
                <X className="w-5 h-5" />
              </button>
            </div>
            {agentTemplates.length === 0 ? (
              <p className="text-gray-500 text-sm text-center py-6">No agent templates available. Save an agent as template first.</p>
            ) : (
              <div className="space-y-2">
                {agentTemplates.map(tmpl => (
                  <button
                    key={tmpl.id}
                    onClick={() => handleCreateFromTemplate(tmpl)}
                    className="w-full text-left p-3 bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors"
                  >
                    <div className="flex items-center gap-2 mb-1">
                      <Bot className="w-4 h-4 text-cyan-400" />
                      <span className="text-sm font-medium text-white">{tmpl.name}</span>
                    </div>
                    {tmpl.description && (
                      <p className="text-xs text-gray-400 ml-6">{tmpl.description}</p>
                    )}
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
              <button onClick={() => setSaveTemplateDialog(null)} className="text-gray-500 hover:text-gray-300">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="space-y-3">
              <FormField label="Template Name">
                <Input
                  value={saveTemplateDialog.name}
                  onChange={e => setSaveTemplateDialog(prev => prev ? { ...prev, name: e.target.value } : null)}
                  className="focus:border-amber-500"
                />
              </FormField>
              <FormField label="Template Description">
                <Input
                  value={saveTemplateDialog.description}
                  onChange={e => setSaveTemplateDialog(prev => prev ? { ...prev, description: e.target.value } : null)}
                  className="focus:border-amber-500"
                />
              </FormField>
              <Checkbox
                color="amber"
                label="Include referenced Skills as templates"
                checked={saveTemplateDialog.includeSkills}
                onChange={e => setSaveTemplateDialog(prev => prev ? { ...prev, includeSkills: e.target.checked } : null)}
                className="text-gray-300"
              />
            </div>
            {saveTemplateMut.error && (
              <p className="text-red-400 text-sm mt-3">{saveTemplateMut.error.message}</p>
            )}
            {saveTemplateMut.isSuccess && (
              <p className="text-green-400 text-sm mt-3">Template saved successfully!</p>
            )}
            <div className="flex justify-end gap-2 mt-4">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setSaveTemplateDialog(null)}
              >
                Cancel
              </Button>
              <Button
                variant="danger"
                size="sm"
                icon={<Save className="w-3.5 h-3.5" />}
                onClick={handleSaveTemplateSubmit}
                disabled={saveTemplateMut.isPending || !saveTemplateDialog.name}
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
