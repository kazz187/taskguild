import { useMemo, useState, useEffect, type FormEvent } from 'react'
import { useQuery } from '@connectrpc/connect-query'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import type { Schedule } from '@taskguild/proto/taskguild/v1/schedule_pb.ts'
import { Input, Textarea, Select, Checkbox } from '../atoms/index.ts'
import { EntityFormModal, FormField } from '../molecules/index.ts'
import { CronExpressionInput, validateCronShape } from '../molecules/CronExpressionInput.tsx'
import { EFFORT_OPTIONS } from '@/lib/constants.ts'

export interface ScheduleFormData {
  name: string
  description: string
  workflowId: string
  statusId: string // empty = use workflow initial
  cronExpression: string
  enabled: boolean
  taskTitle: string
  taskDescription: string
  useWorktree: boolean
  effort: string
}

export const emptyScheduleForm: ScheduleFormData = {
  name: '',
  description: '',
  workflowId: '',
  statusId: '',
  cronExpression: '',
  enabled: true,
  taskTitle: '',
  taskDescription: '',
  useWorktree: false,
  effort: '',
}

export function scheduleToForm(s: Schedule): ScheduleFormData {
  return {
    name: s.name,
    description: s.description,
    workflowId: s.workflowId,
    statusId: s.statusId,
    cronExpression: s.cronExpression,
    enabled: s.enabled,
    taskTitle: s.taskTitle,
    taskDescription: s.taskDescription,
    useWorktree: s.useWorktree,
    effort: s.effort,
  }
}

export interface ScheduleFormModalProps {
  projectId: string
  formMode: 'create' | 'edit'
  initial: ScheduleFormData
  isPending: boolean
  error?: Error | null
  onClose: () => void
  onSubmit: (data: ScheduleFormData) => void
}

const PLACEHOLDER_HINT =
  'Placeholders: {{date}} {{datetime}} {{year}} {{month}} {{day}} {{time}}'

export function ScheduleFormModal({
  projectId,
  formMode,
  initial,
  isPending,
  error,
  onClose,
  onSubmit,
}: ScheduleFormModalProps) {
  const [form, setForm] = useState<ScheduleFormData>(initial)

  const { data: workflowsData } = useQuery(listWorkflows, { projectId })
  const workflows = useMemo(() => workflowsData?.workflows ?? [], [workflowsData])

  // When workflows load and the form has no workflow yet (create mode), pick the
  // first available workflow.
  useEffect(() => {
    if (!form.workflowId && workflows.length > 0) {
      setForm((prev) => ({ ...prev, workflowId: workflows[0].id }))
    }
  }, [workflows, form.workflowId])

  const selectedWorkflow = useMemo(
    () => workflows.find((w) => w.id === form.workflowId),
    [workflows, form.workflowId],
  )
  const statuses = useMemo(
    () => [...(selectedWorkflow?.statuses ?? [])].sort((a, b) => a.order - b.order),
    [selectedWorkflow],
  )

  const cronError = form.cronExpression.trim() === '' ? null : validateCronShape(form.cronExpression)

  const submitDisabled =
    form.name.trim() === '' ||
    form.taskTitle.trim() === '' ||
    form.workflowId === '' ||
    cronError !== null ||
    form.cronExpression.trim() === ''

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (submitDisabled) return
    onSubmit(form)
  }

  return (
    <EntityFormModal
      entityName="Schedule"
      formMode={formMode}
      onSubmit={handleSubmit}
      onClose={onClose}
      isPending={isPending}
      error={error ?? null}
      submitDisabled={submitDisabled}
    >
      <FormField label="Name">
        <Input
          inputSize="sm"
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
          placeholder="Daily standup reminder"
          autoFocus
        />
      </FormField>

      <FormField label="Description">
        <Textarea
          textareaSize="sm"
          value={form.description}
          onChange={(e) => setForm({ ...form, description: e.target.value })}
          placeholder="What this schedule is for (optional)"
        />
      </FormField>

      <FormField label="Cron expression" hint="Server local time. Standard 5-field syntax.">
        <CronExpressionInput
          value={form.cronExpression}
          onChange={(v) => setForm({ ...form, cronExpression: v })}
        />
      </FormField>

      <Checkbox
        label="Enabled (fires on schedule when on)"
        checked={form.enabled}
        onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
      />

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <FormField label="Workflow">
          <Select
            selectSize="sm"
            value={form.workflowId}
            onChange={(e) =>
              setForm({ ...form, workflowId: e.target.value, statusId: '' })
            }
          >
            <option value="">Select workflow...</option>
            {workflows.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </Select>
        </FormField>

        <FormField label="Place task in status">
          <Select
            selectSize="sm"
            value={form.statusId}
            onChange={(e) => setForm({ ...form, statusId: e.target.value })}
            disabled={!selectedWorkflow}
          >
            <option value="">(use initial status)</option>
            {statuses.map((s) => (
              <option key={s.name} value={s.name}>
                {s.name}
                {s.isInitial ? ' (initial)' : ''}
                {s.isTerminal ? ' (terminal)' : ''}
              </option>
            ))}
          </Select>
        </FormField>
      </div>

      <div className="border-t border-slate-800 my-2 pt-2">
        <h4 className="text-xs font-semibold text-gray-400 mb-2">
          Task content
        </h4>
        <p className="text-[11px] text-gray-500 mb-2">{PLACEHOLDER_HINT}</p>
      </div>

      <FormField label="Task title">
        <Input
          inputSize="sm"
          value={form.taskTitle}
          onChange={(e) => setForm({ ...form, taskTitle: e.target.value })}
          placeholder="[scheduled] {{datetime}}"
        />
      </FormField>

      <FormField label="Task description">
        <Textarea
          textareaSize="sm"
          value={form.taskDescription}
          onChange={(e) => setForm({ ...form, taskDescription: e.target.value })}
          placeholder="Optional description for the generated task."
        />
      </FormField>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <FormField label="Effort">
          <Select
            selectSize="sm"
            value={form.effort}
            onChange={(e) => setForm({ ...form, effort: e.target.value })}
          >
            {EFFORT_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </Select>
        </FormField>
        <div className="flex items-end pb-1">
          <Checkbox
            label="Use Worktree"
            checked={form.useWorktree}
            onChange={(e) => setForm({ ...form, useWorktree: e.target.checked })}
          />
        </div>
      </div>
    </EntityFormModal>
  )
}
