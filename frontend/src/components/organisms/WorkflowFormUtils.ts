import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { HookActionType } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import type { StatusDraft, AgentConfigDraft } from './WorkflowFormTypes.ts'
import { genKey } from './WorkflowFormTypes.ts'

export function workflowToDrafts(wf: Workflow) {
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
      enableAgentMdHarness: s.agentMdHarnessExplicitlyDisabled ? s.enableAgentMdHarness : true,
      agentMdHarnessExplicitlyDisabled: s.agentMdHarnessExplicitlyDisabled,
      permissionMode: s.permissionMode ?? '',
      inheritSessionFrom: s.inheritSessionFrom ?? '',
      model: s.model ?? '',
      tools: [...(s.tools ?? [])],
      disallowedTools: [...(s.disallowedTools ?? [])],
      skillIds: [...(s.skillIds ?? [])],
      enableSkillHarness: s.skillHarnessExplicitlyDisabled ? s.enableSkillHarness : true,
      skillHarnessExplicitlyDisabled: s.skillHarnessExplicitlyDisabled,
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

export function buildProtoPayload(statuses: StatusDraft[], agentConfigs: AgentConfigDraft[]) {
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
    enableAgentMdHarness: s.enableAgentMdHarness,
    agentMdHarnessExplicitlyDisabled: s.agentMdHarnessExplicitlyDisabled,
    permissionMode: s.permissionMode,
    inheritSessionFrom: s.inheritSessionFrom,
    model: s.model,
    tools: s.tools,
    disallowedTools: s.disallowedTools,
    skillIds: s.skillIds,
    enableSkillHarness: s.enableSkillHarness,
    skillHarnessExplicitlyDisabled: s.skillHarnessExplicitlyDisabled,
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
