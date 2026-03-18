import { createFileRoute } from '@tanstack/react-router'
import { AgentList } from '@/components/organisms/AgentList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

interface AgentsSearch {
  edit?: string
  mode?: 'create'
}

export const Route = createFileRoute('/projects/$projectId/agents')({
  component: AgentsPage,
  validateSearch: (search: Record<string, unknown>): AgentsSearch => ({
    edit: typeof search.edit === 'string' ? search.edit : undefined,
    mode: search.mode === 'create' ? 'create' : undefined,
  }),
})

function AgentsPage() {
  useDocumentTitle('Agents')
  const { projectId } = Route.useParams()
  const { edit, mode } = Route.useSearch()
  return <AgentList projectId={projectId} editAgentId={edit} mode={mode} />
}
