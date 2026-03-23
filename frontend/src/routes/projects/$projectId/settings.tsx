import { createFileRoute } from '@tanstack/react-router'
import { ClaudeSettingsList } from '@/components/organisms/ClaudeSettingsList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/projects/$projectId/settings')({
  component: SettingsPage,
})

function SettingsPage() {
  useDocumentTitle('Settings')
  const { projectId } = Route.useParams()
  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-8">
      <ClaudeSettingsList projectId={projectId} />
    </div>
  )
}
