import { createFileRoute } from '@tanstack/react-router'
import { SchedulesPage } from '@/components/organisms/SchedulesPage'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

interface SchedulesSearch {
  edit?: string
  mode?: 'create'
}

export const Route = createFileRoute('/projects/$projectId/schedules')({
  component: SchedulesRoute,
  validateSearch: (search: Record<string, unknown>): SchedulesSearch => ({
    edit: typeof search.edit === 'string' ? search.edit : undefined,
    mode: search.mode === 'create' ? 'create' : undefined,
  }),
})

function SchedulesRoute() {
  useDocumentTitle('Schedules')
  const { projectId } = Route.useParams()
  const { edit, mode } = Route.useSearch()
  return <SchedulesPage projectId={projectId} editScheduleId={edit} mode={mode} />
}
