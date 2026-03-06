import { useCallback, useRef, useEffect } from 'react'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation } from '@connectrpc/connect-query'
import { respondToInteraction } from '@taskguild/proto/taskguild/v1/interaction-InteractionService_connectquery.ts'
import { useGlobalTimeline } from '@/hooks/useGlobalTimeline'
import { useNotificationSound } from '@/hooks/useNotificationSound'
import { TimelineEntry } from '@/components/organisms/TimelineEntry'
import { PendingRequestsPanel } from '@/components/organisms/PendingRequestsPanel'
import { ConnectionIndicator } from '@/components/organisms/ConnectionIndicator'
import { shortId } from '@/lib/id'
import { ChevronUp, Loader } from 'lucide-react'

export const Route = createFileRoute('/global-chat')({
  component: GlobalChatPage,
})

function GlobalChatPage() {
  useDocumentTitle('Global Chat')
  const scrollRef = useRef<HTMLDivElement>(null)

  const {
    timelineItems,
    taskMap,
    projectMap,
    taskProjectMap,
    pendingRequests,
    connectionStatus,
    reconnect,
    isLoading,
    hasMore,
    loadMore,
    projectCount,
    taskCount,
  } = useGlobalTimeline()

  const respondMut = useMutation(respondToInteraction)

  // Play notification sound when new pending requests arrive
  useNotificationSound(pendingRequests.length)

  // Auto-scroll to bottom when new timeline items arrive
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [timelineItems.length])

  // Synchronous guard to prevent duplicate responses
  const respondedIdsRef = useRef<Set<string>>(new Set())

  const handleRespond = useCallback((interactionId: string, response: string) => {
    if (respondedIdsRef.current.has(interactionId)) return
    respondedIdsRef.current.add(interactionId)
    respondMut.mutate(
      { id: interactionId, response },
      {
        onError: () => {
          respondedIdsRef.current.delete(interactionId)
        },
      },
    )
  }, [respondMut])

  /** Get the taskId for a timeline item. */
  function getTaskId(item: (typeof timelineItems)[number]): string {
    return item.kind === 'interaction' ? item.interaction.taskId : item.log.taskId
  }

  return (
    <div className="flex flex-col h-dvh">
      {/* Header */}
      <div className="shrink-0 border-b border-slate-800 px-4 py-3 md:px-6 md:py-4">
        <h1 className="text-lg md:text-xl font-bold text-white">Global Chat</h1>
        <p className="text-xs text-gray-500 mt-1">
          All timelines across {projectCount} project{projectCount !== 1 ? 's' : ''},{' '}
          {taskCount} task{taskCount !== 1 ? 's' : ''}
        </p>
      </div>

      {/* Timeline area */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="max-w-4xl mx-auto px-4 py-4 md:px-6 md:py-6">
          {/* Load more button */}
          {hasMore && (
            <div className="flex justify-center pb-3">
              <button
                onClick={loadMore}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-400 hover:text-white bg-slate-800/50 hover:bg-slate-800 rounded-lg transition-colors"
              >
                <ChevronUp className="w-3.5 h-3.5" />
                Load older entries
              </button>
            </div>
          )}

          {isLoading && (
            <div className="flex items-center justify-center py-12">
              <Loader className="w-5 h-5 text-gray-500 animate-spin" />
            </div>
          )}

          {!isLoading && timelineItems.length === 0 && (
            <p className="text-gray-500 text-sm text-center py-12">No timeline entries yet.</p>
          )}

          {timelineItems.map((item, idx) => {
            const taskId = getTaskId(item)
            const prevTaskId = idx > 0 ? getTaskId(timelineItems[idx - 1]) : null
            const showTaskLabel = taskId !== prevTaskId
            const taskTitle = taskMap.get(taskId) || shortId(taskId)
            const projectName = projectMap.get(taskId)
            const projectId = taskProjectMap.get(taskId)

            const itemKey =
              item.kind === 'interaction'
                ? `i-${item.interaction.id}`
                : `l-${item.log.id}`

            return (
              <div key={itemKey}>
                {showTaskLabel && (
                  <div className="flex items-center gap-2 pt-3 pb-1">
                    <div className="flex items-center gap-1 min-w-0 shrink">
                      {projectName && (
                        <span className="text-[11px] text-gray-500 shrink-0">
                          {projectName} /
                        </span>
                      )}
                      {projectId ? (
                        <Link
                          to="/projects/$projectId/tasks/$taskId"
                          params={{ projectId, taskId }}
                          className="text-[11px] text-cyan-400 hover:text-cyan-300 font-medium truncate transition-colors"
                        >
                          {taskTitle}
                        </Link>
                      ) : (
                        <span className="text-[11px] text-cyan-400 font-medium truncate">
                          {taskTitle}
                        </span>
                      )}
                    </div>
                    <div className="flex-1 border-t border-slate-800" />
                  </div>
                )}
                <TimelineEntry item={item} />
              </div>
            )
          })}
        </div>
      </div>

      {/* Pending requests section */}
      {pendingRequests.length > 0 && (
        <div className="shrink-0 border-t border-slate-800 bg-slate-800/50 px-4 md:px-6 py-3">
          <div className="max-w-4xl mx-auto">
            <PendingRequestsPanel
              pendingRequests={pendingRequests}
              onRespond={handleRespond}
              isRespondPending={respondMut.isPending}
              taskMap={taskMap}
              projectMap={projectMap}
              taskProjectMap={taskProjectMap}
            />
          </div>
        </div>
      )}

      {/* Connection status bar */}
      <div className="shrink-0 border-t border-slate-800 px-6 py-2">
        <div className="max-w-4xl mx-auto flex items-center gap-2">
          <ConnectionIndicator status={connectionStatus} onReconnect={reconnect} />
          <span className="text-[11px] text-gray-500">
            {connectionStatus === 'connected' ? 'Connected' : connectionStatus === 'connecting' ? 'Connecting...' : 'Disconnected'}
          </span>
        </div>
      </div>
    </div>
  )
}
