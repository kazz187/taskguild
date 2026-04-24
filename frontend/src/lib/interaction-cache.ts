import type { QueryClient } from '@tanstack/react-query'
import { createConnectQueryKey } from '@connectrpc/connect-query'
import { listInteractions } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import {
  InteractionStatus,
  type Interaction,
  type ListInteractionsResponse,
} from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { create } from '@bufbuild/protobuf'
import { TimestampSchema } from '@bufbuild/protobuf/wkt'

/**
 * Tiny helpers that manipulate the TanStack Query cache for `listInteractions`
 * in-place, so Approve / Reject buttons feel instant without waiting for a
 * background refetch.
 */

type CacheSnapshot = Array<[unknown, ListInteractionsResponse | undefined]>

// A broad key that matches every finite listInteractions query regardless of
// input (projectId / taskId / statusFilter / pagination).
const listInteractionsFiniteKey = createConnectQueryKey({
  schema: listInteractions,
  cardinality: 'finite',
})

function nowTimestamp() {
  const now = Date.now()
  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(now / 1000)),
    nanos: (now % 1000) * 1_000_000,
  })
}

/**
 * Apply an in-place update to every cached `listInteractions` entry that
 * contains the given interaction id. Returns a snapshot suitable for
 * `revertInteractionCache` on mutation failure.
 */
export function optimisticallyUpdateInteraction(
  queryClient: QueryClient,
  id: string,
  patch: Partial<Interaction>,
): CacheSnapshot {
  const snapshots: CacheSnapshot = queryClient.getQueriesData<ListInteractionsResponse>({
    queryKey: listInteractionsFiniteKey,
  })

  queryClient.setQueriesData<ListInteractionsResponse>(
    { queryKey: listInteractionsFiniteKey },
    (old) => {
      if (!old) return old
      let changed = false
      const nextInteractions = old.interactions.map((i) => {
        if (i.id !== id) return i
        changed = true
        return { ...i, ...patch } as Interaction
      })
      if (!changed) return old
      return { ...old, interactions: nextInteractions }
    },
  )

  return snapshots
}

/** Roll back every cache entry to the snapshot captured by the optimistic update. */
export function revertInteractionCache(queryClient: QueryClient, snapshots: CacheSnapshot) {
  for (const [queryKey, data] of snapshots) {
    queryClient.setQueryData<ListInteractionsResponse>(
      queryKey as readonly unknown[],
      data,
    )
  }
}

/** Apply the responded transition to the cache. */
export function optimisticallyRespond(
  queryClient: QueryClient,
  id: string,
  response: string,
): CacheSnapshot {
  return optimisticallyUpdateInteraction(queryClient, id, {
    status: InteractionStatus.RESPONDED,
    response,
    respondedAt: nowTimestamp(),
  })
}

/** Apply the expired transition to the cache. */
export function optimisticallyExpire(queryClient: QueryClient, id: string): CacheSnapshot {
  return optimisticallyUpdateInteraction(queryClient, id, {
    status: InteractionStatus.EXPIRED,
    respondedAt: nowTimestamp(),
  })
}
