import { Bell, BellOff, BellRing } from 'lucide-react'
import { usePushNotifications } from '@/hooks/usePushNotifications'

export function PushNotificationToggle() {
  const { status, subscribe, unsubscribe } = usePushNotifications()

  if (status === 'unsupported') return null

  if (status === 'ios-browser') {
    return (
      <div className="w-full flex items-center gap-2 px-3 py-2 text-xs text-gray-500 rounded-lg">
        <Bell className="w-3.5 h-3.5 shrink-0" />
        <span>ホーム画面に追加して通知を有効化</span>
      </div>
    )
  }

  const isSubscribed = status === 'subscribed'
  const isDenied = status === 'denied'

  return (
    <button
      onClick={isSubscribed ? unsubscribe : subscribe}
      disabled={isDenied}
      className="w-full flex items-center gap-2 px-3 py-2 text-xs text-gray-500 hover:text-gray-300 hover:bg-slate-800/40 rounded-lg transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
      title={
        isDenied
          ? 'Notifications blocked by browser'
          : isSubscribed
            ? 'Disable notifications'
            : 'Enable notifications'
      }
    >
      {isDenied ? (
        <BellOff className="w-3.5 h-3.5" />
      ) : isSubscribed ? (
        <BellRing className="w-3.5 h-3.5 text-cyan-400" />
      ) : (
        <Bell className="w-3.5 h-3.5" />
      )}
      {isDenied ? 'Notifications blocked' : isSubscribed ? 'Notifications on' : 'Enable notifications'}
    </button>
  )
}
