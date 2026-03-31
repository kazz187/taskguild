/**
 * Returns a human-readable description of why a task is pending,
 * based on the task's metadata fields.
 */
export function pendingReasonText(metadata: { [key: string]: string } | undefined): string {
  if (!metadata) return 'Waiting for agent'

  const reason = metadata['_pending_reason']
  switch (reason) {
    case 'worktree_occupied': {
      const title = metadata['_pending_blocker_task_title']
      return title
        ? `Worktree in use by: ${title}`
        : 'Worktree in use by another task'
    }
    case 'waiting_agent':
      return 'Waiting for agent to connect'
    case 'retry_backoff': {
      const retryAfter = metadata['_pending_retry_after']
      if (retryAfter) {
        const diff = new Date(retryAfter).getTime() - Date.now()
        if (diff <= 0) return 'Retrying now...'
        const secs = Math.floor(diff / 1000)
        const mins = Math.floor(secs / 60)
        return mins > 0
          ? `Retrying in ${mins}m ${secs % 60}s`
          : `Retrying in ${secs}s`
      }
      return 'Waiting for retry'
    }
    default:
      return 'Waiting for agent'
  }
}
