import { createFileRoute } from '@tanstack/react-router'
import { WorktreeList } from '@/components/WorktreeList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/projects/$projectId/worktrees')({
  component: WorktreesPage,
})

function WorktreesPage() {
  useDocumentTitle('Worktrees')
  const { projectId } = Route.useParams()
  return <WorktreeList projectId={projectId} />
}
