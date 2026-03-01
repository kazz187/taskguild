import { createFileRoute } from '@tanstack/react-router'
import { WorktreeList } from '@/components/WorktreeList'

export const Route = createFileRoute('/projects/$projectId/worktrees')({
  component: WorktreesPage,
})

function WorktreesPage() {
  const { projectId } = Route.useParams()
  return <WorktreeList projectId={projectId} />
}
