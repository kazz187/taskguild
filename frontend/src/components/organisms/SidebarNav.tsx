import { useMemo, useState } from 'react'
import { Link, useMatchRoute } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listProjects, reorderProjects } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
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
import { ChevronRight, ChevronDown, MessageSquare, Bot, Sparkles, Terminal, Shield, Workflow, GitFork, Layers, Settings } from 'lucide-react'

export function SidebarNav() {
  const { data, refetch } = useQuery(listProjects, {})
  const projects = data?.projects ?? []
  const matchRoute = useMatchRoute()

  const visibleProjects = useMemo(() => {
    // Find the currently active project ID from the route
    const activeProject = projects.find(
      (p) => !!matchRoute({ to: '/projects/$projectId', params: { projectId: p.id }, fuzzy: true }),
    )

    return projects.filter((p) => {
      // Always show projects that are not hidden
      if (!p.hiddenFromSidebar) return true
      // Temporarily show hidden projects if they are currently active
      if (activeProject && p.id === activeProject.id) return true
      return false
    })
  }, [projects, matchRoute])

  const reorderMut = useMutation(reorderProjects)

  const [orderedIds, setOrderedIds] = useState<string[] | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
  )

  // Build an ordered project list: use local ordering during drag, server data otherwise.
  const sortedProjects = useMemo(() => {
    if (!orderedIds) return visibleProjects
    const byId = new Map(projects.map((p) => [p.id, p]))
    const allSorted = orderedIds.map((id) => byId.get(id)).filter(Boolean) as typeof projects
    const visibleIds = new Set(visibleProjects.map((p) => p.id))
    return allSorted.filter((p) => visibleIds.has(p.id))
  }, [projects, visibleProjects, orderedIds])

  const projectIds = useMemo(() => sortedProjects.map((p) => p.id), [sortedProjects])

  const activeProject = useMemo(
    () => (activeId ? projects.find((p) => p.id === activeId) : null),
    [activeId, projects],
  )

  function handleDragStart(event: DragStartEvent) {
    setActiveId(event.active.id as string)
    // Capture current ordering for optimistic reorder
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
    <div className="space-y-1">
      <div className="space-y-0.5 mb-3 pb-3 border-b border-slate-800">
        <Link
          to="/global-chat"
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-lg transition-colors"
          activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1.5 text-sm text-white bg-slate-800/60 rounded-lg' }}
        >
          <MessageSquare className="w-3.5 h-3.5 text-cyan-400" />
          Global Chats
        </Link>
        <Link
          to="/templates"
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-lg transition-colors"
          activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1.5 text-sm text-white bg-slate-800/60 rounded-lg' }}
        >
          <Layers className="w-3.5 h-3.5 text-amber-400" />
          Templates
        </Link>
      </div>

      <p className="px-3 py-1.5 text-[11px] uppercase tracking-wider text-gray-500 font-semibold">
        Projects
      </p>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={projectIds} strategy={verticalListSortingStrategy}>
          {sortedProjects.map((project) => (
            <SortableProjectNode
              key={project.id}
              projectId={project.id}
              name={project.name}
              isDragging={activeId === project.id}
            />
          ))}
        </SortableContext>
        <DragOverlay>
          {activeProject ? (
            <ProjectNodeOverlay name={activeProject.name} />
          ) : null}
        </DragOverlay>
      </DndContext>
    </div>
  )
}

function SortableProjectNode({ projectId, name, isDragging }: { projectId: string; name: string; isDragging: boolean }) {
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

  const matchRoute = useMatchRoute()
  const isActive = !!matchRoute({ to: '/projects/$projectId', params: { projectId }, fuzzy: true })
  const [expanded, setExpanded] = useState(true)

  return (
    <div ref={setNodeRef} style={style}>
      <button
        onClick={() => setExpanded((e) => !e)}
        className={`w-full flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg transition-colors cursor-grab active:cursor-grabbing ${
          isActive ? 'text-white bg-slate-800/60' : 'text-gray-400 hover:text-gray-200 hover:bg-slate-800/40'
        }`}
        {...attributes}
        {...listeners}
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

/** Static overlay shown during drag */
function ProjectNodeOverlay({ name }: { name: string }) {
  return (
    <div className="w-full flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-slate-800 text-white shadow-lg border border-cyan-500/50 cursor-grabbing">
      <ChevronDown className="w-3.5 h-3.5 shrink-0 text-gray-500" />
      <span className="truncate">{name}</span>
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
        to="/projects/$projectId/scripts"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <Terminal className="w-3 h-3" />
        Scripts
      </Link>
      <Link
        to="/projects/$projectId/permissions"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <Shield className="w-3 h-3" />
        Permissions
      </Link>
      <Link
        to="/projects/$projectId/worktrees"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <GitFork className="w-3 h-3" />
        Worktrees
      </Link>
      <Link
        to="/projects/$projectId/settings"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <Settings className="w-3 h-3" />
        Settings
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
      <Link
        to="/projects/$projectId/workflows"
        params={{ projectId }}
        className="flex items-center gap-1.5 px-3 py-1 text-xs text-gray-400 hover:text-white hover:bg-slate-800/40 rounded-md transition-colors"
        activeProps={{ className: 'flex items-center gap-1.5 px-3 py-1 text-xs text-white bg-slate-800 rounded-md' }}
      >
        <Workflow className="w-3 h-3 shrink-0" />
        <span>Workflows</span>
      </Link>
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
