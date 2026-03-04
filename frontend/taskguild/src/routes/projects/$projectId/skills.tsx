import { createFileRoute } from '@tanstack/react-router'
import { SkillList } from '@/components/organisms/SkillList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/projects/$projectId/skills')({
  component: SkillsPage,
})

function SkillsPage() {
  useDocumentTitle('Skills')
  const { projectId } = Route.useParams()
  return <SkillList projectId={projectId} />
}
