import { createFileRoute } from '@tanstack/react-router'
import { AgentList } from '@/components/organisms/AgentList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/projects/$projectId/agents')({
  component: AgentsPage,
})

function AgentsPage() {
  useDocumentTitle('Agents')
  const { projectId } = Route.useParams()
  return <AgentList projectId={projectId} />
}
