import { useState } from 'react'
import { Link, useMatchRoute } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { listProjects } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { ChevronRight, ChevronDown, MessageSquare, Bot, Sparkles, Workflow } from 'lucide-react'

export function SidebarNav() {
  const { data } = useQuery(listProjects, {})
  const projects = data?.projects ?? []

  return (
    <div className="space-y-1">
      <p className="px-3 py-1.5 text-[11px] uppercase tracking-wider text-gray-500 font-semibold">
        Projects
      </p>
      {projects.map((project) => (
        <ProjectNode key={project.id} projectId={project.id} name={project.name} />
      ))}
    </div>
  )
}

function ProjectNode({ projectId, name }: { projectId: string; name: string }) {
  const matchRoute = useMatchRoute()
  const isActive = !!matchRoute({ to: '/projects/$projectId', params: { projectId }, fuzzy: true })
  const [expanded, setExpanded] = useState(true)

  return (
    <div>
      <button
        onClick={() => setExpanded((e) => !e)}
        className={`w-full flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg transition-colors ${
          isActive ? 'text-white bg-slate-800/60' : 'text-gray-400 hover:text-gray-200 hover:bg-slate-800/40'
        }`}
      >
        {expanded ? (
          <ChevronDown className="w-3.5 h-3.5 shrink-0 text-gray-500" />
        ) : (
          <ChevronRight className="w-3.5 h-3.5 shrink-0 text-gray-500" />
        )}
        <span className="truncate">{name}</span>
      </button>
      {expanded && <ProjectChildren projectId={projectId} />}
    </div>
  )
}

function ProjectChildren({ projectId }: { projectId: string }) {
  const { data } = useQuery(listWorkflows, { projectId })
  const workflows = data?.workflows ?? []

  return (
    <div className="ml-4 border-l border-slate-800 pl-2 space-y-0.5 py-0.5">
      <WorkflowsNode projectId={projectId} workflows={workflows} />
      <Link
        to="/projects/$projectId/agents"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <Bot className="w-3 h-3" />
        Agents
      </Link>
      <Link
        to="/projects/$projectId/skills"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <Sparkles className="w-3 h-3" />
        Skills
      </Link>
      <Link
        to="/projects/$projectId/chat"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <MessageSquare className="w-3 h-3" />
        Chat
      </Link>
    </div>
  )
}

function WorkflowsNode({ projectId, workflows }: { projectId: string; workflows: { id: string; name: string }[] }) {
  return (
    <div>
      <div className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400">
        <Workflow className="w-3 h-3 shrink-0" />
        <span>Workflows</span>
      </div>
      {workflows.length > 0 && (
        <div className="ml-3 border-l border-slate-800 pl-2 space-y-0.5 py-0.5">
          {workflows.map((wf) => (
            <Link
              key={wf.id}
              to="/projects/$projectId"
              params={{ projectId }}
              search={{ workflowId: wf.id }}
              className="block px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors truncate"
              activeProps={{ className: 'block px-3 py-1 text-xs text-white bg-slate-800 rounded-md truncate' }}
            >
              {wf.name}
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
