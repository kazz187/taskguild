import { createClient } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import {
  EventService,
  EventType,
  SubscribeEventsRequestSchema,
  type Event,
} from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { transport } from '@/lib/transport'

export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected'

export type EventStreamListener = (ev: Event) => void
export type StatusListener = (status: ConnectionStatus) => void

interface Subscription {
  types: Set<EventType> | null // null = all types
  projectId: string // '' = all projects
  cb: EventStreamListener
}

/**
 * Singleton manager for the `/EventService/SubscribeEvents` server stream.
 *
 * Rationale: previously, every component that needed realtime updates opened
 * its own gRPC-Web stream via `useEventSubscription`. A single page can mount
 * 5–7 such hooks, so server restarts or transient network failures cause a
 * burst of "Event subscription error" logs and force the server to maintain
 * one event-bus channel per hook.
 *
 * The manager multiplexes every in-tab subscription over a single long-lived
 * server stream. Filtering by event type and project ID happens client-side.
 * Reconnect errors are logged once per incident instead of once per hook.
 */
class EventStreamManager {
  private client = createClient(EventService, transport)
  private subs = new Map<number, Subscription>()
  private statusSubs = new Set<StatusListener>()
  private nextId = 1

  private controller: AbortController | null = null
  private status: ConnectionStatus = 'connecting'
  private backoffAttempts = 0
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private running = false
  private loggedErrorForThisIncident = false

  subscribe(
    eventTypes: EventType[],
    projectId: string,
    cb: EventStreamListener,
  ): () => void {
    const id = this.nextId++
    const types = eventTypes.length > 0 ? new Set(eventTypes) : null
    this.subs.set(id, { types, projectId, cb })
    this.ensureConnected()
    return () => {
      this.subs.delete(id)
      if (this.subs.size === 0) {
        this.disconnect()
      }
    }
  }

  onStatus(cb: StatusListener): () => void {
    this.statusSubs.add(cb)
    // Fire the current status immediately so the caller is in sync.
    cb(this.status)
    return () => {
      this.statusSubs.delete(cb)
    }
  }

  /** Force an immediate reconnect (clears backoff). */
  reconnect(): void {
    this.backoffAttempts = 0
    this.loggedErrorForThisIncident = false
    if (this.retryTimer) {
      clearTimeout(this.retryTimer)
      this.retryTimer = null
    }
    this.disconnect()
    this.ensureConnected()
  }

  private setStatus(next: ConnectionStatus): void {
    if (this.status === next) return
    this.status = next
    for (const cb of this.statusSubs) cb(next)
  }

  private ensureConnected(): void {
    if (this.running || this.subs.size === 0) return
    this.running = true
    this.setStatus('connecting')
    void this.run()
  }

  private disconnect(): void {
    if (this.controller) {
      this.controller.abort()
      this.controller = null
    }
    this.running = false
    if (this.retryTimer) {
      clearTimeout(this.retryTimer)
      this.retryTimer = null
    }
    this.setStatus('disconnected')
  }

  private scheduleReconnect(): void {
    if (this.subs.size === 0) return
    const attempt = this.backoffAttempts
    // 2s, 4s, 8s, 16s, capped at 30s.
    const delay = Math.min(2000 * Math.pow(2, attempt), 30000)
    this.backoffAttempts = attempt + 1
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null
      this.ensureConnected()
    }, delay)
  }

  private async run(): Promise<void> {
    // Open a single broad subscription. Filtering happens client-side so new
    // hooks that care about a different event subset do not cost a new stream.
    const controller = new AbortController()
    this.controller = controller
    try {
      const req = create(SubscribeEventsRequestSchema, {
        eventTypes: [],
        projectId: '',
      })
      for await (const event of this.client.subscribeEvents(req, {
        signal: controller.signal,
      })) {
        this.setStatus('connected')
        this.backoffAttempts = 0
        this.loggedErrorForThisIncident = false
        // Skip the initial UNSPECIFIED handshake.
        if (event.type !== EventType.UNSPECIFIED) {
          this.dispatch(event)
        }
      }
      // Stream closed normally by the server. Reconnect if we still have
      // listeners.
      if (!controller.signal.aborted) {
        this.running = false
        this.setStatus('disconnected')
        this.scheduleReconnect()
      }
    } catch (e) {
      if (controller.signal.aborted) return
      if (!this.loggedErrorForThisIncident) {
        this.loggedErrorForThisIncident = true
        console.warn('Event subscription error (will retry):', e)
      }
      this.running = false
      this.setStatus('disconnected')
      this.scheduleReconnect()
    }
  }

  private dispatch(event: Event): void {
    for (const sub of this.subs.values()) {
      if (sub.types && !sub.types.has(event.type)) continue
      if (sub.projectId !== '') {
        const evProjectId = event.metadata['project_id']
        if (evProjectId && evProjectId !== sub.projectId) continue
      }
      try {
        sub.cb(event)
      } catch (err) {
        // Never let one bad listener kill the stream.
        console.error('event listener threw:', err)
      }
    }
  }
}

export const eventStream = new EventStreamManager()
