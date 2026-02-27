import { createFileRoute } from '@tanstack/react-router'
import { PermissionList } from '@/components/PermissionList'

export const Route = createFileRoute('/projects/$projectId/permissions')({
  component: PermissionsPage,
})

function PermissionsPage() {
  const { projectId } = Route.useParams()
  return <PermissionList projectId={projectId} />
}
