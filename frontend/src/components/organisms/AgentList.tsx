import { useState, useMemo, useCallback, useEffect, useRef } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listAgents, createAgent, updateAgent, deleteAgent, syncAgentsFromDir } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
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
import { Bot, Plus, Layers, AlertTriangle } from 'lucide-react'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { useTemplateIntegration } from '@/hooks/useTemplateIntegration.ts'
import { Button, Badge, Checkbox } from '../atoms/index.ts'
import { Card, PageHeading, EmptyState, SyncButton } from '../molecules/index.ts'
import { type AgentFormData, emptyForm, agentToForm } from './AgentListUtils.ts'
import { AgentCard, AgentOnlyDiffCard } from './AgentCard.tsx'
import { AgentFormModal } from './AgentFormModal.tsx'
import { DiffResolutionModal } from './DiffResolutionModal.tsx'
import { SaveAsTemplateDialog } from './SaveAsTemplateDialog.tsx'
import { TemplatePickerDialog } from './TemplatePickerDialog.tsx'

export function AgentList({ projectId, editAgentId, mode }: { projectId: string; editAgentId?: string; mode?: 'create' }) {
  const navigate = useNavigate()
  const { data, refetch, isLoading } = useQuery(listAgents, { projectId })
  const createMut = useMutation(createAgent)
  const updateMut = useMutation(updateAgent)
  const deleteMut = useMutation(deleteAgent)
  const syncMut = useMutation(syncAgentsFromDir)

  // Agent comparison
  const requestComparisonMut = useMutation(requestAgentComparison)
  const { data: comparisonData, refetch: refetchComparison } = useQuery(getAgentComparison, { projectId })
  const resolveConflictMut = useMutation(resolveAgentConflict)

  // Derive form mode from URL search params
  const formMode: 'create' | 'edit' | null = mode === 'create' ? 'create' : editAgentId ? 'edit' : null
  const editingId: string | null = editAgentId ?? null

  const [form, setForm] = useState<AgentFormData>(emptyForm)

  // Template integration
  const { saveDialog, setSaveDialog, openSaveDialog, pickerOpen, openPicker, closePicker } = useTemplateIntegration()
  const [includeSkills, setIncludeSkills] = useState(false)

  // Diff resolution dialog state
  const [diffDialog, setDiffDialog] = useState<AgentDiff | null>(null)

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
    openSaveDialog(agent)
    setIncludeSkills(agent.skills?.length > 0)
  }

  const handleCreateFromTemplate = (tmpl: Template) => {
    if (!tmpl.agentConfig) return
    closePicker()
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
          <SyncButton
            onClick={handleSync}
            isPending={syncMut.isPending || requestComparisonMut.isPending}
            title="Sync agents from .claude/agents/ directory and compare with agent"
          />
          <Button
            variant="secondary"
            size="sm"
            icon={<Layers className="w-4 h-4" />}
            onClick={openPicker}
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
          <EmptyState
            icon={Bot}
            message="No agents defined yet."
            hint="Create agents or sync from your repository's .claude/agents/ directory."
          />
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
      <TemplatePickerDialog
        open={pickerOpen}
        entityType="agent"
        entityLabel="Agent"
        icon={Bot}
        iconColor="text-cyan-400"
        onSelect={handleCreateFromTemplate}
        onClose={closePicker}
      />

      {/* Save as Template Dialog */}
      <SaveAsTemplateDialog
        dialog={saveDialog}
        setDialog={setSaveDialog}
        entityType="agent"
        onSaved={() => {}}
        extraMutationParams={{ includeDependentSkills: includeSkills }}
        extraFields={
          <Checkbox
            color="amber"
            label="Include referenced Skills as templates"
            checked={includeSkills}
            onChange={e => setIncludeSkills(e.target.checked)}
            className="text-gray-300"
          />
        }
      />
    </div>
  )
}
