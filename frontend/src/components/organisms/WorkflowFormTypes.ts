import { HookTrigger, HookActionType } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'

export interface HookDraft {
  key: string
  id: string
  skillId: string // legacy: kept for backward compat
  actionType: HookActionType
  actionId: string
  trigger: HookTrigger
  order: number
  name: string
}

export interface StatusDraft {
  key: string
  id: string
  name: string
  order: number
  isInitial: boolean
  isTerminal: boolean
  transitionsTo: string[] // keys
  agentId: string // Deprecated: reference to AgentDefinition (fallback)
  hooks: HookDraft[]
  enableAgentMdHarness: boolean // Deprecated: use enableSkillHarness
  agentMdHarnessExplicitlyDisabled: boolean // Deprecated
  permissionMode: string // permission mode for agents in this status
  inheritSessionFrom: string // name of status to inherit session from (fork)
  // Execution configuration (replaces agent MD frontmatter)
  model: string
  tools: string[]
  disallowedTools: string[]
  skillIds: string[] // Skill entity IDs
  enableSkillHarness: boolean
  skillHarnessExplicitlyDisabled: boolean
}

export interface AgentConfigDraft {
  key: string
  id: string
  statusKey: string
  name: string
  description: string
  instructions: string
}

let nextKey = 0
export function genKey() {
  return `k${++nextKey}`
}
