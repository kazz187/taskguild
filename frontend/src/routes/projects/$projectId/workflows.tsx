import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { WorkflowForm } from '@/components/organisms/WorkflowForm'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useState } from 'react'
import { Plus, Workflow as WorkflowIcon, ArrowRight } from 'lucide-react'
import { PageHeading } from '@/components/molecules/index.ts'
import { Badge } from '@/components/atoms/index.ts'

export const Route = createFileRoute('/projects/$projectId/workflows')({
  component: WorkflowsPage,
})

function WorkflowsPage() {
  const { projectId } = Route.useParams()
  const navigate = useNavigate()
  const [showForm, setShowForm] = useState(false)

  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: workflowsData, refetch } = useQuery(listWorkflows, { projectId })

  const project = projectData?.project
  const workflows = workflowsData?.workflows ?? []

  useDocumentTitle(project ? `${project.name} - Workflows` : 'Workflows')

  const handleSaved = () => {
    setShowForm(false)
    refetch()
  }

  if (showForm) {
    return (
      <div className="flex flex-col h-dvh">
        <WorkflowForm
          projectId={projectId}
          onClose={() => setShowForm(false)}
          onSaved={handleSaved}
        />
      </div>
    )
  }

  return (
    <div className="p-4 md:p-8 max-w-4xl">
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <PageHeading icon={WorkflowIcon} title="Workflows" iconColor="text-cyan-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {workflows.length}
          </Badge>
        </PageHeading>
        <button
          onClick={() => setShowForm(true)}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors shrink-0"
        >
          <Plus className="w-4 h-4" />
          <span className="hidden sm:inline">New Workflow</span>
        </button>
      </div>

      {workflows.length === 0 && (
        <p className="text-gray-500">No workflows yet. Create one to get started.</p>
      )}

      <div className="grid gap-3 md:gap-4">
        {workflows.map((wf) => (
          <WorkflowCard
            key={wf.id}
            workflow={wf}
            onClick={() =>
              navigate({
                to: '/projects/$projectId',
                params: { projectId },
                search: { workflowId: wf.id },
              })
            }
          />
        ))}
      </div>
    </div>
  )
}

function WorkflowCard({ workflow, onClick }: { workflow: Workflow; onClick: () => void }) {
  const statusCount = workflow.statuses.length

  return (
    <div
      onClick={onClick}
      className="block bg-slate-900 border border-slate-800 rounded-xl p-4 md:p-5 hover:border-cyan-500/50 transition-all group cursor-pointer"
    >
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 min-w-0 flex-1">
          <WorkflowIcon className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
          <div className="min-w-0">
            <h2 className="text-base md:text-lg font-semibold text-white group-hover:text-cyan-400 transition-colors">
              {workflow.name}
            </h2>
            {workflow.description && (
              <p className="text-gray-400 text-sm mt-1 line-clamp-2">
                {workflow.description}
              </p>
            )}
            <div className="flex items-center gap-3 mt-2 text-xs text-gray-500">
              <span>{statusCount} {statusCount === 1 ? 'status' : 'statuses'}</span>
            </div>
          </div>
        </div>
        <ArrowRight className="w-5 h-5 text-gray-600 group-hover:text-cyan-400 transition-colors mt-1 shrink-0" />
      </div>
    </div>
  )
}
