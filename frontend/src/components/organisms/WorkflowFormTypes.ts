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
  hooks: HookDraft[]
  permissionMode: string // permission mode for agents in this status
  inheritSessionFrom: string // name of status to inherit session from (fork)
  // Execution configuration
  model: string
  effort: string // "low" / "medium" / "high" / "xhigh" / "max" (empty = inherit)
  skillId: string // Single execution skill ID (empty = none)
  enableSkillHarness: boolean
  skillHarnessExplicitlyDisabled: boolean
}

let nextKey = 0
export function genKey() {
  return `k${++nextKey}`
}
