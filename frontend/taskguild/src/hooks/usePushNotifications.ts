import { useState, useEffect, useCallback } from 'react'
import { createClient } from '@connectrpc/connect'
import { createAppTransport } from '@/lib/transport'
import { getEffectiveConfig } from '@/lib/config'
import { PushNotificationService } from '@taskguild/proto/taskguild/v1/push_notification_pb.ts'

type PushStatus = 'unsupported' | 'default' | 'denied' | 'granted' | 'subscribed'

export function usePushNotifications() {
  const [status, setStatus] = useState<PushStatus>('unsupported')

  useEffect(() => {
    if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
      setStatus('unsupported')
      return
    }

    if (Notification.permission === 'denied') {
      setStatus('denied')
      return
    }

    navigator.serviceWorker.ready.then((reg) => {
      reg.pushManager.getSubscription().then((sub) => {
        setStatus(sub ? 'subscribed' : Notification.permission === 'granted' ? 'granted' : 'default')
      })
    })
  }, [])

  const subscribe = useCallback(async () => {
    try {
      const config = getEffectiveConfig()
      const transport = createAppTransport(config)
      const client = createClient(PushNotificationService, transport)

      const { publicKey } = await client.getVapidPublicKey({})
      if (!publicKey) {
        console.error('VAPID public key not available')
        return
      }

      const reg = await navigator.serviceWorker.ready
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(publicKey).buffer as ArrayBuffer,
      })

      const json = sub.toJSON()
      await client.registerPushSubscription({
        endpoint: sub.endpoint,
        p256dhKey: json.keys?.p256dh ?? '',
        authKey: json.keys?.auth ?? '',
      })

      setStatus('subscribed')
    } catch (err) {
      console.error('Failed to subscribe to push notifications', err)
      if (Notification.permission === 'denied') {
        setStatus('denied')
      }
    }
  }, [])

  const unsubscribe = useCallback(async () => {
    try {
      const reg = await navigator.serviceWorker.ready
      const sub = await reg.pushManager.getSubscription()
      if (sub) {
        const config = getEffectiveConfig()
        const transport = createAppTransport(config)
        const client = createClient(PushNotificationService, transport)

        await client.unregisterPushSubscription({ endpoint: sub.endpoint })
        await sub.unsubscribe()
      }
      setStatus('default')
    } catch (err) {
      console.error('Failed to unsubscribe from push notifications', err)
    }
  }, [])

  return { status, subscribe, unsubscribe }
}

function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = window.atob(base64)
  const outputArray = new Uint8Array(rawData.length)
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i)
  }
  return outputArray
}
