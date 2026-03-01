import { createFileRoute } from '@tanstack/react-router'
import { ScriptList } from '@/components/ScriptList'

export const Route = createFileRoute('/projects/$projectId/scripts')({
  component: ScriptsPage,
})

function ScriptsPage() {
  const { projectId } = Route.useParams()
  return <ScriptList projectId={projectId} />
}
