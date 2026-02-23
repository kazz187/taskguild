import { useState } from 'react'
import { useMutation } from '@connectrpc/connect-query'
import { createWorkflow, updateWorkflow } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
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
}

interface AgentConfigDraft {
  key: string
  id: string
  statusKey: string
  name: string
  description: string
  instructions: string
  permissionMode: string
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
    }
  })
  // Resolve transitions from IDs to keys
  for (const s of wf.statuses) {
    const draft = statusDrafts.find((d) => d.id === s.id)!
    draft.transitionsTo = s.transitionsTo.map((id) => idToKey.get(id)!).filter(Boolean)
  }

  const agentDrafts: AgentConfigDraft[] = wf.agentConfigs.map((a) => ({
    key: genKey(),
    id: a.id,
    statusKey: idToKey.get(a.workflowStatusId) ?? '',
    name: a.name,
    description: a.description,
    instructions: a.instructions,
    permissionMode: a.permissionMode ?? '',
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
    : {
        statusDrafts: [
          { key: genKey(), id: '', name: 'Open', order: 0, isInitial: true, isTerminal: false, transitionsTo: [] },
          { key: genKey(), id: '', name: 'In Progress', order: 1, isInitial: false, isTerminal: false, transitionsTo: [] },
          { key: genKey(), id: '', name: 'Done', order: 2, isInitial: false, isTerminal: true, transitionsTo: [] },
        ],
        agentDrafts: [],
      }

  const [name, setName] = useState(workflow?.name ?? '')
  const [description, setDescription] = useState(workflow?.description ?? '')
  const [statuses, setStatuses] = useState<StatusDraft[]>(initial.statusDrafts)
  const [agentConfigs, setAgentConfigs] = useState<AgentConfigDraft[]>(initial.agentDrafts)

  const createMutation = useMutation(createWorkflow)
  const updateMutation = useMutation(updateWorkflow)
  const mutation = isEdit ? updateMutation : createMutation

  const addStatus = () => {
    setStatuses((prev) => [
      ...prev,
      { key: genKey(), id: '', name: '', order: prev.length, isInitial: false, isTerminal: false, transitionsTo: [] },
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
    setAgentConfigs((prev) => prev.filter((a) => a.statusKey !== key))
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

  const addAgentConfig = (statusKey: string) => {
    setAgentConfigs((prev) => [
      ...prev,
      { key: genKey(), id: '', statusKey, name: '', description: '', instructions: '', permissionMode: '' },
    ])
  }

  const removeAgentConfig = (key: string) => {
    setAgentConfigs((prev) => prev.filter((a) => a.key !== key))
  }

  const updateAgentConfig = (key: string, patch: Partial<AgentConfigDraft>) => {
    setAgentConfigs((prev) =>
      prev.map((a) => (a.key === key ? { ...a, ...patch } : a)),
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
    }))

    const protoAgentConfigs = agentConfigs.map((a, i) => ({
      id: a.id || `agent-config-${i}`,
      workflowStatusId: keyToId.get(a.statusKey)!,
      name: a.name,
      description: a.description,
      instructions: a.instructions,
      allowedTools: [] as string[],
      useWorktree: false,
      permissionMode: a.permissionMode,
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
              const agentCfg = agentConfigs.find((a) => a.statusKey === s.key)
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

                  {/* Agent config */}
                  {agentCfg ? (
                    <div className="bg-slate-800/50 border border-slate-700 rounded p-3 mt-2">
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-xs text-cyan-400 flex items-center gap-1">
                          <Bot className="w-3.5 h-3.5" />
                          Agent Config
                        </span>
                        <button
                          type="button"
                          onClick={() => removeAgentConfig(agentCfg.key)}
                          className="text-gray-600 hover:text-red-400 transition-colors"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                      <div className="space-y-2">
                        <input
                          type="text"
                          required
                          value={agentCfg.name}
                          onChange={(e) => updateAgentConfig(agentCfg.key, { name: e.target.value })}
                          className="w-full px-2 py-1 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
                          placeholder="Agent name (e.g. Code Reviewer)"
                        />
                        <textarea
                          value={agentCfg.instructions}
                          onChange={(e) => updateAgentConfig(agentCfg.key, { instructions: e.target.value })}
                          className="w-full px-2 py-1 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500 min-h-[60px]"
                          placeholder="Agent instructions / system prompt"
                        />
                        <div>
                          <label className="block text-xs text-gray-500 mb-1">Permission Mode</label>
                          <select
                            value={agentCfg.permissionMode}
                            onChange={(e) => updateAgentConfig(agentCfg.key, { permissionMode: e.target.value })}
                            className="w-full px-2 py-1 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500"
                          >
                            <option value="">Default (ask for permission)</option>
                            <option value="acceptEdits">Accept Edits (auto-approve file changes)</option>
                            <option value="bypassPermissions">Bypass Permissions (auto-approve all)</option>
                          </select>
                        </div>
                      </div>
                    </div>
                  ) : (
                    !s.isTerminal && (
                      <button
                        type="button"
                        onClick={() => addAgentConfig(s.key)}
                        className="flex items-center gap-1 text-xs text-gray-500 hover:text-cyan-400 transition-colors mt-1"
                      >
                        <Bot className="w-3.5 h-3.5" />
                        Add Agent Config
                      </button>
                    )
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
