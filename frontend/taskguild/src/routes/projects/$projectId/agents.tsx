import { createFileRoute } from '@tanstack/react-router'
import { AgentList } from '@/components/AgentList'

export const Route = createFileRoute('/projects/$projectId/agents')({
  component: AgentsPage,
})

function AgentsPage() {
  const { projectId } = Route.useParams()
  return <AgentList projectId={projectId} />
}
