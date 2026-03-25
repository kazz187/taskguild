import type { AgentDefinition } from '@taskguild/proto/taskguild/v1/agent_pb.ts'
import { AgentDiffType } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'

export interface AgentFormData {
  name: string
  description: string
  prompt: string
  tools: string[]
  disallowedTools: string[]
  model: string
  permissionMode: string
  skills: string[]
  memory: string
}

export const emptyForm: AgentFormData = {
  name: '',
  description: '',
  prompt: '',
  tools: [],
  disallowedTools: [],
  model: '',
  permissionMode: '',
  skills: [],
  memory: '',
}

export function agentToForm(a: AgentDefinition): AgentFormData {
  return {
    name: a.name,
    description: a.description,
    prompt: a.prompt,
    tools: [...(a.tools ?? [])],
    disallowedTools: [...(a.disallowedTools ?? [])],
    model: a.model,
    permissionMode: a.permissionMode,
    skills: [...(a.skills ?? [])],
    memory: a.memory,
  }
}

export function diffTypeLabel(dt: AgentDiffType): string {
  switch (dt) {
    case AgentDiffType.MODIFIED: return 'Modified'
    case AgentDiffType.AGENT_ONLY: return 'Agent Only'
    case AgentDiffType.SERVER_ONLY: return 'Server Only'
    default: return 'Unknown'
  }
}
