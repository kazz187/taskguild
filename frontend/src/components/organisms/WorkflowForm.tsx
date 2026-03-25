import { useState } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createWorkflow, updateWorkflow } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { listAgents } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
import { listSkills } from '@taskguild/proto/taskguild/v1/skill-SkillService_connectquery.ts'
import { listScripts } from '@taskguild/proto/taskguild/v1/script-ScriptService_connectquery.ts'
import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { HookTrigger, HookActionType } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { X, Plus } from 'lucide-react'
import { Button, Input, Checkbox, Textarea } from '../atoms/index.ts'
import { FormField } from '../molecules/index.ts'
import type { StatusDraft, HookDraft, AgentConfigDraft } from './WorkflowFormTypes.ts'
import { genKey } from './WorkflowFormTypes.ts'
import { workflowToDrafts, buildProtoPayload } from './WorkflowFormUtils.ts'
import { StatusCard } from './StatusCard.tsx'

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
            { key: kDraft, id: '', name: 'Draft', order: 0, isInitial: true, isTerminal: false, transitionsTo: [kDevelop], agentId: '', hooks: [], enableAgentMdHarness: true, agentMdHarnessExplicitlyDisabled: false, permissionMode: '', inheritSessionFrom: '' },
            { key: kDevelop, id: '', name: 'Develop', order: 1, isInitial: false, isTerminal: false, transitionsTo: [kReview, kDraft], agentId: '', hooks: [], enableAgentMdHarness: true, agentMdHarnessExplicitlyDisabled: false, permissionMode: '', inheritSessionFrom: '' },
            { key: kReview, id: '', name: 'Review', order: 2, isInitial: false, isTerminal: false, transitionsTo: [kTest], agentId: '', hooks: [], enableAgentMdHarness: true, agentMdHarnessExplicitlyDisabled: false, permissionMode: '', inheritSessionFrom: '' },
            { key: kTest, id: '', name: 'Test', order: 3, isInitial: false, isTerminal: false, transitionsTo: [kClosed], agentId: '', hooks: [], enableAgentMdHarness: true, agentMdHarnessExplicitlyDisabled: false, permissionMode: '', inheritSessionFrom: '' },
            { key: kClosed, id: '', name: 'Closed', order: 4, isInitial: false, isTerminal: true, transitionsTo: [], agentId: '', hooks: [], enableAgentMdHarness: true, agentMdHarnessExplicitlyDisabled: false, permissionMode: '', inheritSessionFrom: '' },
          ],
          agentDrafts: [],
        }
      })()

  const [name, setName] = useState(workflow?.name ?? '')
  const [description, setDescription] = useState(workflow?.description ?? '')
  const [defaultUseWorktree, setDefaultUseWorktree] = useState(workflow?.defaultUseWorktree ?? false)
  const [customPrompt, setCustomPrompt] = useState(workflow?.customPrompt ?? '')
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

  // --- Status & Hook handlers ---

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
      { key: genKey(), id: '', name: '', order: prev.length, isInitial: false, isTerminal: false, transitionsTo: [], agentId: '', hooks: [], enableAgentMdHarness: true, agentMdHarnessExplicitlyDisabled: false, permissionMode: '', inheritSessionFrom: '' },
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

    const { protoStatuses, protoAgentConfigs } = buildProtoPayload(statuses, agentConfigs)

    if (isEdit) {
      updateMutation.mutate(
        {
          id: workflow!.id,
          name,
          description,
          statuses: protoStatuses,
          agentConfigs: protoAgentConfigs,
          defaultPermissionMode: '',
          defaultUseWorktree,
          customPrompt,
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
          defaultPermissionMode: '',
          defaultUseWorktree,
          customPrompt,
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
          <Button
            type="button"
            variant="ghost"
            size="xs"
            iconOnly
            icon={<X className="w-5 h-5" />}
            onClick={onClose}
          />
        </div>

        {/* Basic info */}
        <div className="space-y-3 mb-6 md:mb-8">
          <FormField label="Name *" labelSize="sm">
            <Input
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Bug Fix Workflow"
            />
          </FormField>
          <FormField label="Description" labelSize="sm">
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Workflow description"
            />
          </FormField>
        </div>

        {/* Task Defaults */}
        <div className="mb-6 md:mb-8">
          <h3 className="text-sm font-semibold text-gray-300 uppercase tracking-wide mb-3">
            Task Defaults
          </h3>
          <div className="space-y-3">
            <Checkbox
              checked={defaultUseWorktree}
              onChange={(e) => setDefaultUseWorktree(e.target.checked)}
              label="Use Worktree (isolate changes in a git worktree)"
            />
            <FormField
              label="Custom Prompt"
              labelSize="sm"
              hint="If set, this prompt will be prepended to the agent's instructions when a task is claimed."
            >
              <Textarea
                value={customPrompt}
                onChange={(e) => setCustomPrompt(e.target.value)}
                rows={4}
                textareaSize="sm"
                className="resize-y"
                placeholder="Custom prompt prepended to agent instructions for all tasks in this workflow"
              />
            </FormField>
          </div>
        </div>

        {/* Statuses */}
        <div className="mb-6 md:mb-8">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
              Statuses
            </h3>
            <Button
              type="button"
              variant="ghost"
              size="xs"
              icon={<Plus className="w-3.5 h-3.5" />}
              onClick={addStatus}
              className="text-cyan-400 hover:text-cyan-300"
            >
              Add Status
            </Button>
          </div>
          <div className="space-y-3">
            {statuses.map((s, index) => (
              <StatusCard
                key={s.key}
                status={s}
                index={index}
                statuses={statuses}
                agents={agents}
                skills={skills}
                scripts={scripts}
                agentConfigs={agentConfigs}
                onMoveStatus={moveStatus}
                onRemoveStatus={removeStatus}
                onUpdateStatus={updateStatus}
                onToggleTransition={toggleTransition}
                onAddHook={addHook}
                onRemoveHook={removeHook}
                onMoveHook={moveHook}
                onUpdateHook={updateHook}
              />
            ))}
          </div>
        </div>

        {validationError && (
          <p className="text-red-400 text-sm mb-4">{validationError}</p>
        )}
        {mutation.error && (
          <p className="text-red-400 text-sm mb-4">{mutation.error.message}</p>
        )}

        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            variant="primary"
            size="sm"
            disabled={mutation.isPending || !name || statuses.length === 0}
            className="px-4"
          >
            {mutation.isPending
              ? isEdit ? 'Saving...' : 'Creating...'
              : isEdit ? 'Save' : 'Create Workflow'}
          </Button>
        </div>
      </div>
    </form>
  )
}
