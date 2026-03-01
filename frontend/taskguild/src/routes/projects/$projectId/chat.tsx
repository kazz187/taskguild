import { useCallback, useRef, useEffect, useMemo } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTasks } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { listInteractions, respondToInteraction } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { ChatBubble } from '@/components/ChatBubble'
import { PendingRequestsPanel } from '@/components/PendingRequestsPanel'
import { shortId } from '@/lib/id'
import { ArrowLeft } from 'lucide-react'
import { ConnectionIndicator } from '@/components/ConnectionIndicator'

export const Route = createFileRoute('/projects/$projectId/chat')({
  component: ProjectChatPage,
})

function ProjectChatPage() {
  const { projectId } = Route.useParams()
  const scrollRef = useRef<HTMLDivElement>(null)
  const bellAudioRef = useRef<HTMLAudioElement | null>(null)
  const prevPendingCountRef = useRef(0)

  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: tasksData, refetch: refetchTasks } = useQuery(listTasks, { projectId })
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, { projectId, pagination: { limit: 0 } })

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

  const eventTypes = useMemo(() => [
    EventType.TASK_CREATED, EventType.TASK_UPDATED, EventType.INTERACTION_CREATED, EventType.INTERACTION_RESPONDED,
  ], [])

  const { connectionStatus, reconnect } = useEventSubscription(eventTypes, projectId, onEvent)

  // Pending requests (permission requests and questions)
  const pendingRequests = useMemo(
    () =>
      interactions.filter(
        (i) =>
          (i.type === InteractionType.PERMISSION_REQUEST ||
            i.type === InteractionType.QUESTION) &&
          i.status === InteractionStatus.PENDING,
      ),
    [interactions],
  )
  const pendingRequestCount = pendingRequests.length

  // Play notification sound when new pending requests arrive
  useEffect(() => {
    if (pendingRequestCount > prevPendingCountRef.current) {
      if (!bellAudioRef.current) {
        bellAudioRef.current = new Audio('/bell.mp3')
      }
      bellAudioRef.current.currentTime = 0
      bellAudioRef.current.play().catch(() => {})
    }
    prevPendingCountRef.current = pendingRequestCount
  }, [pendingRequestCount])

  // Auto-scroll to bottom when new interactions arrive
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [interactions.length])

  // Synchronous guard to prevent duplicate responses (survives across renders before mutation state propagates)
  const respondedIdsRef = useRef<Set<string>>(new Set())

  const handleRespond = useCallback((interactionId: string, response: string) => {
    if (respondedIdsRef.current.has(interactionId)) return
    respondedIdsRef.current.add(interactionId)
    respondMut.mutate(
      { id: interactionId, response },
      {
        onSuccess: () => refetchInteractions(),
        onError: () => {
          // Allow retry on failure
          respondedIdsRef.current.delete(interactionId)
        },
      },
    )
  }, [respondMut, refetchInteractions])

  return (
    <div className="flex flex-col h-screen">
      {/* Header */}
      <div className="shrink-0 border-b border-slate-800 px-4 py-3 md:px-6 md:py-4">
        <div className="flex items-center gap-3 text-sm text-gray-400 mb-1">
          <Link
            to="/projects/$projectId"
            params={{ projectId }}
            className="hover:text-white transition-colors flex items-center gap-1"
          >
            <ArrowLeft className="w-4 h-4" />
            <span className="hidden sm:inline">{project?.name ?? 'Project'}</span>
            <span className="sm:hidden">Back</span>
          </Link>
        </div>
        <h1 className="text-lg md:text-xl font-bold text-white">Chat</h1>
        <p className="text-xs text-gray-500 mt-1">
          All interactions across {tasks.length} task{tasks.length !== 1 ? 's' : ''}
        </p>
      </div>

      {/* Pending requests section — pinned between header and chat */}
      {pendingRequests.length > 0 && (
        <div className="shrink-0 border-b border-slate-800 bg-slate-800/50 px-4 md:px-6 py-3">
          <div className="max-w-3xl mx-auto">
            <PendingRequestsPanel
              pendingRequests={pendingRequests}
              onRespond={handleRespond}
              isRespondPending={respondMut.isPending}
              taskMap={taskMap}
              projectId={projectId}
            />
          </div>
        </div>
      )}

      {/* Chat area */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto px-4 py-4 md:px-6 md:py-6 space-y-3">
          {interactions.length === 0 && (
            <p className="text-gray-500 text-sm text-center py-12">No interactions yet.</p>
          )}
          {interactions.map((interaction, idx) => {
            // Show task label when task changes
            const prevTaskId = idx > 0 ? interactions[idx - 1].taskId : null
            const showTaskLabel = interaction.taskId !== prevTaskId
            const taskTitle = taskMap.get(interaction.taskId) ?? shortId(interaction.taskId)

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
                  hideActions={
                    interaction.status === InteractionStatus.PENDING &&
                    (interaction.type === InteractionType.PERMISSION_REQUEST ||
                      interaction.type === InteractionType.QUESTION)
                  }
                />
              </div>
            )
          })}
        </div>
      </div>

      {/* Connection status bar */}
      <div className="shrink-0 border-t border-slate-800 px-6 py-2">
        <div className="max-w-3xl mx-auto flex items-center gap-2">
          <ConnectionIndicator status={connectionStatus} onReconnect={reconnect} />
          <span className="text-[11px] text-gray-500">
            {connectionStatus === 'connected' ? 'Connected' : connectionStatus === 'connecting' ? 'Connecting...' : 'Disconnected'}
          </span>
        </div>
      </div>
    </div>
  )
}
