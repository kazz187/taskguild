import { createFileRoute } from '@tanstack/react-router'
import { PermissionList } from '@/components/organisms/PermissionList'
import { SingleCommandPermissionList } from '@/components/organisms/SingleCommandPermissionList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/projects/$projectId/permissions')({
  component: PermissionsPage,
})

function PermissionsPage() {
  useDocumentTitle('Permissions')
  const { projectId } = Route.useParams()
  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-8">
      <PermissionList projectId={projectId} />
      <div className="border-t border-slate-800" />
      <SingleCommandPermissionList projectId={projectId} />
    </div>
  )
}
