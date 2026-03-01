import { useState } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createWorkflow, updateWorkflow } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { listAgents } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
import { listSkills } from '@taskguild/proto/taskguild/v1/skill-SkillService_connectquery.ts'
import { listScripts } from '@taskguild/proto/taskguild/v1/script-ScriptService_connectquery.ts'
import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { HookTrigger, HookActionType } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { X, Plus, Trash2, Bot, ChevronUp, ChevronDown, Zap } from 'lucide-react'

interface HookDraft {
  key: string
  id: string
  skillId: string // legacy: kept for backward compat
  actionType: HookActionType
  actionId: string
  trigger: HookTrigger
  order: number
  name: string
}

interface StatusDraft {
  key: string
  id: string
  name: string
  order: number
  isInitial: boolean
  isTerminal: boolean
  transitionsTo: string[] // keys
  agentId: string // reference to AgentDefinition
  hooks: HookDraft[]
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
      hooks: (s.hooks ?? []).map((h) => ({
        key: genKey(),
        id: h.id,
        skillId: h.skillId,
        actionType: h.actionType || (h.skillId ? HookActionType.SKILL : HookActionType.UNSPECIFIED),
        actionId: h.actionId || h.skillId || '',
        trigger: h.trigger,
        order: h.order,
        name: h.name,
      })),
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
            { key: kDraft, id: '', name: 'Draft', order: 0, isInitial: true, isTerminal: false, transitionsTo: [kDevelop], agentId: '', hooks: [] },
            { key: kDevelop, id: '', name: 'Develop', order: 1, isInitial: false, isTerminal: false, transitionsTo: [kReview, kDraft], agentId: '', hooks: [] },
            { key: kReview, id: '', name: 'Review', order: 2, isInitial: false, isTerminal: false, transitionsTo: [kTest], agentId: '', hooks: [] },
            { key: kTest, id: '', name: 'Test', order: 3, isInitial: false, isTerminal: false, transitionsTo: [kClosed], agentId: '', hooks: [] },
            { key: kClosed, id: '', name: 'Closed', order: 4, isInitial: false, isTerminal: true, transitionsTo: [], agentId: '', hooks: [] },
          ],
          agentDrafts: [],
        }
      })()

  const [name, setName] = useState(workflow?.name ?? '')
  const [description, setDescription] = useState(workflow?.description ?? '')
  const [defaultPermissionMode, setDefaultPermissionMode] = useState(workflow?.defaultPermissionMode ?? '')
  const [defaultUseWorktree, setDefaultUseWorktree] = useState(workflow?.defaultUseWorktree ?? false)
  const [statuses, setStatuses] = useState<StatusDraft[]>(initial.statusDrafts)
  const [agentConfigs] = useState<AgentConfigDraft[]>(initial.agentDrafts)

  // Fetch available agents for the project.
  const { data: agentsData } = useQuery(listAgents, { projectId })
  const agents = agentsData?.agents ?? []

  // Fetch available skills for the project.
  const { data: skillsData } = useQuery(listSkills, { projectId })
  const skills = skillsData?.skills ?? []

  // Fetch available scripts for the project.
  const { data: scriptsData } = useQuery(listScripts, { projectId })
  const scripts = scriptsData?.scripts ?? []

  const [validationError, setValidationError] = useState('')

  const createMutation = useMutation(createWorkflow)
  const updateMutation = useMutation(updateWorkflow)
  const mutation = isEdit ? updateMutation : createMutation

  const addHook = (statusKey: string) => {
    setStatuses((prev) =>
      prev.map((s) =>
        s.key === statusKey
          ? {
              ...s,
              hooks: [
                ...s.hooks,
                {
                  key: genKey(),
                  id: '',
                  skillId: '',
                  actionType: HookActionType.SKILL,
                  actionId: '',
                  trigger: HookTrigger.BEFORE_TASK_EXECUTION,
                  order: s.hooks.length,
                  name: '',
                },
              ],
            }
          : s,
      ),
    )
  }

  const removeHook = (statusKey: string, hookKey: string) => {
    setStatuses((prev) =>
      prev.map((s) =>
        s.key === statusKey
          ? { ...s, hooks: s.hooks.filter((h) => h.key !== hookKey).map((h, i) => ({ ...h, order: i })) }
          : s,
      ),
    )
  }

  const moveHook = (statusKey: string, hookIndex: number, direction: -1 | 1) => {
    setStatuses((prev) =>
      prev.map((s) => {
        if (s.key !== statusKey) return s
        const next = [...s.hooks]
        const target = hookIndex + direction
        if (target < 0 || target >= next.length) return s
        ;[next[hookIndex], next[target]] = [next[target], next[hookIndex]]
        return { ...s, hooks: next.map((h, i) => ({ ...h, order: i })) }
      }),
    )
  }

  const updateHook = (statusKey: string, hookKey: string, patch: Partial<HookDraft>) => {
    setStatuses((prev) =>
      prev.map((s) =>
        s.key === statusKey
          ? { ...s, hooks: s.hooks.map((h) => (h.key === hookKey ? { ...h, ...patch } : h)) }
          : s,
      ),
    )
  }

  const addStatus = () => {
    setStatuses((prev) => [
      ...prev,
      { key: genKey(), id: '', name: '', order: prev.length, isInitial: false, isTerminal: false, transitionsTo: [], agentId: '', hooks: [] },
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
    statuses.forEach((s) => {
      keyToId.set(s.key, s.name)
    })

    const protoStatuses = statuses.map((s) => ({
      id: keyToId.get(s.key)!,
      name: s.name,
      order: s.order,
      isInitial: s.isInitial,
      isTerminal: s.isTerminal,
      transitionsTo: s.transitionsTo.map((k) => keyToId.get(k)!).filter(Boolean),
      agentId: s.agentId,
      hooks: s.hooks
        .filter((h) => h.actionId)
        .map((h) => ({
          id: h.id,
          skillId: h.actionType === HookActionType.SKILL ? h.actionId : '',
          actionType: h.actionType,
          actionId: h.actionId,
          trigger: h.trigger,
          order: h.order,
          name: h.name,
        })),
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
    setValidationError('')

    const alphanumeric = /^[a-zA-Z0-9]+$/
    for (const s of statuses) {
      if (!s.name) {
        setValidationError('All status names must be non-empty.')
        return
      }
      if (!alphanumeric.test(s.name)) {
        setValidationError(`Status name "${s.name}" must be alphanumeric only.`)
        return
      }
    }
    const nameSet = new Set<string>()
    for (const s of statuses) {
      if (nameSet.has(s.name)) {
        setValidationError(`Duplicate status name "${s.name}".`)
        return
      }
      nameSet.add(s.name)
    }

    const { protoStatuses, protoAgentConfigs } = buildProtoPayload()

    if (isEdit) {
      updateMutation.mutate(
        {
          id: workflow!.id,
          name,
          description,
          statuses: protoStatuses,
          agentConfigs: protoAgentConfigs,
          defaultPermissionMode,
          defaultUseWorktree,
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
          defaultPermissionMode,
          defaultUseWorktree,
        },
        { onSuccess: onSaved },
      )
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex-1 overflow-y-auto p-4 md:p-6">
      <div className="max-w-3xl mx-auto">
        <div className="flex items-center justify-between mb-4 md:mb-6">
          <h2 className="text-lg md:text-xl font-bold text-white">
            {isEdit ? 'Edit Workflow' : 'Create Workflow'}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="text-gray-500 hover:text-gray-300 transition-colors p-1"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Basic info */}
        <div className="space-y-3 mb-6 md:mb-8">
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

        {/* Task Defaults */}
        <div className="mb-6 md:mb-8">
          <h3 className="text-sm font-semibold text-gray-300 uppercase tracking-wide mb-3">
            Task Defaults
          </h3>
          <div className="space-y-3">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Permission Mode</label>
              <select
                value={defaultPermissionMode}
                onChange={(e) => setDefaultPermissionMode(e.target.value)}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
              >
                <option value="">Default (ask for permission)</option>
                <option value="acceptEdits">Accept Edits (auto-approve file changes)</option>
                <option value="bypassPermissions">Bypass Permissions (auto-approve all)</option>
              </select>
            </div>
            <label className="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
              <input
                type="checkbox"
                checked={defaultUseWorktree}
                onChange={(e) => setDefaultUseWorktree(e.target.checked)}
                className="accent-cyan-500"
              />
              Use Worktree (isolate changes in a git worktree)
            </label>
          </div>
        </div>

        {/* Statuses */}
        <div className="mb-6 md:mb-8">
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
                  className="bg-slate-900 border border-slate-800 rounded-lg p-3 md:p-4"
                >
                  <div className="flex items-center gap-2 md:gap-3 mb-3">
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
                      pattern="[a-zA-Z0-9]+"
                      value={s.name}
                      onChange={(e) => {
                        const v = e.target.value.replace(/[^a-zA-Z0-9]/g, '')
                        updateStatus(s.key, { name: v })
                      }}
                      className="flex-1 min-w-0 px-2 md:px-3 py-1.5 bg-slate-800 border border-slate-700 rounded text-white text-sm focus:outline-none focus:border-cyan-500"
                      placeholder="Status name (alphanumeric)"
                    />
                    <div className="flex items-center gap-2 shrink-0">
                      <label className="flex items-center gap-1 text-xs text-gray-400 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={s.isInitial}
                          onChange={(e) => updateStatus(s.key, { isInitial: e.target.checked })}
                          className="accent-cyan-500"
                        />
                        <span className="hidden sm:inline">Initial</span>
                        <span className="sm:hidden">I</span>
                      </label>
                      <label className="flex items-center gap-1 text-xs text-gray-400 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={s.isTerminal}
                          onChange={(e) => updateStatus(s.key, { isTerminal: e.target.checked })}
                          className="accent-cyan-500"
                        />
                        <span className="hidden sm:inline">Terminal</span>
                        <span className="sm:hidden">T</span>
                      </label>
                      {statuses.length > 1 && (
                        <button
                          type="button"
                          onClick={() => removeStatus(s.key)}
                          className="text-gray-600 hover:text-red-400 transition-colors p-1"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      )}
                    </div>
                  </div>

                  {/* Transitions */}
                  <div className="mb-3">
                    <span className="text-xs text-gray-500 mr-2 block sm:inline mb-1 sm:mb-0">Transitions to:</span>
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
                    <div className="bg-slate-800/50 border border-slate-700 rounded p-2.5 md:p-3 mt-2">
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

                  {/* Hooks */}
                  {!s.isTerminal && (
                    <div className="bg-slate-800/50 border border-slate-700 rounded p-2.5 md:p-3 mt-2">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          <Zap className="w-3.5 h-3.5 text-amber-400" />
                          <span className="text-xs text-amber-400">Hooks</span>
                        </div>
                        <button
                          type="button"
                          onClick={() => addHook(s.key)}
                          className="flex items-center gap-1 text-[11px] text-amber-400 hover:text-amber-300 transition-colors"
                        >
                          <Plus className="w-3 h-3" />
                          Add Hook
                        </button>
                      </div>
                      {s.hooks.length === 0 && (
                        <p className="text-[11px] text-gray-600">No hooks configured.</p>
                      )}
                      <div className="space-y-2">
                        {s.hooks.map((h, hi) => (
                          <div key={h.key} className="flex items-center gap-2 bg-slate-900/50 rounded p-2">
                            <div className="flex flex-col -my-1">
                              <button
                                type="button"
                                onClick={() => moveHook(s.key, hi, -1)}
                                disabled={hi === 0}
                                className="text-gray-600 hover:text-white disabled:opacity-20 disabled:cursor-not-allowed transition-colors"
                              >
                                <ChevronUp className="w-3 h-3" />
                              </button>
                              <button
                                type="button"
                                onClick={() => moveHook(s.key, hi, 1)}
                                disabled={hi === s.hooks.length - 1}
                                className="text-gray-600 hover:text-white disabled:opacity-20 disabled:cursor-not-allowed transition-colors"
                              >
                                <ChevronDown className="w-3 h-3" />
                              </button>
                            </div>
                            <select
                              value={h.trigger}
                              onChange={(e) =>
                                updateHook(s.key, h.key, { trigger: Number(e.target.value) as HookTrigger })
                              }
                              className="px-1.5 py-1 bg-slate-800 border border-slate-700 rounded text-white text-[11px] focus:outline-none focus:border-cyan-500"
                            >
                              <option value={HookTrigger.BEFORE_TASK_EXECUTION}>Before Task</option>
                              <option value={HookTrigger.AFTER_TASK_EXECUTION}>After Task</option>
                              <option value={HookTrigger.AFTER_WORKTREE_CREATION}>After Worktree</option>
                            </select>
                            <select
                              value={h.actionType}
                              onChange={(e) => {
                                const newType = Number(e.target.value) as HookActionType
                                updateHook(s.key, h.key, {
                                  actionType: newType,
                                  actionId: '',
                                  skillId: '',
                                  name: '',
                                })
                              }}
                              className="px-1.5 py-1 bg-slate-800 border border-slate-700 rounded text-white text-[11px] focus:outline-none focus:border-cyan-500"
                            >
                              <option value={HookActionType.SKILL}>Skill</option>
                              <option value={HookActionType.SCRIPT}>Script</option>
                            </select>
                            {h.actionType === HookActionType.SCRIPT ? (
                              <select
                                value={h.actionId}
                                onChange={(e) => {
                                  const sc = scripts.find((sc) => sc.id === e.target.value)
                                  updateHook(s.key, h.key, {
                                    actionId: e.target.value,
                                    skillId: '',
                                    name: sc?.name ?? h.name,
                                  })
                                }}
                                className="flex-1 min-w-0 px-1.5 py-1 bg-slate-800 border border-slate-700 rounded text-white text-[11px] focus:outline-none focus:border-cyan-500"
                              >
                                <option value="">Select script…</option>
                                {scripts.map((sc) => (
                                  <option key={sc.id} value={sc.id}>
                                    {sc.name}
                                  </option>
                                ))}
                              </select>
                            ) : (
                              <select
                                value={h.actionId}
                                onChange={(e) => {
                                  const sk = skills.find((sk) => sk.id === e.target.value)
                                  updateHook(s.key, h.key, {
                                    actionId: e.target.value,
                                    skillId: e.target.value,
                                    name: sk?.name ?? h.name,
                                  })
                                }}
                                className="flex-1 min-w-0 px-1.5 py-1 bg-slate-800 border border-slate-700 rounded text-white text-[11px] focus:outline-none focus:border-cyan-500"
                              >
                                <option value="">Select skill…</option>
                                {skills.map((sk) => (
                                  <option key={sk.id} value={sk.id}>
                                    {sk.name}
                                  </option>
                                ))}
                              </select>
                            )}
                            <button
                              type="button"
                              onClick={() => removeHook(s.key, h.key)}
                              className="text-gray-600 hover:text-red-400 transition-colors p-0.5 shrink-0"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </div>
                        ))}
                      </div>
                      {skills.length === 0 && scripts.length === 0 && s.hooks.length > 0 && (
                        <p className="mt-2 text-[11px] text-gray-600">
                          No skills or scripts defined yet. Create them in the Skills or Scripts page first.
                        </p>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>

        {validationError && (
          <p className="text-red-400 text-sm mb-4">{validationError}</p>
        )}
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
