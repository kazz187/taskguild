import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listProjects, createProject, reorderProjects, updateProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { Folder, ArrowRight, Plus, X, Eye, EyeOff } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  DndContext,
  DragOverlay,
  closestCenter,
  useSensor,
  useSensors,
  PointerSensor,
  type DragStartEvent,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'

export const Route = createFileRoute('/')({ component: ProjectsPage })

function ProjectsPage() {
  useDocumentTitle('Projects')
  const { data, isLoading, error, refetch } = useQuery(listProjects, {})
  const [showForm, setShowForm] = useState(false)
  const toggleVisibility = useMutation(updateProject)

  const reorderMut = useMutation(reorderProjects)

  const [orderedIds, setOrderedIds] = useState<string[] | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)

  const projects = data?.projects ?? []

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
  )

  // Build an ordered project list: use local ordering during drag, server data otherwise.
  const sortedProjects = useMemo(() => {
    if (!orderedIds) return projects
    const byId = new Map(projects.map((p) => [p.id, p]))
    return orderedIds.map((id) => byId.get(id)).filter(Boolean) as typeof projects
  }, [projects, orderedIds])

  const projectIds = useMemo(() => sortedProjects.map((p) => p.id), [sortedProjects])

  const activeProject = useMemo(
    () => (activeId ? projects.find((p) => p.id === activeId) : null),
    [activeId, projects],
  )

  function handleDragStart(event: DragStartEvent) {
    setActiveId(event.active.id as string)
    setOrderedIds(projects.map((p) => p.id))
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event
    setActiveId(null)

    if (!over || active.id === over.id) {
      setOrderedIds(null)
      return
    }

    const currentIds = orderedIds ?? projects.map((p) => p.id)
    const oldIndex = currentIds.indexOf(active.id as string)
    const newIndex = currentIds.indexOf(over.id as string)
    if (oldIndex === -1 || newIndex === -1) {
      setOrderedIds(null)
      return
    }

    const newIds = arrayMove(currentIds, oldIndex, newIndex)
    setOrderedIds(newIds)

    reorderMut.mutate(
      { projectIds: newIds },
      {
        onSuccess: () => {
          setOrderedIds(null)
          refetch()
        },
        onError: () => {
          setOrderedIds(null)
        },
      },
    )
  }

  return (
    <div className="p-4 md:p-8 max-w-4xl">
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <h1 className="text-xl md:text-2xl font-bold text-white">Projects</h1>
        <button
          onClick={() => setShowForm(true)}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors"
        >
          <Plus className="w-4 h-4" />
          <span className="hidden sm:inline">New Project</span>
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

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={projectIds} strategy={verticalListSortingStrategy}>
          <div className="grid gap-3 md:gap-4">
            {sortedProjects.map((project) => (
              <SortableProjectCard
                key={project.id}
                projectId={project.id}
                name={project.name}
                description={project.description}
                repositoryUrl={project.repositoryUrl}
                hiddenFromSidebar={project.hiddenFromSidebar}
                isDragging={activeId === project.id}
                onToggleVisibility={() => {
                  toggleVisibility.mutate(
                    { id: project.id, hiddenFromSidebar: !project.hiddenFromSidebar },
                    { onSuccess: () => refetch() },
                  )
                }}
              />
            ))}
          </div>
        </SortableContext>
        <DragOverlay>
          {activeProject ? (
            <ProjectCardOverlay
              name={activeProject.name}
              description={activeProject.description}
              repositoryUrl={activeProject.repositoryUrl}
            />
          ) : null}
        </DragOverlay>
      </DndContext>
    </div>
  )
}

interface SortableProjectCardProps {
  projectId: string
  name: string
  description: string
  repositoryUrl: string
  hiddenFromSidebar: boolean
  isDragging: boolean
  onToggleVisibility: () => void
}

function SortableProjectCard({ projectId, name, description, repositoryUrl, hiddenFromSidebar, isDragging, onToggleVisibility }: SortableProjectCardProps) {
  const navigate = useNavigate()
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
  } = useSortable({ id: projectId })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  }

  return (
    <div
      ref={setNodeRef}
      style={style}
      className="block bg-slate-900 border border-slate-800 rounded-xl p-4 md:p-5 hover:border-cyan-500/50 transition-all group active:bg-slate-800/50 cursor-grab active:cursor-grabbing"
      onClick={() => navigate({ to: '/projects/$projectId', params: { projectId } })}
      {...attributes}
      {...listeners}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 min-w-0 flex-1">
          <Folder className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
          <div className="min-w-0">
            <h2 className="text-base md:text-lg font-semibold text-white group-hover:text-cyan-400 transition-colors">
              {name}
            </h2>
            {description && (
              <p className="text-gray-400 text-sm mt-1 line-clamp-2">
                {description}
              </p>
            )}
            {repositoryUrl && (
              <p className="text-gray-500 text-xs mt-2 font-mono truncate">
                {repositoryUrl}
              </p>
            )}
          </div>
        </div>
        <div className="flex items-center gap-1 mt-1 shrink-0">
          <button
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              onToggleVisibility()
            }}
            className={`p-1 rounded transition-colors ${
              hiddenFromSidebar
                ? 'text-gray-600 hover:text-gray-400'
                : 'text-cyan-400 hover:text-cyan-300'
            }`}
            title={hiddenFromSidebar ? 'Show in sidebar' : 'Hide from sidebar'}
          >
            {hiddenFromSidebar ? (
              <EyeOff className="w-4 h-4" />
            ) : (
              <Eye className="w-4 h-4" />
            )}
          </button>
          <ArrowRight className="w-5 h-5 text-gray-600 group-hover:text-cyan-400 transition-colors" />
        </div>
      </div>
    </div>
  )
}

function ProjectCardOverlay({ name, description, repositoryUrl }: { name: string; description: string; repositoryUrl: string }) {
  return (
    <div className="block bg-slate-900 border border-cyan-500/50 rounded-xl p-4 md:p-5 shadow-lg cursor-grabbing">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 min-w-0 flex-1">
          <Folder className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
          <div className="min-w-0">
            <h2 className="text-base md:text-lg font-semibold text-white">
              {name}
            </h2>
            {description && (
              <p className="text-gray-400 text-sm mt-1 line-clamp-2">
                {description}
              </p>
            )}
            {repositoryUrl && (
              <p className="text-gray-500 text-xs mt-2 font-mono truncate">
                {repositoryUrl}
              </p>
            )}
          </div>
        </div>
        <ArrowRight className="w-5 h-5 text-gray-600 mt-1 shrink-0" />
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
      className="bg-slate-900 border border-slate-800 rounded-xl p-4 md:p-5 mb-4 md:mb-6"
    >
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">New Project</h2>
        <button
          type="button"
          onClick={onClose}
          className="text-gray-500 hover:text-gray-300 transition-colors p-1"
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
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
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
