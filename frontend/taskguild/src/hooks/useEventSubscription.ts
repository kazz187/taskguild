import { useEffect } from 'react'
import { createClient } from '@connectrpc/connect'
import { EventService, type EventType, SubscribeEventsRequestSchema } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { create } from '@bufbuild/protobuf'
import { transport } from '@/lib/transport'

export function useEventSubscription(
  eventTypes: EventType[],
  projectId: string,
  onEvent: () => void,
) {
  useEffect(() => {
    if (!projectId) return

    const client = createClient(EventService, transport)
    const controller = new AbortController()

    async function subscribe() {
      try {
        const req = create(SubscribeEventsRequestSchema, {
          eventTypes,
          projectId,
        })
        for await (const _event of client.subscribeEvents(req, {
          signal: controller.signal,
        })) {
          onEvent()
        }
      } catch (e) {
        if (controller.signal.aborted) return
        console.error('Event subscription error:', e)
      }
    }

    subscribe()

    return () => {
      controller.abort()
    }
  }, [projectId, eventTypes, onEvent])
}
