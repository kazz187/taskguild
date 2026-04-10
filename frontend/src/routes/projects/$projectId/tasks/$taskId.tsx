import { useState, useCallback, useRef, useEffect, useMemo } from 'react'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getTask, listTasks, updateTaskStatus, stopTask, resumeTask } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { listInteractions, respondToInteraction, expireInteraction, sendMessage } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { useTaskLogs } from '@/hooks/useTaskLogs'
import { TaskLogCategory } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import { useNotificationSound } from '@/hooks/useNotificationSound'
import { TaskDetailModal } from '@/components/organisms/TaskDetailModal'
import { ChildTaskCreateModal } from '@/components/organisms/ChildTaskCreateModal'
import { ForceTransitionDialog } from '@/components/organisms/ForceTransitionDialog'
import { shortId } from '@/lib/id'
import { InputBar } from '@/components/organisms/InputBar'
import { MarkdownDescription } from '@/components/organisms/MarkdownDescription'
import { DescriptionHistory } from '@/components/organisms/DescriptionHistory'
import { TimelineEntry, type TimelineItem } from '@/components/organisms/TimelineEntry'
import { PendingRequestsPanel } from '@/components/organisms/PendingRequestsPanel'
import { ConnectionIndicator } from '@/components/organisms/ConnectionIndicator'
import {
  AlertTriangle,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUpRight,
  Bot,
  Clock,
  CopyPlus,
  ExternalLink,
  GitBranch,
  Layers,
  Loader,
  Pencil,
  Play,
  Square,
} from 'lucide-react'
import { Button } from '@/components/atoms/index.ts'

export const Route = createFileRoute('/projects/$projectId/tasks/$taskId')({
  component: TaskDetailPage,
})

function TaskDetailPage() {
  const { projectId, taskId } = Route.useParams()
  const navigate = useNavigate()
  const [showEditModal, setShowEditModal] = useState(false)
  const [showChildCreateModal, setShowChildCreateModal] = useState(false)
  const timelineScrollRef = useRef<HTMLDivElement>(null)
  const isAutoScrollEnabled = useRef(true)
  const [showScrollToBottom, setShowScrollToBottom] = useState(false)

  const { data: taskData, refetch: refetchTask } = useQuery(getTask, { id: taskId })
  const { data: projectData } = useQuery(getProject, { id: projectId })
  const { data: workflowsData } = useQuery(listWorkflows, { projectId })
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, { taskId, pagination: { limit: 0 } })

  const statusMut = useMutation(updateTaskStatus)
  const respondMut = useMutation(respondToInteraction)
  const expireMut = useMutation(expireInteraction)
  const sendMut = useMutation(sendMessage)
  const stopMut = useMutation(stopTask)
  const resumeMut = useMutation(resumeTask)

  const task = taskData?.task
  const project = projectData?.project
  const workflows = workflowsData?.workflows ?? []
  const interactions = interactionsData?.interactions ?? []

  useDocumentTitle(task?.title)

  const workflow = workflows.find((w) => w.id === task?.workflowId)
  const sortedStatuses = workflow ? [...workflow.statuses].sort((a, b) => a.order - b.order) : []
  const currentStatus = sortedStatuses.find((s) => s.id === task?.statusId)
  const allowedTransitions = currentStatus?.transitionsTo ?? []

  // Fetch sibling tasks for parent-child relationship display
  const { data: allTasksData, refetch: refetchAllTasks } = useQuery(
    listTasks,
    { projectId, workflowId: workflow?.id ?? '', pagination: { limit: 0 } },
    { enabled: !!workflow },
  )
  const allTasks = allTasksData?.tasks ?? []

  // Child tasks: tasks whose metadata.source_task_id matches this task
  const childTasks = useMemo(() => {
    return allTasks
      .filter((t) => t.metadata?.['source_task_id'] === taskId)
      .map((t) => ({ id: t.id, title: t.title, statusId: t.statusId }))
  }, [allTasks, taskId])

  // Parent task: if this task has a source_task_id, find the parent
  const parentTaskId = task?.metadata?.['source_task_id'] ?? ''
  const parentTask = useMemo(() => {
    if (!parentTaskId) return null
    const parent = allTasks.find((t) => t.id === parentTaskId)
    if (!parent) return null
    return { id: parent.id, title: parent.title }
  }, [allTasks, parentTaskId])

  // Force transition targets: all statuses except current and normal transitions
  const forceTransitions = useMemo(() => {
    if (!currentStatus) return []
    const normalSet = new Set(allowedTransitions)
    return sortedStatuses
      .filter((s) => s.id !== currentStatus.id && !normalSet.has(s.id))
      .map((s) => ({ id: s.id, name: s.name }))
  }, [currentStatus, allowedTransitions, sortedStatuses])

  // Force-move is only blocked when agent is actively running (assigned).
  // Pending tasks (agent not yet started) are allowed to be force-moved.
  const isForceMoveBlocked = task?.assignmentStatus === TaskAssignmentStatus.ASSIGNED

  // Check if current status has an agent configured (for resume button visibility).
  const currentStatusHasAgent = !!(currentStatus?.agentId)

  // Force-transition confirmation dialog state
  const [forceTransitionTarget, setForceTransitionTarget] = useState<{ id: string; name: string } | null>(null)

  // Fetch task logs
  const { logs } = useTaskLogs(taskId, projectId)

  // Description version history
  const [showDescHistory, setShowDescHistory] = useState(false)
  const descriptionLogs = useMemo(
    () =>
      logs
        .filter((l) => l.category === TaskLogCategory.RESULT && l.metadata['result_type'] === 'description')
        .sort((a, b) => {
          if (!a.createdAt || !b.createdAt) return 0
          const diff = Number(b.createdAt.seconds) - Number(a.createdAt.seconds)
          return diff !== 0 ? diff : b.createdAt.nanos - a.createdAt.nanos
        }),
    [logs],
  )

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
    refetchAllTasks()
  }, [refetchTask, refetchInteractions, refetchAllTasks])

  const eventTypes = useMemo(() => [
    EventType.TASK_CREATED, EventType.TASK_UPDATED, EventType.TASK_STATUS_CHANGED, EventType.AGENT_ASSIGNED,
    EventType.AGENT_STATUS_CHANGED, EventType.INTERACTION_CREATED, EventType.INTERACTION_RESPONDED,
    EventType.TASK_LOG,
  ], [])

  const { connectionStatus, reconnect } = useEventSubscription(eventTypes, projectId, onEvent)

  // Auto-scroll timeline to bottom on new entries (only when user is at the bottom)
  useEffect(() => {
    const el = timelineScrollRef.current
    if (!el || !isAutoScrollEnabled.current) return
    el.scrollTop = el.scrollHeight
  }, [timelineItems.length])

  const handleTimelineScroll = useCallback(() => {
    const el = timelineScrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30
    isAutoScrollEnabled.current = atBottom
    setShowScrollToBottom(!atBottom)
  }, [])

  const scrollTimelineToBottom = useCallback(() => {
    const el = timelineScrollRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
    isAutoScrollEnabled.current = true
    setShowScrollToBottom(false)
  }, [])

  // Play notification sound when new pending requests arrive
  useNotificationSound(pendingRequests.length)

  const handleDeleted = useCallback(() => {
    navigate({
      to: '/projects/$projectId',
      params: { projectId },
      search: task?.workflowId ? { workflowId: task.workflowId } : {},
    })
  }, [navigate, projectId, task?.workflowId])

  const handleStatusChange = (statusId: string, force = false) => {
    if (!task) return
    statusMut.mutate(
      { id: task.id, statusId, force },
      { onSuccess: () => refetchTask() },
    )
  }

  const handleForceTransitionClick = (target: { id: string; name: string }) => {
    setForceTransitionTarget(target)
  }

  const handleForceConfirm = () => {
    if (!forceTransitionTarget) return
    handleStatusChange(forceTransitionTarget.id, true)
    setForceTransitionTarget(null)
  }

  const handleForceCancel = () => {
    setForceTransitionTarget(null)
  }

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
          // Allow retry on failure
          respondedIdsRef.current.delete(interactionId)
        },
      },
    )
  }, [expireMut, refetchInteractions])

  const handleStopTask = () => {
    if (!task) return
    stopMut.mutate({ id: task.id }, { onSuccess: () => refetchTask() })
  }

  const handleResumeTask = () => {
    if (!task) return
    resumeMut.mutate({ id: task.id }, { onSuccess: () => refetchTask() })
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
  const metadataEntries = Object.entries(metadata).filter(([key, v]) => v && key !== 'result_summary' && key !== 'result_status' && key !== 'result_error' && key !== 'plan_result')

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

        <div>
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

            {task.assignmentStatus === TaskAssignmentStatus.ASSIGNED && (
              <button
                onClick={handleStopTask}
                disabled={stopMut.isPending}
                className="flex items-center gap-1 px-3 py-1 text-xs bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 hover:bg-red-500/20 hover:border-red-500/50 hover:text-red-300 transition-colors disabled:opacity-50"
              >
                <Square className="w-3 h-3" />
                Stop
              </button>
            )}
            {task.assignmentStatus === TaskAssignmentStatus.UNASSIGNED && currentStatusHasAgent && (
              <button
                onClick={handleResumeTask}
                disabled={resumeMut.isPending}
                className="flex items-center gap-1 px-3 py-1 text-xs bg-green-500/10 border border-green-500/30 rounded-lg text-green-400 hover:bg-green-500/20 hover:border-green-500/50 hover:text-green-300 transition-colors disabled:opacity-50"
              >
                <Play className="w-3 h-3" />
                Resume
              </button>
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
            {!isForceMoveBlocked && forceTransitions.map((target) => (
              <button
                key={target.id}
                onClick={() => handleForceTransitionClick(target)}
                disabled={statusMut.isPending}
                className="flex items-center gap-1 px-3 py-1 text-xs bg-slate-800 border border-slate-700 rounded-lg text-gray-400 hover:border-amber-500/50 hover:text-amber-300 transition-colors disabled:opacity-50"
                title="Force move (not defined in workflow)"
              >
                <AlertTriangle className="w-3 h-3 text-amber-500/70" />
                {target.name}
              </button>
            ))}

            <span className="text-[11px] text-gray-600 font-mono ml-auto hidden sm:inline">{task.id}</span>

            <button
              onClick={() => setShowChildCreateModal(true)}
              className="flex items-center gap-1 px-3 py-1 text-xs text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors shrink-0"
            >
              <CopyPlus className="w-3.5 h-3.5" />
              Subtask
            </button>
            <button
              onClick={() => setShowEditModal(true)}
              className="flex items-center gap-1 px-3 py-1 text-xs text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors shrink-0"
            >
              <Pencil className="w-3.5 h-3.5" />
              Edit
            </button>
          </div>
        </div>
      </div>

      {/* Timeline area — full height */}
      <div ref={timelineScrollRef} onScroll={handleTimelineScroll} className="relative flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto px-4 py-4 md:px-6 md:py-6 space-y-4">
          {/* Parent task link */}
          {parentTask && (
            <Link
              to="/projects/$projectId/tasks/$taskId"
              params={{ projectId, taskId: parentTask.id }}
              className="flex items-center gap-2 bg-slate-900 border border-slate-800 rounded-lg px-3 py-2 text-xs text-gray-400 hover:text-white hover:border-slate-700 transition-colors"
            >
              <ArrowUpRight className="w-3.5 h-3.5 text-gray-500 shrink-0" />
              <span className="text-gray-500">Parent:</span>
              <span className="font-medium text-gray-300 truncate">{parentTask.title}</span>
              <span className="text-gray-600 font-mono shrink-0">{shortId(parentTask.id)}</span>
            </Link>
          )}

          {/* Description card */}
          {task.description && (
            <div className="bg-slate-900 border border-slate-800 rounded-lg p-3 md:p-4">
              <div className="flex items-center justify-between mb-2">
                <p className="text-xs text-gray-500 uppercase tracking-wide">Description</p>
                {descriptionLogs.length > 0 && (
                  <button
                    onClick={() => setShowDescHistory(!showDescHistory)}
                    className="flex items-center gap-1 text-[10px] text-gray-500 hover:text-gray-300 transition-colors"
                  >
                    <Clock className="w-3 h-3" />
                    {descriptionLogs.length} version{descriptionLogs.length !== 1 ? 's' : ''}
                  </button>
                )}
              </div>
              {showDescHistory ? (
                <DescriptionHistory
                  versions={descriptionLogs}
                  currentDescription={task.description}
                  taskId={task.id}
                  onClose={() => setShowDescHistory(false)}
                />
              ) : (
                <MarkdownDescription content={task.description} taskId={task.id} />
              )}
            </div>
          )}

          {/* Results — all RESULT logs in chronological order (description, plan, summary, error) */}
          {(() => {
            const resultLogs = logs
              .filter((l) => l.category === TaskLogCategory.RESULT)
              .sort((a, b) => {
                if (!a.createdAt || !b.createdAt) return 0
                const diff = Number(a.createdAt.seconds) - Number(b.createdAt.seconds)
                return diff !== 0 ? diff : a.createdAt.nanos - b.createdAt.nanos
              })
            if (resultLogs.length === 0) return null
            return (
              <div className="bg-slate-900 border border-slate-800 rounded-lg p-3 md:p-4">
                <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">Results</p>
                <div className="space-y-2">
                  {resultLogs.map((log) => {
                    const resultType = log.metadata['result_type'] ?? 'summary'
                    const fullText = log.metadata['full_text'] ?? log.message
                    const borderColor =
                      resultType === 'error' ? 'border-red-500/30' :
                      resultType === 'plan' ? 'border-blue-500/30' :
                      resultType === 'description' ? 'border-cyan-500/30' :
                      'border-green-500/30'
                    const labelColor =
                      resultType === 'error' ? 'text-red-400' :
                      resultType === 'plan' ? 'text-blue-400' :
                      resultType === 'description' ? 'text-cyan-400' :
                      'text-green-400'
                    return (
                      <div key={log.id} className={`border-l-2 ${borderColor} pl-3 py-1`}>
                        <div className="flex items-center gap-2 mb-1">
                          <span className={`text-[10px] font-medium uppercase ${labelColor}`}>{resultType}</span>
                          {log.createdAt && (
                            <span className="text-[10px] text-gray-600">
                              {new Date(Number(log.createdAt.seconds) * 1000).toLocaleString()}
                            </span>
                          )}
                        </div>
                        <MarkdownDescription content={fullText} className="text-sm text-gray-300" />
                      </div>
                    )
                  })}
                </div>
              </div>
            )
          })()}

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

          {/* Child tasks */}
          {childTasks.length > 0 && (
            <div className="bg-slate-900 border border-slate-800 rounded-lg p-3 md:p-4">
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-2 flex items-center gap-1.5">
                <Layers className="w-3 h-3" />
                Subtasks ({childTasks.length})
              </p>
              <div className="space-y-1">
                {childTasks.map((child) => {
                  const childStatus = sortedStatuses.find((s) => s.id === child.statusId)
                  return (
                    <Link
                      key={child.id}
                      to="/projects/$projectId/tasks/$taskId"
                      params={{ projectId, taskId: child.id }}
                      className="flex items-center gap-2 w-full text-left px-2.5 py-1.5 text-xs bg-slate-800/50 border border-slate-700/50 rounded-lg hover:border-slate-600 hover:bg-slate-800 transition-colors"
                    >
                      <span className="text-white font-medium truncate flex-1">{child.title}</span>
                      {childStatus && (
                        <span className={`text-[10px] px-2 py-0.5 rounded-full font-medium ${
                          childStatus.isInitial ? 'bg-blue-500/20 text-blue-400' :
                          childStatus.isTerminal ? 'bg-green-500/20 text-green-400' :
                          'bg-gray-500/20 text-gray-300'
                        }`}>
                          {childStatus.name}
                        </span>
                      )}
                      <span className="text-[10px] text-gray-600 font-mono shrink-0">{shortId(child.id)}</span>
                    </Link>
                  )
                })}
              </div>
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
        {showScrollToBottom && (
          <Button
            variant="ghost"
            size="xs"
            icon={<ArrowDown className="w-3 h-3" />}
            onClick={scrollTimelineToBottom}
            className="sticky bottom-2 left-1/2 -translate-x-1/2 text-[10px] text-gray-300 bg-slate-800/80 backdrop-blur-sm border border-slate-600/50 hover:bg-slate-700/90 hover:text-white shadow-lg z-10"
          >
            Latest
          </Button>
        )}
      </div>

      {/* Pending requests section — pinned above input bar */}
      {pendingRequests.length > 0 && (
        <div className="shrink-0 border-t border-slate-800 bg-slate-800/50 px-4 md:px-6 py-3">
          <div className="max-w-3xl mx-auto">
            <PendingRequestsPanel
              pendingRequests={pendingRequests}
              onRespond={handleRespond}
              isRespondPending={respondMut.isPending}
              onDismiss={handleDismiss}
              isDismissPending={expireMut.isPending}
              hideTaskHeader
            />
          </div>
        </div>
      )}

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
          onDeleted={handleDeleted}
        />
      )}

      {/* Child task creation modal */}
      {showChildCreateModal && workflow && (
        <ChildTaskCreateModal
          parentTask={task}
          projectId={projectId}
          workflowId={workflow.id}
          onCreated={(newTaskId) => {
            refetchAllTasks()
            setShowChildCreateModal(false)
            if (newTaskId) {
              navigate({
                to: '/projects/$projectId/tasks/$taskId',
                params: { projectId, taskId: newTaskId },
              })
            }
          }}
          onClose={() => setShowChildCreateModal(false)}
        />
      )}

      {/* Force-transition confirmation dialog */}
      {forceTransitionTarget && currentStatus && (
        <ForceTransitionDialog
          fromStatusName={currentStatus.name}
          toStatusName={forceTransitionTarget.name}
          onConfirm={handleForceConfirm}
          onCancel={handleForceCancel}
        />
      )}
    </div>
  )
}
