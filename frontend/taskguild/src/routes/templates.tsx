import { createFileRoute } from '@tanstack/react-router'
import { TemplateList } from '@/components/TemplateList'

export const Route = createFileRoute('/templates')({
  component: TemplatesPage,
})

function TemplatesPage() {
  return <TemplateList />
}
