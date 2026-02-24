import { useState } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createWorkflow, updateWorkflow } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { listAgents } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { X, Plus, Trash2, Bot, ChevronUp, ChevronDown } from 'lucide-react'

interface StatusDraft {
  key: string
  id: string
  name: string
  order: number
  isInitial: boolean
  isTerminal: boolean
  transitionsTo: string[] // keys
  agentId: string // reference to AgentDefinition
}

interface AgentConfigDraft {
  key: string
  id: string
  statusKey: string
  name: string
  description: string
  instructions: string
}

let nextKey = 0
function genKey() {
  return `k${++nextKey}`
}

function workflowToDrafts(wf: Workflow) {
  const idToKey = new Map<string, string>()
  const statusDrafts: StatusDraft[] = wf.statuses.map((s) => {
    const key = genKey()
    idToKey.set(s.id, key)
    return {
      key,
      id: s.id,
      name: s.name,
      order: s.order,
      isInitial: s.isInitial,
      isTerminal: s.isTerminal,
      transitionsTo: [], // fill below
      agentId: s.agentId ?? '',
    }
  })
  // Resolve transitions from IDs to keys
  for (const s of wf.statuses) {
    const draft = statusDrafts.find((d) => d.id === s.id)!
    draft.transitionsTo = s.transitionsTo.map((id) => idToKey.get(id)!).filter(Boolean)
  }

  // Legacy: if a status doesn't have agentId but has a matching AgentConfig, use it for display.
  for (const cfg of wf.agentConfigs) {
    const draft = statusDrafts.find((d) => d.id === cfg.workflowStatusId)
    if (draft && !draft.agentId) {
      // Legacy agent configs don't map to Agent entities, so leave agentId empty.
      // They'll be shown as "Legacy" in the UI.
    }
  }

  const agentDrafts: AgentConfigDraft[] = wf.agentConfigs.map((a) => ({
    key: genKey(),
    id: a.id,
    statusKey: idToKey.get(a.workflowStatusId) ?? '',
    name: a.name,
    description: a.description,
    instructions: a.instructions,
  }))

  return { statusDrafts, agentDrafts }
}

export function WorkflowForm({
  projectId,
  workflow,
  onClose,
  onSaved,
}: {
  projectId: string
  workflow?: Workflow
  onClose: () => void
  onSaved: () => void
}) {
  const isEdit = !!workflow

  const initial = workflow
    ? workflowToDrafts(workflow)
    : (() => {
        const kDraft = genKey()
        const kDevelop = genKey()
        const kReview = genKey()
        const kTest = genKey()
        const kClosed = genKey()
        return {
          statusDrafts: [
            { key: kDraft, id: '', name: 'Draft', order: 0, isInitial: true, isTerminal: false, transitionsTo: [kDevelop], agentId: '' },
            { key: kDevelop, id: '', name: 'Develop', order: 1, isInitial: false, isTerminal: false, transitionsTo: [kReview, kDraft], agentId: '' },
            { key: kReview, id: '', name: 'Review', order: 2, isInitial: false, isTerminal: false, transitionsTo: [kTest], agentId: '' },
            { key: kTest, id: '', name: 'Test', order: 3, isInitial: false, isTerminal: false, transitionsTo: [kClosed], agentId: '' },
            { key: kClosed, id: '', name: 'Closed', order: 4, isInitial: false, isTerminal: true, transitionsTo: [], agentId: '' },
          ],
          agentDrafts: [],
        }
      })()

  const [name, setName] = useState(workflow?.name ?? '')
  const [description, setDescription] = useState(workflow?.description ?? '')
  const [statuses, setStatuses] = useState<StatusDraft[]>(initial.statusDrafts)
  const [agentConfigs] = useState<AgentConfigDraft[]>(initial.agentDrafts)

  // Fetch available agents for the project.
  const { data: agentsData } = useQuery(listAgents, { projectId })
  const agents = agentsData?.agents ?? []

  const createMutation = useMutation(createWorkflow)
  const updateMutation = useMutation(updateWorkflow)
  const mutation = isEdit ? updateMutation : createMutation

  const addStatus = () => {
    setStatuses((prev) => [
      ...prev,
      { key: genKey(), id: '', name: '', order: prev.length, isInitial: false, isTerminal: false, transitionsTo: [], agentId: '' },
    ])
  }

  const removeStatus = (key: string) => {
    setStatuses((prev) =>
      prev
        .filter((s) => s.key !== key)
        .map((s, i) => ({
          ...s,
          order: i,
          transitionsTo: s.transitionsTo.filter((k) => k !== key),
        })),
    )
  }

  const moveStatus = (index: number, direction: -1 | 1) => {
    setStatuses((prev) => {
      const next = [...prev]
      const target = index + direction
      if (target < 0 || target >= next.length) return prev
      ;[next[index], next[target]] = [next[target], next[index]]
      return next.map((s, i) => ({ ...s, order: i }))
    })
  }

  const updateStatus = (key: string, patch: Partial<StatusDraft>) => {
    setStatuses((prev) =>
      prev
        .map((s) => (s.key === key ? { ...s, ...patch } : s))
        .map((s) => {
          if (patch.isInitial && key !== s.key) return { ...s, isInitial: false }
          return s
        }),
    )
  }

  const toggleTransition = (fromKey: string, toKey: string) => {
    setStatuses((prev) =>
      prev.map((s) => {
        if (s.key !== fromKey) return s
        const has = s.transitionsTo.includes(toKey)
        return {
          ...s,
          transitionsTo: has
            ? s.transitionsTo.filter((k) => k !== toKey)
            : [...s.transitionsTo, toKey],
        }
      }),
    )
  }

  const buildProtoPayload = () => {
    const keyToId = new Map<string, string>()
    statuses.forEach((s, i) => {
      keyToId.set(s.key, s.id || `status-${i}`)
    })

    const protoStatuses = statuses.map((s) => ({
      id: keyToId.get(s.key)!,
      name: s.name,
      order: s.order,
      isInitial: s.isInitial,
      isTerminal: s.isTerminal,
      transitionsTo: s.transitionsTo.map((k) => keyToId.get(k)!).filter(Boolean),
      agentId: s.agentId,
    }))

    // Keep legacy agent configs for backward compatibility with existing statuses
    // that don't have agentId set. Only include legacy configs for statuses without agentId.
    const statusesWithAgentId = new Set(
      statuses.filter(s => s.agentId).map(s => keyToId.get(s.key)!)
    )
    const protoAgentConfigs = agentConfigs
      .filter(a => {
        const statusId = keyToId.get(a.statusKey)
        return statusId && !statusesWithAgentId.has(statusId)
      })
      .map((a, i) => ({
        id: a.id || `agent-config-${i}`,
        workflowStatusId: keyToId.get(a.statusKey)!,
        name: a.name,
        description: a.description,
        instructions: a.instructions,
        allowedTools: [] as string[],
      }))

    return { protoStatuses, protoAgentConfigs }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const { protoStatuses, protoAgentConfigs } = buildProtoPayload()

    if (isEdit) {
      updateMutation.mutate(
        {
          id: workflow!.id,
          name,
          description,
          statuses: protoStatuses,
          agentConfigs: protoAgentConfigs,
        },
        { onSuccess: onSaved },
      )
    } else {
      createMutation.mutate(
        {
          projectId,
          name,
          description,
          statuses: protoStatuses,
          agentConfigs: protoAgentConfigs,
        },
        { onSuccess: onSaved },
      )
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex-1 overflow-y-auto p-6">
      <div className="max-w-3xl mx-auto">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-bold text-white">
            {isEdit ? 'Edit Workflow' : 'Create Workflow'}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="text-gray-500 hover:text-gray-300 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Basic info */}
        <div className="space-y-3 mb-8">
          <div>
            <label className="block text-sm text-gray-400 mb-1">Name *</label>
            <input
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
              placeholder="e.g. Bug Fix Workflow"
            />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Description</label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
              placeholder="Workflow description"
            />
          </div>
        </div>

        {/* Statuses */}
        <div className="mb-8">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
              Statuses
            </h3>
            <button
              type="button"
              onClick={addStatus}
              className="flex items-center gap-1 text-xs text-cyan-400 hover:text-cyan-300 transition-colors"
            >
              <Plus className="w-3.5 h-3.5" />
              Add Status
            </button>
          </div>
          <div className="space-y-3">
            {statuses.map((s, index) => {
              const selectedAgent = agents.find(a => a.id === s.agentId)
              // Check for legacy agent config
              const legacyAgent = agentConfigs.find(a => a.statusKey === s.key)
              return (
                <div
                  key={s.key}
                  className="bg-slate-900 border border-slate-800 rounded-lg p-4"
                >
                  <div className="flex items-center gap-3 mb-3">
                    <div className="flex flex-col -my-1">
                      <button
                        type="button"
                        onClick={() => moveStatus(index, -1)}
                        disabled={index === 0}
                        className="text-gray-600 hover:text-white disabled:opacity-20 disabled:cursor-not-allowed transition-colors"
                      >
                        <ChevronUp className="w-4 h-4" />
                      </button>
                      <button
                        type="button"
                        onClick={() => moveStatus(index, 1)}
                        disabled={index === statuses.length - 1}
                        className="text-gray-600 hover:text-white disabled:opacity-20 disabled:cursor-not-allowed transition-colors"
                      >
                        <ChevronDown className="w-4 h-4" />
                      </button>
                    </div>
                    <input
                      type="text"
                      required
                      value={s.name}
                      onChange={(e) => updateStatus(s.key, { name: e.target.value })}
                      className="flex-1 px-3 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-sm focus:outline-none focus:border-cyan-500"
                      placeholder="Status name"
                    />
                    <label className="flex items-center gap-1.5 text-xs text-gray-400 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={s.isInitial}
                        onChange={(e) => updateStatus(s.key, { isInitial: e.target.checked })}
                        className="accent-cyan-500"
                      />
                      Initial
                    </label>
                    <label className="flex items-center gap-1.5 text-xs text-gray-400 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={s.isTerminal}
                        onChange={(e) => updateStatus(s.key, { isTerminal: e.target.checked })}
                        className="accent-cyan-500"
                      />
                      Terminal
                    </label>
                    {statuses.length > 1 && (
                      <button
                        type="button"
                        onClick={() => removeStatus(s.key)}
                        className="text-gray-600 hover:text-red-400 transition-colors"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    )}
                  </div>

                  {/* Transitions */}
                  <div className="mb-3">
                    <span className="text-xs text-gray-500 mr-2">Transitions to:</span>
                    <div className="inline-flex gap-1 flex-wrap">
                      {statuses
                        .filter((other) => other.key !== s.key)
                        .map((other) => {
                          const active = s.transitionsTo.includes(other.key)
                          return (
                            <button
                              key={other.key}
                              type="button"
                              onClick={() => toggleTransition(s.key, other.key)}
                              className={`px-2 py-0.5 text-xs rounded transition-colors ${
                                active
                                  ? 'bg-cyan-500/20 text-cyan-400 border border-cyan-500/30'
                                  : 'bg-slate-800 text-gray-500 border border-slate-700 hover:text-gray-300'
                              }`}
                            >
                              {other.name || '(unnamed)'}
                            </button>
                          )
                        })}
                    </div>
                  </div>

                  {/* Agent Assignment (dropdown) */}
                  {!s.isTerminal && (
                    <div className="bg-slate-800/50 border border-slate-700 rounded p-3 mt-2">
                      <div className="flex items-center gap-2 mb-2">
                        <Bot className="w-3.5 h-3.5 text-cyan-400" />
                        <span className="text-xs text-cyan-400">Assigned Agent</span>
                      </div>
                      <select
                        value={s.agentId}
                        onChange={(e) => updateStatus(s.key, { agentId: e.target.value })}
                        className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
                      >
                        <option value="">No agent (manual status)</option>
                        {agents.map(agent => (
                          <option key={agent.id} value={agent.id}>
                            {agent.name} — {agent.description}
                          </option>
                        ))}
                      </select>
                      {selectedAgent && (
                        <div className="mt-2 text-[11px] text-gray-500">
                          <span className="text-gray-400">Model:</span> {selectedAgent.model || 'inherit'}
                          {selectedAgent.tools.length > 0 && (
                            <>
                              {' · '}
                              <span className="text-gray-400">Tools:</span> {selectedAgent.tools.join(', ')}
                            </>
                          )}
                        </div>
                      )}
                      {!s.agentId && legacyAgent && (
                        <div className="mt-2 text-[11px] text-amber-500/70">
                          Legacy agent config: {legacyAgent.name} (will be preserved)
                        </div>
                      )}
                      {agents.length === 0 && (
                        <p className="mt-2 text-[11px] text-gray-600">
                          No agents defined yet. Create agents in the Agents page first.
                        </p>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>

        {mutation.error && (
          <p className="text-red-400 text-sm mb-4">{mutation.error.message}</p>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 text-sm text-gray-400 hover:text-white transition-colors"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={mutation.isPending || !name || statuses.length === 0}
            className="px-4 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
          >
            {mutation.isPending
              ? isEdit ? 'Saving...' : 'Creating...'
              : isEdit ? 'Save' : 'Create Workflow'}
          </button>
        </div>
      </div>
    </form>
  )
}
