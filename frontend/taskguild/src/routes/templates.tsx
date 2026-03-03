import { createFileRoute } from '@tanstack/react-router'
import { TemplateList } from '@/components/TemplateList'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'

export const Route = createFileRoute('/templates')({
  component: TemplatesPage,
})

function TemplatesPage() {
  useDocumentTitle('Templates')
  return <TemplateList />
}
