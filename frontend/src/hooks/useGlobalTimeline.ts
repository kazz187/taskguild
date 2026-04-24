import { useCallback, useMemo, useRef, useState } from 'react'
import { useQuery } from '@connectrpc/connect-query'
import { listProjects } from '@taskguild/proto/taskguild/v1/project-ProjectService_connectquery.ts'
import { listInteractions } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { listTaskLogs } from '@taskguild/proto/taskguild/v1/task_log-TaskLogService_connectquery.ts'
import { InteractionStatus, InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { EventType, type Event } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription } from './useEventSubscription'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { TimelineItem } from '@/components/organisms/TimelineEntry'

const INITIAL_LIMIT = 200

/**
 * Data hook powering the Global Chat view.
 *
 * Design notes:
 * - `listInteractions` is split into two queries: a tiny PENDING-only query
 *   that drives the pending-requests panel, and a paginated history query
 *   that drives the timeline. The pending query returns few rows even on
 *   busy projects, so `limit: 0` is cheap.
 * - Task titles and projectIds come from the list responses' side-channel
 *   maps; we no longer call `listTasks` separately.
 * - The returned `recentSelfMutatedIds` ref lets callers mark their own
 *   mutations so the subsequent server event does not trigger a redundant
 *   refetch (the optimistic update has already updated the cache).
 */
export function useGlobalTimeline() {
  const [historyLimit, setHistoryLimit] = useState(INITIAL_LIMIT)

  const { data: projectsData, refetch: refetchProjects } = useQuery(listProjects, {})
  const projects = useMemo(() => projectsData?.projects ?? [], [projectsData])

  // Pending interactions. Small result set; no pagination needed.
  const {
    data: pendingData,
    refetch: refetchPendingInteractions,
  } = useQuery(listInteractions, {
    projectId: '',
    taskId: '',
    statusFilter: InteractionStatus.PENDING,
    pagination: { limit: 0 },
  })
  const pendingInteractions = useMemo(() => pendingData?.interactions ?? [], [pendingData])

  // History. Paginated because responded/expired histories can be large.
  const {
    data: historyData,
    refetch: refetchHistoryInteractions,
  } = useQuery(listInteractions, {
    projectId: '',
    taskId: '',
    statusFilter: InteractionStatus.UNSPECIFIED,
    pagination: { limit: historyLimit },
  })
  const historyInteractions = useMemo(() => historyData?.interactions ?? [], [historyData])

  const { data: logsData, refetch: refetchLogs } = useQuery(listTaskLogs, {
    taskId: '',
    projectId: '',
    pagination: { limit: historyLimit },
  })
  const logs = useMemo(() => logsData?.logs ?? [], [logsData])

  // Task title map: taskId → title. Sources: pending query, history query, logs.
  const taskMap = useMemo(() => {
    const m = new Map<string, string>()
    const mergeTitles = (titles?: { [k: string]: string }) => {
      if (!titles) return
      for (const [id, title] of Object.entries(titles)) {
        if (!m.has(id)) m.set(id, title)
      }
    }
    mergeTitles(pendingData?.taskTitles)
    mergeTitles(historyData?.taskTitles)
    mergeTitles(logsData?.taskTitles)
    return m
  }, [pendingData?.taskTitles, historyData?.taskTitles, logsData?.taskTitles])

  // Task → project ID map (for linking).
  const taskProjectMap = useMemo(() => {
    const m = new Map<string, string>()
    const mergeIds = (ids?: { [k: string]: string }) => {
      if (!ids) return
      for (const [id, projectId] of Object.entries(ids)) {
        if (!m.has(id)) m.set(id, projectId)
      }
    }
    mergeIds(pendingData?.taskProjectIds)
    mergeIds(historyData?.taskProjectIds)
    mergeIds(logsData?.taskProjectIds)
    return m
  }, [pendingData?.taskProjectIds, historyData?.taskProjectIds, logsData?.taskProjectIds])

  // Project name map: taskId → projectName.
  const projectMap = useMemo(() => {
    const projectNameById = new Map<string, string>()
    for (const p of projects) projectNameById.set(p.id, p.name)
    const m = new Map<string, string>()
    for (const [taskId, projectId] of taskProjectMap) {
      const pName = projectNameById.get(projectId)
      if (pName) m.set(taskId, pName)
    }
    return m
  }, [projects, taskProjectMap])

  // Timeline items: merge history interactions + logs sorted by createdAt.
  const timelineItems = useMemo<TimelineItem[]>(() => {
    const items: TimelineItem[] = [
      ...historyInteractions.map((interaction): TimelineItem => ({ kind: 'interaction', interaction })),
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
  }, [historyInteractions, logs])

  // Pending requests drive the action panel.
  const pendingRequests = useMemo<Interaction[]>(
    () =>
      pendingInteractions.filter(
        (i) =>
          (i.type === InteractionType.PERMISSION_REQUEST ||
            i.type === InteractionType.QUESTION) &&
          i.status === InteractionStatus.PENDING,
      ),
    [pendingInteractions],
  )

  // IDs the local tab has just mutated. An inbound event for one of these is
  // suppressed because the optimistic cache update already reflects the
  // server-side change.
  const recentSelfMutatedIds = useRef<Set<string>>(new Set())

  const onEvent = useCallback(
    (event: Event) => {
      switch (event.type) {
        case EventType.INTERACTION_CREATED:
        case EventType.INTERACTION_RESPONDED:
          if (recentSelfMutatedIds.current.has(event.resourceId)) {
            recentSelfMutatedIds.current.delete(event.resourceId)
            return
          }
          void refetchPendingInteractions()
          void refetchHistoryInteractions()
          return
        case EventType.TASK_LOG:
          void refetchLogs()
          return
        case EventType.TASK_CREATED:
        case EventType.TASK_UPDATED:
        case EventType.TASK_STATUS_CHANGED:
        case EventType.TASK_ARCHIVED:
        case EventType.TASK_UNARCHIVED:
          // Task list may affect project/task titles in subsequent list
          // responses, but since we rely on side-channel title maps from
          // the interaction/log queries, refetching those is enough.
          void refetchPendingInteractions()
          void refetchHistoryInteractions()
          return
        default:
          return
      }
    },
    [refetchPendingInteractions, refetchHistoryInteractions, refetchLogs],
  )

  const eventTypes = useMemo(
    () => [
      EventType.TASK_CREATED,
      EventType.TASK_UPDATED,
      EventType.TASK_STATUS_CHANGED,
      EventType.TASK_ARCHIVED,
      EventType.TASK_UNARCHIVED,
      EventType.INTERACTION_CREATED,
      EventType.INTERACTION_RESPONDED,
      EventType.TASK_LOG,
    ],
    [],
  )

  const { connectionStatus, reconnect } = useEventSubscription(eventTypes, '', onEvent)

  const logTotal = logsData?.pagination?.total ?? 0
  const hasMore = logTotal > historyLimit

  const loadMore = useCallback(() => {
    setHistoryLimit((prev) => prev + INITIAL_LIMIT)
  }, [])

  // Derived counts; projects/taskMap sizes are a proxy for "how many active
  // projects / tasks currently show up in Global Chat".
  const taskCount = taskMap.size

  return {
    timelineItems,
    taskMap,
    projectMap,
    taskProjectMap,
    pendingRequests,
    connectionStatus,
    reconnect,
    isLoading: !projectsData || !pendingData || !historyData || !logsData,
    hasMore,
    loadMore,
    projectCount: projects.length,
    taskCount,
    recentSelfMutatedIds,
    // Exposed so mutations can manually force a refetch if desired (currently
    // unused by callers, which rely on the event stream).
    refetchProjects,
    refetchPendingInteractions,
    refetchHistoryInteractions,
    refetchLogs,
  }
}
