import { createFileRoute } from '@tanstack/react-router'
import { SkillList } from '@/components/SkillList'

export const Route = createFileRoute('/projects/$projectId/skills')({
  component: SkillsPage,
})

function SkillsPage() {
  const { projectId } = Route.useParams()
  return <SkillList projectId={projectId} />
}
