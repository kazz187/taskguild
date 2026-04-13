import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { HookActionType } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import type { StatusDraft } from './WorkflowFormTypes.ts'
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
      permissionMode: s.permissionMode ?? '',
      inheritSessionFrom: s.inheritSessionFrom ?? '',
      model: s.model ?? '',
      effort: s.effort ?? '',
      skillId: s.skillIds?.[0] ?? '',
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

  return { statusDrafts }
}

export function buildProtoPayload(statuses: StatusDraft[]) {
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
    agentId: '',
    enableAgentMdHarness: false,
    agentMdHarnessExplicitlyDisabled: true,
    permissionMode: s.permissionMode,
    inheritSessionFrom: s.inheritSessionFrom,
    model: s.model,
    effort: s.effort,
    tools: [] as string[],
    disallowedTools: [] as string[],
    skillIds: s.skillId ? [s.skillId] : [],
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

  return { protoStatuses, protoAgentConfigs: [] as { id: string; workflowStatusId: string; name: string; description: string; instructions: string; allowedTools: string[] }[] }
}
