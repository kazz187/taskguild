import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@connectrpc/connect-query'
import { listProjects } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listTasks } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { listInteractions } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { listTaskLogs } from '@taskguild/proto/taskguild/v1/task_log-TaskLogService_connectquery.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription, type ConnectionStatus } from './useEventSubscription'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { TimelineItem } from '@/components/organisms/TimelineEntry'

const INITIAL_LIMIT = 200

export function useGlobalTimeline() {
  const [limit, setLimit] = useState(INITIAL_LIMIT)

  // Fetch all projects
  const { data: projectsData, refetch: refetchProjects } = useQuery(listProjects, {})
  const projects = projectsData?.projects ?? []

  // Fetch all tasks across all projects (empty project_id = all)
  const { data: tasksData, refetch: refetchTasks } = useQuery(listTasks, {
    projectId: '',
    pagination: { limit: 0 },
  })
  const tasks = tasksData?.tasks ?? []

  // Fetch all interactions across all projects (empty project_id and task_id = all)
  const { data: interactionsData, refetch: refetchInteractions } = useQuery(listInteractions, {
    projectId: '',
    taskId: '',
    pagination: { limit },
  })
  const interactions = interactionsData?.interactions ?? []

  // Fetch all task logs across all projects (empty task_id and project_id = all)
  const { data: logsData, refetch: refetchLogs } = useQuery(listTaskLogs, {
    taskId: '',
    projectId: '',
    pagination: { limit },
  })
  const logs = logsData?.logs ?? []

  // Build task title map: taskId → taskTitle
  // Merges titles from listTasks (active) and from interaction/log responses
  // (which include archived tasks).
  const taskMap = useMemo(() => {
    const m = new Map<string, string>()
    // From listTasks (active tasks – preferred source)
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

  // Build project name map: taskId → projectName
  const projectMap = useMemo(() => {
    const projectNameById = new Map<string, string>()
    for (const p of projects) {
      projectNameById.set(p.id, p.name)
    }
    const m = new Map<string, string>()
    for (const t of tasks) {
      const pName = projectNameById.get(t.projectId)
      if (pName) {
        m.set(t.id, pName)
      }
    }
    return m
  }, [projects, tasks])

  // Build taskId → projectId map (for linking)
  const taskProjectMap = useMemo(() => {
    const m = new Map<string, string>()
    for (const t of tasks) {
      m.set(t.id, t.projectId)
    }
    return m
  }, [tasks])

  // Merge interactions + logs into unified timeline sorted by createdAt
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
  const pendingRequests = useMemo<Interaction[]>(
    () =>
      interactions.filter(
        (i) =>
          (i.type === InteractionType.PERMISSION_REQUEST ||
            i.type === InteractionType.QUESTION) &&
          i.status === InteractionStatus.PENDING,
      ),
    [interactions],
  )

  // Event subscription for real-time updates (empty projectId = all projects)
  const onEvent = useCallback(() => {
    refetchProjects()
    refetchTasks()
    refetchInteractions()
    refetchLogs()
  }, [refetchProjects, refetchTasks, refetchInteractions, refetchLogs])

  const eventTypes = useMemo(() => [
    EventType.TASK_CREATED,
    EventType.TASK_UPDATED,
    EventType.INTERACTION_CREATED,
    EventType.INTERACTION_RESPONDED,
    EventType.TASK_LOG,
  ], [])

  const { connectionStatus, reconnect } = useEventSubscription(eventTypes, '', onEvent)

  // Pagination
  const interactionTotal = interactionsData?.pagination?.total ?? 0
  const logTotal = logsData?.pagination?.total ?? 0
  const hasMore = interactionTotal > limit || logTotal > limit

  const loadMore = useCallback(() => {
    setLimit((prev) => prev + INITIAL_LIMIT)
  }, [])

  return {
    timelineItems,
    taskMap,
    projectMap,
    taskProjectMap,
    pendingRequests,
    connectionStatus,
    reconnect,
    isLoading: !projectsData || !tasksData || !interactionsData || !logsData,
    hasMore,
    loadMore,
    projectCount: projects.length,
    taskCount: tasks.length,
  }
}
