import { useMemo } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listSchedules,
  createSchedule,
  updateSchedule,
  deleteSchedule,
  setScheduleEnabled,
} from '@taskguild/proto/taskguild/v1/schedule-ScheduleService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import type { Schedule } from '@taskguild/proto/taskguild/v1/schedule_pb.ts'
import { Clock, Plus, Pencil, Trash2 } from 'lucide-react'
import { PageHeading, EmptyState } from '../molecules/index.ts'
import { Badge, Checkbox } from '../atoms/index.ts'
import {
  ScheduleFormModal,
  emptyScheduleForm,
  scheduleToForm,
  type ScheduleFormData,
} from './ScheduleFormModal.tsx'

export interface SchedulesPageProps {
  projectId: string
  editScheduleId?: string
  mode?: 'create'
}

export function SchedulesPage({ projectId, editScheduleId, mode }: SchedulesPageProps) {
  const navigate = useNavigate()

  const { data, refetch, isLoading } = useQuery(listSchedules, { projectId })
  const { data: workflowsData } = useQuery(listWorkflows, { projectId })

  const createMut = useMutation(createSchedule)
  const updateMut = useMutation(updateSchedule)
  const deleteMut = useMutation(deleteSchedule)
  const enableMut = useMutation(setScheduleEnabled)

  const schedules = useMemo(() => data?.schedules ?? [], [data])
  const workflows = useMemo(() => workflowsData?.workflows ?? [], [workflowsData])
  const workflowsById = useMemo(() => {
    const m = new Map<string, string>()
    for (const w of workflows) m.set(w.id, w.name)
    return m
  }, [workflows])

  const formMode: 'create' | 'edit' | null =
    mode === 'create' ? 'create' : editScheduleId ? 'edit' : null

  const editing = useMemo(
    () => (editScheduleId ? schedules.find((s) => s.id === editScheduleId) : undefined),
    [schedules, editScheduleId],
  )

  const openCreate = () => {
    navigate({
      to: '/projects/$projectId/schedules',
      params: { projectId },
      search: { mode: 'create' },
    })
  }

  const openEdit = (id: string) => {
    navigate({
      to: '/projects/$projectId/schedules',
      params: { projectId },
      search: { edit: id },
    })
  }

  const closeForm = () => {
    navigate({
      to: '/projects/$projectId/schedules',
      params: { projectId },
      search: {},
    })
  }

  const handleCreate = async (form: ScheduleFormData) => {
    await createMut.mutateAsync({
      projectId,
      workflowId: form.workflowId,
      name: form.name,
      description: form.description,
      cronExpression: form.cronExpression,
      enabled: form.enabled,
      taskTitle: form.taskTitle,
      taskDescription: form.taskDescription,
      // proto field `optional string status_id`. Empty value is omitted by
      // passing undefined; backend interprets that as "use initial status".
      statusId: form.statusId === '' ? undefined : form.statusId,
      useWorktree: form.useWorktree,
      effort: form.effort,
    })
    refetch()
    closeForm()
  }

  const handleUpdate = async (form: ScheduleFormData) => {
    if (!editing) return
    await updateMut.mutateAsync({
      id: editing.id,
      workflowId: form.workflowId,
      name: form.name,
      description: form.description,
      cronExpression: form.cronExpression,
      taskTitle: form.taskTitle,
      taskDescription: form.taskDescription,
      statusId: form.statusId === '' ? undefined : form.statusId,
      useWorktree: form.useWorktree,
      effort: form.effort,
    })
    // `enabled` is intentionally absent from UpdateScheduleRequest — toggling
    // routes through SetScheduleEnabled so the in-process cron runner picks up
    // the change. If the user flipped Enabled in the edit modal, mirror that
    // change through the dedicated RPC.
    if (form.enabled !== editing.enabled) {
      await enableMut.mutateAsync({ id: editing.id, enabled: form.enabled })
    }
    refetch()
    closeForm()
  }

  const handleDelete = async (id: string) => {
    if (!window.confirm('Delete this schedule? This cannot be undone.')) return
    await deleteMut.mutateAsync({ id })
    refetch()
  }

  const handleToggle = async (s: Schedule, enabled: boolean) => {
    await enableMut.mutateAsync({ id: s.id, enabled })
    refetch()
  }

  const initial = useMemo<ScheduleFormData>(() => {
    if (formMode === 'edit' && editing) return scheduleToForm(editing)
    return emptyScheduleForm
  }, [formMode, editing])

  return (
    <div className="p-4 md:p-8 max-w-5xl">
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <PageHeading icon={Clock} title="Schedules" iconColor="text-cyan-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {schedules.length}
          </Badge>
        </PageHeading>
        <button
          onClick={openCreate}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors shrink-0"
        >
          <Plus className="w-4 h-4" />
          <span className="hidden sm:inline">New Schedule</span>
        </button>
      </div>

      {!isLoading && schedules.length === 0 && (
        <EmptyState
          icon={Clock}
          message="No schedules yet"
          hint="Create a schedule to automatically generate tasks on a cron expression."
        />
      )}

      {schedules.length > 0 && (
        <div className="overflow-x-auto -mx-4 md:mx-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-500 border-b border-slate-800">
                <th className="px-3 py-2 font-medium">Enabled</th>
                <th className="px-3 py-2 font-medium">Name</th>
                <th className="px-3 py-2 font-medium">Cron</th>
                <th className="px-3 py-2 font-medium">Workflow → Status</th>
                <th className="px-3 py-2 font-medium">Next run</th>
                <th className="px-3 py-2 font-medium">Last run</th>
                <th className="px-3 py-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              {schedules.map((s) => (
                <ScheduleRow
                  key={s.id}
                  schedule={s}
                  workflowName={workflowsById.get(s.workflowId) ?? s.workflowId}
                  onToggle={(en) => handleToggle(s, en)}
                  onEdit={() => openEdit(s.id)}
                  onDelete={() => handleDelete(s.id)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {formMode === 'create' && (
        <ScheduleFormModal
          projectId={projectId}
          formMode="create"
          initial={initial}
          isPending={createMut.isPending}
          error={createMut.error}
          onClose={closeForm}
          onSubmit={handleCreate}
        />
      )}

      {formMode === 'edit' && editing && (
        <ScheduleFormModal
          projectId={projectId}
          formMode="edit"
          initial={initial}
          isPending={updateMut.isPending}
          error={updateMut.error}
          onClose={closeForm}
          onSubmit={handleUpdate}
        />
      )}
    </div>
  )
}

function ScheduleRow({
  schedule,
  workflowName,
  onToggle,
  onEdit,
  onDelete,
}: {
  schedule: Schedule
  workflowName: string
  onToggle: (enabled: boolean) => void
  onEdit: () => void
  onDelete: () => void
}) {
  const nextRun = schedule.nextRunAt
    ? new Date(Number(schedule.nextRunAt.seconds) * 1000).toLocaleString()
    : '—'
  const lastRun = schedule.lastRunAt
    ? new Date(Number(schedule.lastRunAt.seconds) * 1000).toLocaleString()
    : '—'

  return (
    <tr className="border-b border-slate-800/50 hover:bg-slate-800/30 transition-colors">
      <td className="px-3 py-2 align-middle">
        <Checkbox
          checked={schedule.enabled}
          onChange={(e) => onToggle(e.target.checked)}
        />
      </td>
      <td className="px-3 py-2 align-middle">
        <div className="text-white font-medium">{schedule.name}</div>
        {schedule.description && (
          <div className="text-xs text-gray-500 line-clamp-1">{schedule.description}</div>
        )}
      </td>
      <td className="px-3 py-2 align-middle">
        <code className="text-xs bg-slate-800 px-1.5 py-0.5 rounded text-cyan-300">
          {schedule.cronExpression}
        </code>
      </td>
      <td className="px-3 py-2 align-middle text-gray-300">
        <span className="text-gray-400">{workflowName}</span>
        <span className="text-gray-600"> → </span>
        <span>{schedule.statusId || '(initial)'}</span>
      </td>
      <td className="px-3 py-2 align-middle text-xs text-gray-400">{nextRun}</td>
      <td className="px-3 py-2 align-middle text-xs text-gray-400">
        {lastRun}
        {schedule.lastError && (
          <div className="text-[10px] text-rose-400 mt-0.5 line-clamp-1" title={schedule.lastError}>
            {schedule.lastError}
          </div>
        )}
      </td>
      <td className="px-3 py-2 align-middle text-right">
        <button
          onClick={onEdit}
          className="text-gray-500 hover:text-cyan-400 transition-colors p-1"
          title="Edit"
        >
          <Pencil className="w-3.5 h-3.5" />
        </button>
        <button
          onClick={onDelete}
          className="text-gray-500 hover:text-rose-400 transition-colors p-1"
          title="Delete"
        >
          <Trash2 className="w-3.5 h-3.5" />
        </button>
      </td>
    </tr>
  )
}
