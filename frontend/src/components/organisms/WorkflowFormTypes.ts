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
  agentId: string // deprecated: use skillId
  skillId: string // reference to SkillDefinition assigned to this status
  hooks: HookDraft[]
  enableAgentMdHarness: boolean // default true: review agent markdown on status exit
  agentMdHarnessExplicitlyDisabled: boolean // tracks explicit user choice
  permissionMode: string // permission mode for agents in this status
  inheritSessionFrom: string // name of status to inherit session from (fork)
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
