import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { TaskBoard } from '@/components/TaskBoard'
import { useState } from 'react'
import { GitBranch } from 'lucide-react'

export const Route = createFileRoute('/projects/$projectId')({
  component: ProjectDetailPage,
})

function ProjectDetailPage() {
  const { projectId } = Route.useParams()
  const [selectedWorkflow, setSelectedWorkflow] = useState<Workflow | null>(null)

  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: workflowsData } = useQuery(listWorkflows, {
    projectId,
  })

  const project = projectData?.project
  const workflows = workflowsData?.workflows ?? []

  // Auto-select first workflow
  if (!selectedWorkflow && workflows.length > 0) {
    setSelectedWorkflow(workflows[0])
  }

  return (
    <div className="flex flex-col h-screen">
      {/* Header */}
      <div className="shrink-0 border-b border-slate-800 px-6 py-4">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-bold text-white">
              {project?.name ?? 'Loading...'}
            </h1>
            {project?.repositoryUrl && (
              <p className="text-gray-500 text-xs mt-1 font-mono flex items-center gap-1">
                <GitBranch className="w-3 h-3" />
                {project.repositoryUrl}
                {project.defaultBranch && ` (${project.defaultBranch})`}
              </p>
            )}
          </div>

          {/* Workflow tabs */}
          {workflows.length > 0 && (
            <div className="flex gap-1 bg-slate-900 rounded-lg p-1">
              {workflows.map((wf) => (
                <button
                  key={wf.id}
                  onClick={() => setSelectedWorkflow(wf)}
                  className={`px-3 py-1.5 text-sm rounded-md transition-colors ${
                    selectedWorkflow?.id === wf.id
                      ? 'bg-slate-700 text-white'
                      : 'text-gray-400 hover:text-gray-200'
                  }`}
                >
                  {wf.name}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Task Board */}
      {selectedWorkflow ? (
        <TaskBoard
          projectId={projectId}
          workflow={selectedWorkflow}
        />
      ) : (
        <div className="flex-1 flex items-center justify-center text-gray-500">
          No workflow found for this project.
        </div>
      )}
    </div>
  )
}
