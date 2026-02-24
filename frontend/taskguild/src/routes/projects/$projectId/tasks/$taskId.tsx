import { useState, useCallback, useRef, useEffect, useMemo } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getTask, updateTaskStatus } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { getProject } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listWorkflows } from '@taskguild/proto/taskguild/v1/workflow-WorkflowService_connectquery.ts'
import { listInteractions, respondToInteraction, sendMessage } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { TaskAssignmentStatus } from '@taskguild/proto/taskguild/v1/task_pb.ts'
import { InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { TaskDetailModal } from '@/components/TaskDetailModal'
import { ChatBubble, InputBar } from '@/components/ChatBubble'
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
  const scrollRef = useRef<HTMLDivElement>(null)

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

  const pendingInteraction = interactions.find((i) => i.status === InteractionStatus.PENDING)

  const onEvent = useCallback(() => {
    refetchTask()
    refetchInteractions()
  }, [refetchTask, refetchInteractions])

  const eventTypes = useMemo(() => [
    EventType.TASK_UPDATED, EventType.TASK_STATUS_CHANGED, EventType.AGENT_ASSIGNED,
    EventType.AGENT_STATUS_CHANGED, EventType.INTERACTION_CREATED, EventType.INTERACTION_RESPONDED,
  ], [])

  useEventSubscription(eventTypes, projectId, onEvent)

  // Auto-scroll to bottom when new interactions arrive
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [interactions.length])

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
      <div className="flex items-center justify-center h-screen text-gray-400">
        Loading...
      </div>
    )
  }

  const metadata = task.metadata ?? {}
  const metadataEntries = Object.entries(metadata).filter(([, v]) => v)

  return (
    <div className="flex flex-col h-screen">
      {/* Header */}
      <div className="shrink-0 border-b border-slate-800 px-6 py-4">
        <div className="flex items-center gap-3 text-sm text-gray-400 mb-3">
          <Link
            to="/projects/$projectId"
            params={{ projectId }}
            className="hover:text-white transition-colors flex items-center gap-1"
          >
            <ArrowLeft className="w-4 h-4" />
            {project?.name ?? 'Project'}
          </Link>
          <span className="text-gray-600">/</span>
          <span className="text-gray-300 truncate">{task.title}</span>
        </div>

        <div className="flex items-start justify-between gap-4">
          <div className="flex-1 min-w-0">
            <h1 className="text-xl font-bold text-white">{task.title}</h1>
            <div className="flex items-center gap-2 mt-2 flex-wrap">
              {/* Status badge */}
              <span className={`text-xs px-2.5 py-1 rounded-full font-medium ${
                currentStatus?.isInitial ? 'bg-blue-500/20 text-blue-400' :
                currentStatus?.isTerminal ? 'bg-green-500/20 text-green-400' :
                'bg-gray-500/20 text-gray-300'
              }`}>
                {currentStatus?.name ?? task.statusId}
              </span>

              {/* Agent badge */}
              {task.assignmentStatus === TaskAssignmentStatus.ASSIGNED ? (
                <span className="inline-flex items-center gap-1 text-xs bg-cyan-500/10 text-cyan-400 border border-cyan-500/20 rounded-full px-2.5 py-1">
                  <Bot className="w-3 h-3" />
                  {task.assignedAgentId.slice(0, 12)}
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

              {/* Status transitions */}
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

              <span className="text-[11px] text-gray-600 font-mono ml-auto">{task.id}</span>
            </div>
          </div>

          <button
            onClick={() => setShowEditModal(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors shrink-0"
          >
            <Pencil className="w-4 h-4" />
            Edit
          </button>
        </div>
      </div>

      {/* Chat area */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto px-6 py-6 space-y-4">
          {/* Description card */}
          {task.description && (
            <div className="bg-slate-900 border border-slate-800 rounded-lg p-4">
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">Description</p>
              <p className="text-sm text-gray-300 whitespace-pre-wrap">{task.description}</p>
            </div>
          )}

          {/* Metadata */}
          {metadataEntries.length > 0 && (
            <div className="bg-slate-900 border border-slate-800 rounded-lg divide-y divide-slate-800">
              {metadataEntries.map(([key, value]) => (
                <div key={key} className="flex items-center gap-3 px-4 py-2.5">
                  <span className="text-xs text-gray-500 font-mono w-40 shrink-0">{key}</span>
                  {key === 'pull_request_url' || key === 'pr_url' ? (
                    <a
                      href={value}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm text-cyan-400 hover:text-cyan-300 flex items-center gap-1 truncate"
                    >
                      <ExternalLink className="w-3 h-3 shrink-0" />
                      {value}
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

          {/* Interaction timeline */}
          {interactions.length > 0 && (
            <div className="space-y-3 pt-2">
              <p className="text-xs text-gray-500 uppercase tracking-wide">
                Conversation ({interactions.length})
              </p>
              {interactions.map((interaction) => (
                <ChatBubble
                  key={interaction.id}
                  interaction={interaction}
                  onRespond={handleRespond}
                  isRespondPending={respondMut.isPending}
                />
              ))}
            </div>
          )}

          {/* Input bar (inline, below conversation) */}
          <InputBar
            pendingInteraction={pendingInteraction}
            onRespond={handleRespond}
            onSendMessage={handleSendMessage}
            isRespondPending={respondMut.isPending}
            isSendPending={sendMut.isPending}
          />
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

