import { useEffect, useState, useCallback, useRef, useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { EventService, EventType, SubscribeEventsRequestSchema } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { create } from '@bufbuild/protobuf'
import { transport } from '@/lib/transport'

export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected'

export function useEventSubscription(
  eventTypes: EventType[],
  projectId: string,
  onEvent: () => void,
): { connectionStatus: ConnectionStatus; reconnect: () => void } {
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('connecting')
  const [reconnectTrigger, setReconnectTrigger] = useState(0)
  const autoReconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const autoReconnectCountRef = useRef(0)

  // Use refs to avoid re-subscribing when onEvent or eventTypes reference changes
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const eventTypesRef = useRef(eventTypes)
  eventTypesRef.current = eventTypes

  // Stable key derived from eventTypes values for useEffect dependency
  const eventTypesKey = useMemo(() => eventTypes.slice().sort().join(','), [eventTypes])

  useEffect(() => {
    if (!projectId) return

    const client = createClient(EventService, transport)
    const controller = new AbortController()

    // Clear any pending auto-reconnect timer
    if (autoReconnectTimerRef.current) {
      clearTimeout(autoReconnectTimerRef.current)
      autoReconnectTimerRef.current = null
    }

    setConnectionStatus('connecting')

    async function subscribe() {
      try {
        const req = create(SubscribeEventsRequestSchema, {
          eventTypes: eventTypesRef.current,
          projectId,
        })
        for await (const event of client.subscribeEvents(req, {
          signal: controller.signal,
        })) {
          setConnectionStatus('connected')
          autoReconnectCountRef.current = 0 // Reset backoff on successful event
          // Skip onEvent callback for initial connection signal (UNSPECIFIED type)
          if (event.type !== EventType.UNSPECIFIED) {
            onEventRef.current()
          }
        }
        // Stream ended normally (server closed the stream)
        if (!controller.signal.aborted) {
          setConnectionStatus('disconnected')
          scheduleAutoReconnect()
        }
      } catch (e) {
        if (controller.signal.aborted) return
        console.error('Event subscription error:', e)
        setConnectionStatus('disconnected')
        scheduleAutoReconnect()
      }
    }

    function scheduleAutoReconnect() {
      const count = autoReconnectCountRef.current
      // Exponential backoff: 2s, 4s, 8s, 16s, max 30s
      const delay = Math.min(2000 * Math.pow(2, count), 30000)
      autoReconnectCountRef.current = count + 1
      autoReconnectTimerRef.current = setTimeout(() => {
        setReconnectTrigger((prev) => prev + 1)
      }, delay)
    }

    subscribe()

    return () => {
      controller.abort()
      if (autoReconnectTimerRef.current) {
        clearTimeout(autoReconnectTimerRef.current)
        autoReconnectTimerRef.current = null
      }
    }
  }, [projectId, eventTypesKey, reconnectTrigger])

  const reconnect = useCallback(() => {
    autoReconnectCountRef.current = 0 // Reset backoff on manual reconnect
    setReconnectTrigger((prev) => prev + 1)
  }, [])

  return { connectionStatus, reconnect }
}
