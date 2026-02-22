import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { listProjects } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { Folder, ArrowRight } from 'lucide-react'

export const Route = createFileRoute('/')({ component: ProjectsPage })

function ProjectsPage() {
  const { data, isLoading, error } = useQuery(listProjects, {})

  return (
    <div className="p-8 max-w-4xl">
      <h1 className="text-2xl font-bold text-white mb-6">Projects</h1>

      {isLoading && <p className="text-gray-400">Loading projects...</p>}
      {error && (
        <p className="text-red-400">Failed to load projects: {error.message}</p>
      )}

      {data && data.projects.length === 0 && (
        <p className="text-gray-500">No projects found.</p>
      )}

      <div className="grid gap-4">
        {data?.projects.map((project) => (
          <Link
            key={project.id}
            to="/projects/$projectId"
            params={{ projectId: project.id }}
            className="block bg-slate-900 border border-slate-800 rounded-xl p-5 hover:border-cyan-500/50 transition-all group"
          >
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3">
                <Folder className="w-5 h-5 text-cyan-400 mt-0.5" />
                <div>
                  <h2 className="text-lg font-semibold text-white group-hover:text-cyan-400 transition-colors">
                    {project.name}
                  </h2>
                  {project.description && (
                    <p className="text-gray-400 text-sm mt-1">
                      {project.description}
                    </p>
                  )}
                  {project.repositoryUrl && (
                    <p className="text-gray-500 text-xs mt-2 font-mono">
                      {project.repositoryUrl}
                    </p>
                  )}
                </div>
              </div>
              <ArrowRight className="w-5 h-5 text-gray-600 group-hover:text-cyan-400 transition-colors mt-1" />
            </div>
          </Link>
        ))}
      </div>
    </div>
  )
}
