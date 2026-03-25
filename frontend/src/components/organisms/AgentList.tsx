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
import { Bot, Plus, RefreshCw, X, Save, Layers, AlertTriangle } from 'lucide-react'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { Button } from '../atoms/index.ts'
import { Input, Badge, Checkbox } from '../atoms/index.ts'
import { Card, FormField, PageHeading } from '../molecules/index.ts'
import { type AgentFormData, emptyForm, agentToForm } from './AgentListUtils.ts'
import { AgentCard, AgentOnlyDiffCard } from './AgentCard.tsx'
import { AgentFormModal } from './AgentFormModal.tsx'
import { DiffResolutionModal } from './DiffResolutionModal.tsx'

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

    } else if (editAgentId) {
      const agent = agents.find(a => a.id === editAgentId)
      if (agent) {
        setForm(agentToForm(agent))
  
      }
    } else {
      setForm(emptyForm)

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
        <AgentFormModal
          formMode={formMode}
          form={form}
          setForm={setForm}
          onSubmit={handleSubmit}
          onClose={closeForm}
          isPending={mutation.isPending}
          error={mutation.error}
        />
      )}

      {/* Agent Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading agents...</p>}

      <div className="space-y-3">
        {agents.map(agent => (
          <AgentCard
            key={agent.id}
            agent={agent}
            diff={diffByAgentId.get(agent.id)}
            onEdit={() => openEdit(agent)}
            onDelete={() => handleDelete(agent.id)}
            onSaveAsTemplate={() => handleSaveAsTemplate(agent)}
            onShowDiff={setDiffDialog}
            isDeleting={deleteMut.isPending}
          />
        ))}

        {/* Agent-only diffs (exist on agent but not in server DB) */}
        {agentOnlyDiffs.map(diff => (
          <AgentOnlyDiffCard
            key={`agent-only-${diff.filename}`}
            diff={diff}
            onClick={() => setDiffDialog(diff)}
          />
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
      <DiffResolutionModal
        diff={diffDialog}
        onClose={() => setDiffDialog(null)}
        onResolve={handleResolveConflict}
        isPending={resolveConflictMut.isPending}
        error={resolveConflictMut.error}
      />

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
