import { useState, useEffect, useCallback } from 'react'
import { createClient } from '@connectrpc/connect'
import { createAppTransport } from '@/lib/transport'
import { getEffectiveConfig } from '@/lib/config'
import { PushNotificationService } from '@taskguild/proto/taskguild/v1/push_notification_pb.ts'

type PushStatus = 'unsupported' | 'ios-browser' | 'default' | 'denied' | 'granted' | 'subscribed'

export function usePushNotifications() {
  const [status, setStatus] = useState<PushStatus>('unsupported')

  useEffect(() => {
    const isIos = isIOSDevice()
    const isStandalone = isStandaloneApp()
    if (isIos && !isStandalone) {
      setStatus('ios-browser')
      return
    }

    if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
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
      if (!('Notification' in window)) {
        setStatus('unsupported')
        return
      }

      if (Notification.permission === 'denied') {
        setStatus('denied')
        return
      }

      if (Notification.permission !== 'granted') {
        const permission = await Notification.requestPermission()
        if (permission === 'denied') {
          setStatus('denied')
          return
        }
        if (permission !== 'granted') {
          setStatus('default')
          return
        }
        setStatus('granted')
      }

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

function isIOSDevice(): boolean {
  return /iphone|ipad|ipod/i.test(navigator.userAgent) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

function isStandaloneApp(): boolean {
  return (navigator as any).standalone === true ||
    window.matchMedia('(display-mode: standalone)').matches
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
