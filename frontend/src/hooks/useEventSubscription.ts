import { useEffect, useMemo, useRef, useState, useCallback } from 'react'
import type { Event, EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import {
  eventStream,
  type ConnectionStatus,
  type EventStreamListener,
} from '@/lib/event-stream'

export type { ConnectionStatus }

/**
 * Subscribe to realtime events filtered by type and project ID.
 *
 * The callback receives the full Event object so handlers can inspect
 * `event.type`, `event.resourceId`, and `event.metadata` to only refetch the
 * queries that are actually invalidated.
 *
 * All hook instances across the app share a single underlying gRPC-Web stream
 * (see `EventStreamManager` in `@/lib/event-stream`), so mounting this hook
 * in many components is essentially free.
 */
export function useEventSubscription(
  eventTypes: EventType[],
  projectId: string,
  onEvent: (event: Event) => void,
): { connectionStatus: ConnectionStatus; reconnect: () => void } {
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('connecting')

  // Avoid re-subscribing when onEvent identity changes; callbacks look up the
  // latest version through a ref.
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  // Derive a stable key from the event type list so sorting/ordering changes
  // don't cause a re-subscribe churn.
  const eventTypesKey = useMemo(
    () => eventTypes.slice().sort().join(','),
    [eventTypes],
  )

  useEffect(() => {
    const listener: EventStreamListener = (ev) => {
      onEventRef.current(ev)
    }
    return eventStream.subscribe(eventTypes, projectId, listener)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- eventTypesKey encodes eventTypes for stability
  }, [eventTypesKey, projectId])

  useEffect(() => eventStream.onStatus(setConnectionStatus), [])

  const reconnect = useCallback(() => {
    eventStream.reconnect()
  }, [])

  return { connectionStatus, reconnect }
}
