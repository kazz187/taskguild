import { createFileRoute } from '@tanstack/react-router'
import { PermissionList } from '@/components/PermissionList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/projects/$projectId/permissions')({
  component: PermissionsPage,
})

function PermissionsPage() {
  useDocumentTitle('Permissions')
  const { projectId } = Route.useParams()
  return <PermissionList projectId={projectId} />
}
