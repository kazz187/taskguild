import { Wifi, WifiOff, Loader } from 'lucide-react'
import type { ConnectionStatus } from '@/hooks/useEventSubscription'

export function ConnectionIndicator({
  status,
  onReconnect,
}: {
  status: ConnectionStatus
  onReconnect: () => void
}) {
  if (status === 'connected') {
    return (
      <div
        className="flex items-center justify-center shrink-0 w-8 h-8 rounded-lg"
        title="Connected to server"
      >
        <Wifi className="w-4 h-4 text-green-400" />
      </div>
    )
  }

  if (status === 'connecting') {
    return (
      <div
        className="flex items-center justify-center shrink-0 w-8 h-8 rounded-lg"
        title="Connecting..."
      >
        <Loader className="w-4 h-4 text-yellow-400 animate-spin" />
      </div>
    )
  }

  // disconnected
  return (
    <button
      onClick={onReconnect}
      className="flex items-center justify-center shrink-0 w-8 h-8 rounded-lg hover:bg-slate-800 transition-colors"
      title="Disconnected. Click to reconnect."
    >
      <WifiOff className="w-4 h-4 text-red-400" />
    </button>
  )
}
