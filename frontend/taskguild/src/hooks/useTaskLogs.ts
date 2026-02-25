import { useCallback, useMemo } from 'react'
import { useQuery } from '@connectrpc/connect-query'
import { listTaskLogs } from '@taskguild/proto/taskguild/v1/task_log-TaskLogService_connectquery.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { useEventSubscription, type ConnectionStatus } from './useEventSubscription'
import type { TaskLog } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'

export function useTaskLogs(
  taskId: string,
  projectId: string,
): {
  logs: TaskLog[]
  connectionStatus: ConnectionStatus
  reconnect: () => void
} {
  const { data, refetch } = useQuery(listTaskLogs, { taskId, pagination: { limit: 0 } })

  const onEvent = useCallback(() => {
    refetch()
  }, [refetch])

  const eventTypes = useMemo(() => [EventType.TASK_LOG], [])

  const { connectionStatus, reconnect } = useEventSubscription(eventTypes, projectId, onEvent)

  const logs = data?.logs ?? []

  return { logs, connectionStatus, reconnect }
}
