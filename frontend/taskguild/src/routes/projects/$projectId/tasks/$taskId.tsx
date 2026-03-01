import { useState, useCallback, useRef, useEffect, useMemo } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getTask, updateTaskStatus } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { listInteractions, respondToInteraction, sendMessage } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { useTaskLogs } from '@/hooks/useTaskLogs'
import { TaskDetailModal } from '@/components/TaskDetailModal'
import { shortId } from '@/lib/id'
import { InputBar } from '@/components/ChatBubble'
import { MarkdownDescription } from '@/components/MarkdownDescription'
import { TimelineEntry, type TimelineItem } from '@/components/TimelineEntry'
import { PendingRequestsPanel } from '@/components/PendingRequestsPanel'
import { ConnectionIndicator } from '@/components/ConnectionIndicator'
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  Clock,
  ExternalLink,
  GitBranch,
  Loader,
  Pencil,
} from 'lucide-react'

export const Route = createFileRoute('/projects/$projectId/tasks/$taskId')({
  component: TaskDetailPage,
})

function TaskDetailPage() {
  const { projectId, taskId } = Route.useParams()
  const [showEditModal, setShowEditModal] = useState(false)
  const timelineScrollRef = useRef<HTMLDivElement>(null)
  const prevTimelineCountRef = useRef(0)
  const prevPendingCountRef = useRef(0)
  const bellAudioRef = useRef<HTMLAudioElement | null>(null)

  const { data: taskData, refetch: refetchTask } = useQuery(getTask, { id: taskId })
  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: workflowsData } = useQuery(listWorkflows, { projectId })
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, { taskId, pagination: { limit: 0 } })

  const statusMut = useMutation(updateTaskStatus)
  const respondMut = useMutation(respondToInteraction)
  const sendMut = useMutation(sendMessage)

  const task = taskData?.task
  const project = projectData?.project
  const workflows = workflowsData?.workflows ?? []
  const interactions = interactionsData?.interactions ?? []

  const workflow = workflows.find((w) => w.id === task?.workflowId)
  const sortedStatuses = workflow ? [...workflow.statuses].sort((a, b) => a.order - b.order) : []
  const currentStatus = sortedStatuses.find((s) => s.id === task?.statusId)
  const allowedTransitions = currentStatus?.transitionsTo ?? []

  // Fetch task logs
  const { logs } = useTaskLogs(taskId, projectId)

  // Merge interactions + logs into a sorted timeline
  const timelineItems = useMemo<TimelineItem[]>(() => {
    const items: TimelineItem[] = [
      ...interactions.map((interaction): TimelineItem => ({ kind: 'interaction', interaction })),
      ...logs.map((log): TimelineItem => ({ kind: 'log', log })),
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
  }, [interactions, logs])

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

  const pendingInteraction = interactions.find((i) => i.status === InteractionStatus.PENDING)

  const onEvent = useCallback(() => {
    refetchTask()
    refetchInteractions()
  }, [refetchTask, refetchInteractions])

  const eventTypes = useMemo(() => [
    EventType.TASK_UPDATED, EventType.TASK_STATUS_CHANGED, EventType.AGENT_ASSIGNED,
    EventType.AGENT_STATUS_CHANGED, EventType.INTERACTION_CREATED, EventType.INTERACTION_RESPONDED,
    EventType.TASK_LOG,
  ], [])

  const { connectionStatus, reconnect } = useEventSubscription(eventTypes, projectId, onEvent)

  // Auto-scroll timeline to bottom on new entries
  useEffect(() => {
    if (timelineScrollRef.current && timelineItems.length > prevTimelineCountRef.current) {
      timelineScrollRef.current.scrollTop = timelineScrollRef.current.scrollHeight
    }
    prevTimelineCountRef.current = timelineItems.length
  }, [timelineItems.length])

  // Play notification sound when new pending requests arrive
  useEffect(() => {
    if (pendingRequests.length > prevPendingCountRef.current) {
      if (!bellAudioRef.current) {
        bellAudioRef.current = new Audio('/bell.mp3')
      }
      bellAudioRef.current.currentTime = 0
      bellAudioRef.current.play().catch(() => {})
    }
    prevPendingCountRef.current = pendingRequests.length
  }, [pendingRequests.length])

  const handleStatusChange = (statusId: string) => {
    if (!task) return
    statusMut.mutate(
      { id: task.id, statusId },
      { onSuccess: () => refetchTask() },
    )
  }

  const handleRespond = (interactionId: string, response: string) => {
    respondMut.mutate(
      { id: interactionId, response },
      { onSuccess: () => refetchInteractions() },
    )
  }

  const handleSendMessage = (message: string) => {
    sendMut.mutate(
      { taskId, message },
      { onSuccess: () => refetchInteractions() },
    )
  }

  if (!task) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        Loading...
      </div>
    )
  }

  const metadata = task.metadata ?? {}
  const resultSummary = metadata['result_summary'] ?? ''
  const metadataEntries = Object.entries(metadata).filter(([key, v]) => v && key !== 'result_summary' && key !== 'result_status' && key !== 'result_error')

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="shrink-0 border-b border-slate-800 px-4 py-3 md:px-6 md:py-4">
        <div className="flex items-center gap-2 md:gap-3 text-sm text-gray-400 mb-2 md:mb-3">
          <Link
            to="/projects/$projectId"
            params={{ projectId }}
            className="hover:text-white transition-colors flex items-center gap-1 shrink-0"
          >
            <ArrowLeft className="w-4 h-4" />
            <span className="hidden sm:inline">{project?.name ?? 'Project'}</span>
            <span className="sm:hidden">Back</span>
          </Link>
          <span className="text-gray-600">/</span>
          <span className="text-gray-300 truncate">{task.title}</span>
        </div>

        <div className="flex flex-col md:flex-row md:items-start md:justify-between gap-3 md:gap-4">
          <div className="flex-1 min-w-0">
            <h1 className="text-lg md:text-xl font-bold text-white">{task.title}</h1>
            <div className="flex items-center gap-2 mt-2 flex-wrap">
              <span className={`text-xs px-2.5 py-1 rounded-full font-medium ${
                currentStatus?.isInitial ? 'bg-blue-500/20 text-blue-400' :
                currentStatus?.isTerminal ? 'bg-green-500/20 text-green-400' :
                'bg-gray-500/20 text-gray-300'
              }`}>
                {currentStatus?.name ?? task.statusId}
              </span>

              {task.assignmentStatus === TaskAssignmentStatus.ASSIGNED ? (
                <span className="inline-flex items-center gap-1 text-xs bg-cyan-500/10 text-cyan-400 border border-cyan-500/20 rounded-full px-2.5 py-1">
                  <Bot className="w-3 h-3" />
                  {shortId(task.assignedAgentId)}
                </span>
              ) : task.assignmentStatus === TaskAssignmentStatus.PENDING ? (
                <span className="inline-flex items-center gap-1 text-xs bg-yellow-500/10 text-yellow-400 border border-yellow-500/20 rounded-full px-2.5 py-1">
                  <Loader className="w-3 h-3" />
                  Pending claim
                </span>
              ) : (
                <span className="inline-flex items-center gap-1 text-xs text-gray-500">
                  <Clock className="w-3 h-3" />
                  Unassigned
                </span>
              )}

              {allowedTransitions.map((toId) => {
                const toStatus = sortedStatuses.find((s) => s.id === toId)
                return (
                  <button
                    key={toId}
                    onClick={() => handleStatusChange(toId)}
                    disabled={statusMut.isPending}
                    className="flex items-center gap-1 px-3 py-1 text-xs bg-slate-800 border border-slate-700 rounded-lg text-gray-300 hover:border-cyan-500/50 hover:text-white transition-colors disabled:opacity-50"
                  >
                    <ArrowRight className="w-3 h-3" />
                    {toStatus?.name ?? toId}
                  </button>
                )
              })}

              <span className="text-[11px] text-gray-600 font-mono ml-auto hidden sm:inline">{task.id}</span>
            </div>
          </div>

          <button
            onClick={() => setShowEditModal(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors shrink-0 self-start"
          >
            <Pencil className="w-4 h-4" />
            Edit
          </button>
        </div>
      </div>

      {/* Pending requests section — pinned between header and timeline */}
      {pendingRequests.length > 0 && (
        <div className="shrink-0 border-b border-slate-800 px-4 md:px-6 py-3">
          <div className="max-w-3xl mx-auto">
            <PendingRequestsPanel
              pendingRequests={pendingRequests}
              onRespond={handleRespond}
              isRespondPending={respondMut.isPending}
            />
          </div>
        </div>
      )}

      {/* Timeline area — full height */}
      <div ref={timelineScrollRef} className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto px-4 py-4 md:px-6 md:py-6 space-y-4">
          {/* Description card */}
          {task.description && (
            <div className="bg-slate-900 border border-slate-800 rounded-lg p-3 md:p-4">
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">Description</p>
              <p className="text-sm text-gray-300 whitespace-pre-wrap">{task.description}</p>
            </div>
          )}

          {/* Result Summary */}
          {resultSummary && (
            <div className="bg-slate-900 border border-slate-800 rounded-lg p-3 md:p-4">
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">Result Summary</p>
              <MarkdownDescription content={resultSummary} className="text-sm text-gray-300" />
            </div>
          )}

          {/* Metadata */}
          {metadataEntries.length > 0 && (
            <div className="bg-slate-900 border border-slate-800 rounded-lg divide-y divide-slate-800">
              {metadataEntries.map(([key, value]) => (
                <div key={key} className="flex flex-col sm:flex-row sm:items-center gap-1 sm:gap-3 px-3 py-2 md:px-4 md:py-2.5">
                  <span className="text-xs text-gray-500 font-mono sm:w-40 shrink-0">{key}</span>
                  {key === 'pull_request_url' || key === 'pr_url' ? (
                    <a
                      href={value}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm text-cyan-400 hover:text-cyan-300 flex items-center gap-1 truncate"
                    >
                      <ExternalLink className="w-3 h-3 shrink-0" />
                      <span className="truncate">{value}</span>
                    </a>
                  ) : key === 'worktree' ? (
                    <span className="text-sm text-gray-300 font-mono flex items-center gap-1">
                      <GitBranch className="w-3 h-3 text-gray-500 shrink-0" />
                      {value}
                    </span>
                  ) : (
                    <span className="text-sm text-gray-300 truncate">{value}</span>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Execution Timeline */}
          {timelineItems.length > 0 && (
            <div>
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">
                Execution Timeline ({timelineItems.length})
              </p>
              <div className="space-y-0.5">
                {timelineItems.map((item) => {
                  const key = item.kind === 'interaction' ? item.interaction.id : item.log.id
                  return <TimelineEntry key={key} item={item} />
                })}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* InputBar pinned to bottom */}
      <div className="shrink-0 border-t border-slate-800 px-4 py-3 md:px-6">
        <div className="max-w-3xl mx-auto">
          <InputBar
            pendingInteraction={pendingInteraction}
            onRespond={handleRespond}
            onSendMessage={handleSendMessage}
            isRespondPending={respondMut.isPending}
            isSendPending={sendMut.isPending}
          />
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

      {/* Edit modal */}
      {showEditModal && (
        <TaskDetailModal
          taskId={taskId}
          projectId={projectId}
          statuses={sortedStatuses}
          currentStatusId={task.statusId}
          onClose={() => setShowEditModal(false)}
          onChanged={() => refetchTask()}
        />
      )}
    </div>
  )
}
