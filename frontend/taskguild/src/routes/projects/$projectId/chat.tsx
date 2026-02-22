import { useCallback, useRef, useEffect, useMemo } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTasks } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { listInteractions, respondToInteraction } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { ChatBubble } from '@/components/ChatBubble'
import { ArrowLeft } from 'lucide-react'

export const Route = createFileRoute('/projects/$projectId/chat')({
  component: ProjectChatPage,
})

function ProjectChatPage() {
  const { projectId } = Route.useParams()
  const scrollRef = useRef<HTMLDivElement>(null)

  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: tasksData, refetch: refetchTasks } = useQuery(listTasks, { projectId })
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, { projectId })

  const respondMut = useMutation(respondToInteraction)

  const project = projectData?.project
  const tasks = tasksData?.tasks ?? []
  const interactions = interactionsData?.interactions ?? []

  // Build task title map
  const taskMap = useMemo(() => {
    const m = new Map<string, string>()
    for (const t of tasks) {
      m.set(t.id, t.title)
    }
    return m
  }, [tasks])

  const onEvent = useCallback(() => {
    refetchTasks()
    refetchInteractions()
  }, [refetchTasks, refetchInteractions])

  useEventSubscription(
    [EventType.TASK_CREATED, EventType.TASK_UPDATED, EventType.INTERACTION_CREATED, EventType.INTERACTION_RESPONDED],
    projectId,
    onEvent,
  )

  // Auto-scroll to bottom when new interactions arrive
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [interactions.length])

  const handleRespond = (interactionId: string, response: string) => {
    respondMut.mutate(
      { id: interactionId, response },
      { onSuccess: () => refetchInteractions() },
    )
  }

  return (
    <div className="flex flex-col h-screen">
      {/* Header */}
      <div className="shrink-0 border-b border-slate-800 px-6 py-4">
        <div className="flex items-center gap-3 text-sm text-gray-400 mb-1">
          <Link
            to="/projects/$projectId"
            params={{ projectId }}
            className="hover:text-white transition-colors flex items-center gap-1"
          >
            <ArrowLeft className="w-4 h-4" />
            {project?.name ?? 'Project'}
          </Link>
        </div>
        <h1 className="text-xl font-bold text-white">Chat</h1>
        <p className="text-xs text-gray-500 mt-1">
          All interactions across {tasks.length} task{tasks.length !== 1 ? 's' : ''}
        </p>
      </div>

      {/* Chat area */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto px-6 py-6 space-y-3">
          {interactions.length === 0 && (
            <p className="text-gray-500 text-sm text-center py-12">No interactions yet.</p>
          )}
          {interactions.map((interaction, idx) => {
            // Show task label when task changes
            const prevTaskId = idx > 0 ? interactions[idx - 1].taskId : null
            const showTaskLabel = interaction.taskId !== prevTaskId
            const taskTitle = taskMap.get(interaction.taskId) ?? interaction.taskId.slice(0, 8)

            return (
              <div key={interaction.id}>
                {showTaskLabel && (
                  <div className="flex items-center gap-2 pt-3 pb-1">
                    <Link
                      to="/projects/$projectId/tasks/$taskId"
                      params={{ projectId, taskId: interaction.taskId }}
                      className="text-[11px] text-cyan-400 hover:text-cyan-300 font-medium truncate transition-colors"
                    >
                      {taskTitle}
                    </Link>
                    <div className="flex-1 border-t border-slate-800" />
                  </div>
                )}
                <ChatBubble
                  interaction={interaction}
                  onRespond={handleRespond}
                  isRespondPending={respondMut.isPending}
                />
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
