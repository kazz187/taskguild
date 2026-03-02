import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import type { Workflow } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { TaskBoard } from '@/components/TaskBoard'
import { WorkflowForm } from '@/components/WorkflowForm'
import { useState, useEffect } from 'react'
import { Link } from '@tanstack/react-router'
import { listAgents } from '@taskguild/proto/taskguild/v1/agent-AgentService_connectquery.ts'
import { GitBranch, Plus, Settings, Bot } from 'lucide-react'

type FormMode = { kind: 'create' } | { kind: 'edit'; workflow: Workflow }

export const Route = createFileRoute('/projects/$projectId/')({
  component: ProjectDetailPage,
  validateSearch: (search: Record<string, unknown>): { workflowId?: string } => ({
    workflowId: typeof search.workflowId === 'string' ? search.workflowId : undefined,
  }),
})

function ProjectDetailPage() {
  const { projectId } = Route.useParams()
  const { workflowId } = Route.useSearch()
  const [selectedWorkflow, setSelectedWorkflow] = useState<Workflow | null>(null)
  const [formMode, setFormMode] = useState<FormMode | null>(null)

  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: workflowsData, refetch: refetchWorkflows } = useQuery(listWorkflows, {
    projectId,
  })
  const { data: agentsData } = useQuery(listAgents, { projectId })

  const project = projectData?.project
  const workflows = workflowsData?.workflows ?? []

  // Select workflow from search params or auto-select first.
  // Always sync selectedWorkflow with the latest workflows data so that
  // workflow-level defaults (e.g. defaultPermissionMode, defaultUseWorktree)
  // stay up-to-date when the data is refetched.
  useEffect(() => {
    if (workflows.length === 0) return
    if (workflowId) {
      const target = workflows.find((w) => w.id === workflowId)
      if (target) {
        setSelectedWorkflow(target)
        return
      }
    }
    setSelectedWorkflow((prev: Workflow | null) => {
      if (!prev) return workflows[0]
      // Keep selectedWorkflow in sync with the latest data by matching ID
      return workflows.find((w: Workflow) => w.id === prev.id) ?? workflows[0]
    })
  }, [workflows, workflowId])

  const handleSaved = () => {
    setFormMode(null)
    // Refetch workflows; the useEffect will automatically sync selectedWorkflow
    // with the latest data (including updated defaults).
    refetchWorkflows()
  }

  return (
    <div className="flex flex-col h-dvh">
      {/* Header */}
      <div className="shrink-0 border-b border-slate-800 px-4 py-3 md:px-6 md:py-4">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-3">
          <div className="min-w-0">
            <h1 className="text-lg md:text-xl font-bold text-white truncate">
              {project?.name ?? 'Loading...'}
            </h1>
            {project?.repositoryUrl && (
              <p className="text-gray-500 text-xs mt-1 font-mono flex items-center gap-1 truncate">
                <GitBranch className="w-3 h-3 shrink-0" />
                <span className="truncate">
                  {project.repositoryUrl}
                  {project.defaultBranch && ` (${project.defaultBranch})`}
                </span>
              </p>
            )}
          </div>

          <div className="flex items-center gap-2 overflow-x-auto shrink-0">
            {/* Workflow tabs */}
            {workflows.length > 0 && (
              <div className="flex gap-1 bg-slate-900 rounded-lg p-1 shrink-0">
                {workflows.map((wf) => (
                  <button
                    key={wf.id}
                    onClick={() => {
                      setSelectedWorkflow(wf)
                      setFormMode(null)
                    }}
                    className={`px-2.5 py-1.5 text-xs md:text-sm rounded-md transition-colors whitespace-nowrap ${
                      selectedWorkflow?.id === wf.id && !formMode
                        ? 'bg-slate-700 text-white'
                        : 'text-gray-400 hover:text-gray-200'
                    }`}
                  >
                    {wf.name}
                  </button>
                ))}
              </div>
            )}
            {/* Edit button */}
            {selectedWorkflow && !formMode && (
              <button
                onClick={() => setFormMode({ kind: 'edit', workflow: selectedWorkflow })}
                className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs md:text-sm text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors shrink-0"
              >
                <Settings className="w-4 h-4" />
                <span className="hidden sm:inline">Edit</span>
              </button>
            )}
            <button
              onClick={() => setFormMode({ kind: 'create' })}
              className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs md:text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors shrink-0"
            >
              <Plus className="w-4 h-4" />
              <span className="hidden sm:inline">New Workflow</span>
            </button>
          </div>
        </div>
      </div>

      {/* Content */}
      {formMode ? (
        <WorkflowForm
          projectId={projectId}
          workflow={formMode.kind === 'edit' ? formMode.workflow : undefined}
          onClose={() => setFormMode(null)}
          onSaved={handleSaved}
        />
      ) : selectedWorkflow ? (
        <TaskBoard
          projectId={projectId}
          workflow={selectedWorkflow}
        />
      ) : (
        <div className="flex-1 flex flex-col items-center justify-center text-gray-500 gap-6 p-4">
          <div className="text-center">
            <p className="text-lg font-medium text-gray-400 mb-1">Get started with your project</p>
            <p className="text-sm">Create agents and a workflow to begin managing tasks.</p>
          </div>
          <div className="flex flex-col sm:flex-row items-center gap-3">
            <Link
              to="/projects/$projectId/agents"
              params={{ projectId }}
              className="flex items-center gap-1.5 px-4 py-2 text-sm border border-slate-700 hover:border-slate-600 text-gray-300 hover:text-white rounded-lg transition-colors"
            >
              <Bot className="w-4 h-4" />
              {(agentsData?.agents?.length ?? 0) > 0
                ? `Agents (${agentsData!.agents.length})`
                : 'Create Agents'}
            </Link>
            <button
              onClick={() => setFormMode({ kind: 'create' })}
              className="flex items-center gap-1.5 px-4 py-2 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors"
            >
              <Plus className="w-4 h-4" />
              Create Workflow
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
