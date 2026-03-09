import { createFileRoute } from '@tanstack/react-router'
import { ScriptList } from '@/components/organisms/ScriptList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/projects/$projectId/scripts')({
  component: ScriptsPage,
})

function ScriptsPage() {
  useDocumentTitle('Scripts')
  const { projectId } = Route.useParams()
  return <ScriptList projectId={projectId} />
}
