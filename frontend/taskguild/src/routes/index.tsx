import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listProjects, createProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { Folder, ArrowRight, Plus, X } from 'lucide-react'
import { useState } from 'react'

export const Route = createFileRoute('/')({ component: ProjectsPage })

function ProjectsPage() {
  const { data, isLoading, error, refetch } = useQuery(listProjects, {})
  const [showForm, setShowForm] = useState(false)

  return (
    <div className="p-8 max-w-4xl">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Projects</h1>
        <button
          onClick={() => setShowForm(true)}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors"
        >
          <Plus className="w-4 h-4" />
          New Project
        </button>
      </div>

      {showForm && (
        <CreateProjectForm
          onClose={() => setShowForm(false)}
          onCreated={() => {
            setShowForm(false)
            refetch()
          }}
        />
      )}

      {isLoading && <p className="text-gray-400">Loading projects...</p>}
      {error && (
        <p className="text-red-400">Failed to load projects: {error.message}</p>
      )}

      {data && data.projects.length === 0 && !showForm && (
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

function CreateProjectForm({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [repositoryUrl, setRepositoryUrl] = useState('')
  const [defaultBranch, setDefaultBranch] = useState('main')

  const mutation = useMutation(createProject)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    mutation.mutate(
      { name, description, repositoryUrl, defaultBranch },
      { onSuccess: onCreated },
    )
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-6"
    >
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">New Project</h2>
        <button
          type="button"
          onClick={onClose}
          className="text-gray-500 hover:text-gray-300 transition-colors"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      <div className="space-y-3">
        <div>
          <label className="block text-sm text-gray-400 mb-1">Name *</label>
          <input
            type="text"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
            placeholder="My Project"
          />
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">Description</label>
          <input
            type="text"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
            placeholder="Project description"
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-sm text-gray-400 mb-1">Repository URL</label>
            <input
              type="text"
              value={repositoryUrl}
              onChange={(e) => setRepositoryUrl(e.target.value)}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
              placeholder="https://github.com/org/repo"
            />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Default Branch</label>
            <input
              type="text"
              value={defaultBranch}
              onChange={(e) => setDefaultBranch(e.target.value)}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
              placeholder="main"
            />
          </div>
        </div>
      </div>

      {mutation.error && (
        <p className="text-red-400 text-sm mt-3">{mutation.error.message}</p>
      )}

      <div className="flex justify-end gap-2 mt-4">
        <button
          type="button"
          onClick={onClose}
          className="px-3 py-1.5 text-sm text-gray-400 hover:text-white transition-colors"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={mutation.isPending || !name}
          className="px-4 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
        >
          {mutation.isPending ? 'Creating...' : 'Create'}
        </button>
      </div>
    </form>
  )
}
