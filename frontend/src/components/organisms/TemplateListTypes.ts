import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { Bot, Sparkles, Terminal } from 'lucide-react'

export type EntityType = 'agent' | 'skill' | 'script'

export const CONTEXT_OPTIONS = [
  { value: '', label: 'Inline (default)' },
  { value: 'fork', label: 'Fork (run in sub-agent)' },
]

export const AGENT_OPTIONS = [
  { value: '', label: 'general-purpose (default)' },
  { value: 'Explore', label: 'Explore' },
  { value: 'Plan', label: 'Plan' },
  { value: 'general-purpose', label: 'General Purpose' },
]

export const TABS: { type: EntityType; label: string; icon: typeof Bot; color: string }[] = [
  { type: 'agent', label: 'Agents', icon: Bot, color: 'cyan' },
  { type: 'skill', label: 'Skills', icon: Sparkles, color: 'purple' },
  { type: 'script', label: 'Scripts', icon: Terminal, color: 'green' },
]

// --- Agent Form ---

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

export const emptyAgentForm: AgentFormData = {
  name: '', description: '', prompt: '', tools: [], disallowedTools: [],
  model: '', permissionMode: '', skills: [], memory: '',
}

// --- Skill Form ---

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

export const emptySkillForm: SkillFormData = {
  name: '', description: '', content: '', disableModelInvocation: false,
  userInvocable: true, allowedTools: [], model: '', context: '', agent: '', argumentHint: '',
}

// --- Script Form ---

export interface ScriptFormData {
  name: string
  description: string
  filename: string
  content: string
}

export const emptyScriptForm: ScriptFormData = {
  name: '', description: '', filename: '', content: '',
}

// --- Template Form (wraps config forms) ---

export interface TemplateFormData {
  templateName: string
  templateDescription: string
  entityType: EntityType
  agentConfig: AgentFormData
  skillConfig: SkillFormData
  scriptConfig: ScriptFormData
}

export const emptyTemplateForm = (entityType: EntityType): TemplateFormData => ({
  templateName: '',
  templateDescription: '',
  entityType,
  agentConfig: { ...emptyAgentForm },
  skillConfig: { ...emptySkillForm },
  scriptConfig: { ...emptyScriptForm },
})

export function templateToForm(t: Template): TemplateFormData {
  const form = emptyTemplateForm(t.entityType as EntityType)
  form.templateName = t.name
  form.templateDescription = t.description

  if (t.entityType === 'agent' && t.agentConfig) {
    form.agentConfig = {
      name: t.agentConfig.name,
      description: t.agentConfig.description,
      prompt: t.agentConfig.prompt,
      tools: [...(t.agentConfig.tools ?? [])],
      disallowedTools: [...(t.agentConfig.disallowedTools ?? [])],
      model: t.agentConfig.model,
      permissionMode: t.agentConfig.permissionMode,
      skills: [...(t.agentConfig.skills ?? [])],
      memory: t.agentConfig.memory,
    }
  } else if (t.entityType === 'skill' && t.skillConfig) {
    form.skillConfig = {
      name: t.skillConfig.name,
      description: t.skillConfig.description,
      content: t.skillConfig.content,
      disableModelInvocation: t.skillConfig.disableModelInvocation,
      userInvocable: t.skillConfig.userInvocable,
      allowedTools: [...(t.skillConfig.allowedTools ?? [])],
      model: t.skillConfig.model,
      context: t.skillConfig.context,
      agent: t.skillConfig.agent,
      argumentHint: t.skillConfig.argumentHint,
    }
  } else if (t.entityType === 'script' && t.scriptConfig) {
    form.scriptConfig = {
      name: t.scriptConfig.name,
      description: t.scriptConfig.description,
      filename: t.scriptConfig.filename,
      content: t.scriptConfig.content,
    }
  }
  return form
}
