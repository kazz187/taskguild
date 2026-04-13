import type { SkillDefinition } from '@taskguild/proto/taskguild/v1/skill_pb.ts'

export interface SkillFormData {
  name: string
  description: string
  content: string
  disableModelInvocation: boolean
  userInvocable: boolean
  allowedTools: string[]
  model: string
  context: string
  agent: string
  argumentHint: string
}

export const emptyForm: SkillFormData = {
  name: '',
  description: '',
  content: '',
  disableModelInvocation: false,
  userInvocable: true,
  allowedTools: [],
  model: '',
  context: '',
  agent: '',
  argumentHint: '',
}

export function skillToForm(s: SkillDefinition): SkillFormData {
  return {
    name: s.name,
    description: s.description,
    content: s.content,
    disableModelInvocation: s.disableModelInvocation,
    userInvocable: s.userInvocable,
    allowedTools: [...(s.allowedTools ?? [])],
    model: s.model,
    context: s.context,
    agent: s.agent,
    argumentHint: s.argumentHint,
  }
}
