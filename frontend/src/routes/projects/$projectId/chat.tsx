import { useCallback, useRef, useEffect, useMemo } from 'react'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTasks } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { listInteractions, respondToInteraction, expireInteraction } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { listTaskLogs } from '@taskguild/proto/taskguild/v1/task_log-TaskLogService_connectquery.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { TaskLogCategory } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { useNotificationSound } from '@/hooks/useNotificationSound'
import { TimelineEntry, type TimelineItem } from '@/components/organisms/TimelineEntry'
import { PendingRequestsPanel } from '@/components/organisms/PendingRequestsPanel'
import { shortId } from '@/lib/id'
import { ArrowLeft, MessageSquare } from 'lucide-react'
import { PageHeading } from '@/components/molecules/index.ts'
import { Badge } from '@/components/atoms/index.ts'
import { ConnectionIndicator } from '@/components/organisms/ConnectionIndicator'

export const Route = createFileRoute('/projects/$projectId/chat')({
  component: ProjectChatPage,
})

function ProjectChatPage() {
  useDocumentTitle('Chat')
  const { projectId } = Route.useParams()
  const scrollRef = useRef<HTMLDivElement>(null)

  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: tasksData, refetch: refetchTasks } = useQuery(listTasks, { projectId })
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, { projectId, pagination: { limit: 0 } })
  const { data: logsData, refetch: refetchLogs } = useQuery(listTaskLogs, {
    taskId: '',
    projectId,
    pagination: { limit: 200 },
  })

  const respondMut = useMutation(respondToInteraction)
  const expireMut = useMutation(expireInteraction)

  const project = projectData?.project
  const tasks = tasksData?.tasks ?? []
  const interactions = interactionsData?.interactions ?? []
  const logs = logsData?.logs ?? []

  // Build task title map
  const taskMap = useMemo(() => {
    const m = new Map<string, string>()
    for (const t of tasks) {
      m.set(t.id, t.title)
    }
    // Supplement from listInteractions response (includes archived tasks)
    if (interactionsData?.taskTitles) {
      for (const [id, title] of Object.entries(interactionsData.taskTitles)) {
        if (!m.has(id)) m.set(id, title)
      }
    }
    // Supplement from listTaskLogs response (includes archived tasks)
    if (logsData?.taskTitles) {
      for (const [id, title] of Object.entries(logsData.taskTitles)) {
        if (!m.has(id)) m.set(id, title)
      }
    }
    return m
  }, [tasks, interactionsData?.taskTitles, logsData?.taskTitles])

  // Filter logs to only RESULT category (avoid noise from TOOL_USE, STDERR, etc.)
  const filteredLogs = useMemo(
    () => logs.filter((l) => l.category === TaskLogCategory.RESULT),
    [logs],
  )

  // Merge interactions + filtered logs into unified timeline sorted by createdAt
  const timelineItems = useMemo<TimelineItem[]>(() => {
    const items: TimelineItem[] = [
      ...interactions.map((interaction): TimelineItem => ({ kind: 'interaction', interaction })),
      ...filteredLogs.map((log): TimelineItem => ({ kind: 'log', log })),
    ]
    items.sort((a, b) => {
      const tsA = a.kind === 'interaction' ? a.interaction.createdAt : a.log.createdAt
      const tsB = b.kind === 'interaction' ? b.interaction.createdAt : b.log.createdAt
      if (!tsA || !tsB) return 0
      const diff = Number(tsA.seconds) - Number(tsB.seconds)
      if (diff !== 0) return diff
      return tsA.nanos - tsB.nanos
    })
    return items
  }, [interactions, filteredLogs])

  const onEvent = useCallback(() => {
    refetchTasks()
    refetchInteractions()
    refetchLogs()
  }, [refetchTasks, refetchInteractions, refetchLogs])

  const eventTypes = useMemo(() => [
    EventType.TASK_CREATED, EventType.TASK_UPDATED, EventType.INTERACTION_CREATED, EventType.INTERACTION_RESPONDED,
    EventType.TASK_LOG,
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
  useNotificationSound(pendingRequestCount)

  // Auto-scroll to bottom when new items arrive
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [timelineItems.length])

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

  const handleDismiss = useCallback((interactionId: string) => {
    if (respondedIdsRef.current.has(interactionId)) return
    respondedIdsRef.current.add(interactionId)
    expireMut.mutate(
      { id: interactionId },
      {
        onSuccess: () => refetchInteractions(),
        onError: () => {
          respondedIdsRef.current.delete(interactionId)
        },
      },
    )
  }, [expireMut, refetchInteractions])

  return (
    <div className="flex flex-col h-dvh">
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
        <PageHeading icon={MessageSquare} title="Chat" iconColor="text-cyan-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {tasks.length} task{tasks.length !== 1 ? 's' : ''}
          </Badge>
        </PageHeading>
      </div>

      {/* Timeline area */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="max-w-4xl mx-auto px-4 py-4 md:px-6 md:py-6">
          {timelineItems.length === 0 && (
            <p className="text-gray-500 text-sm text-center py-12">No interactions yet.</p>
          )}
          {timelineItems.map((item, idx) => {
            const prev = idx > 0 ? timelineItems[idx - 1] : null
            const taskId = item.kind === 'interaction' ? item.interaction.taskId : item.log.taskId
            const prevTaskId = prev
              ? (prev.kind === 'interaction' ? prev.interaction.taskId : prev.log.taskId)
              : null
            const showTaskLabel = taskId !== prevTaskId
            const taskTitle = taskMap.get(taskId) || shortId(taskId)

            return (
              <div key={item.kind === 'interaction' ? `i-${item.interaction.id}` : `l-${item.log.id}`}>
                {showTaskLabel && (
                  <div className="flex items-center gap-2 pt-3 pb-1">
                    <Link
                      to="/projects/$projectId/tasks/$taskId"
                      params={{ projectId, taskId }}
                      className="text-[11px] text-cyan-400 hover:text-cyan-300 font-medium truncate transition-colors"
                    >
                      {taskTitle}
                    </Link>
                    <div className="flex-1 border-t border-slate-800" />
                  </div>
                )}
                <TimelineEntry item={item} />
              </div>
            )
          })}
        </div>
      </div>

      {/* Pending requests section — pinned above connection bar */}
      {pendingRequests.length > 0 && (
        <div className="shrink-0 border-t border-slate-800 bg-slate-800/50 px-4 md:px-6 py-3">
          <div className="max-w-4xl mx-auto">
            <PendingRequestsPanel
              pendingRequests={pendingRequests}
              onRespond={handleRespond}
              isRespondPending={respondMut.isPending}
              taskMap={taskMap}
              projectId={projectId}
              onDismiss={handleDismiss}
              isDismissPending={expireMut.isPending}
            />
          </div>
        </div>
      )}

      {/* Connection status bar */}
      <div className="shrink-0 border-t border-slate-800 px-6 py-2">
        <div className="max-w-4xl mx-auto flex items-center gap-2">
          <ConnectionIndicator status={connectionStatus} onReconnect={reconnect} />
          <span className="text-[11px] text-gray-500">
            {connectionStatus === 'connected' ? 'Connected' : connectionStatus === 'connecting' ? 'Connecting...' : 'Disconnected'}
          </span>
        </div>
      </div>
    </div>
  )
}
