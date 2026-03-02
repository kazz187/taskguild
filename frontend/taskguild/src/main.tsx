import ReactDOM from 'react-dom/client'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'
import { ConfigProvider } from './components/ConfigProvider'

const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
  scrollRestoration: true,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const rootElement = document.getElementById('app')!

if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement)
  root.render(
    <ConfigProvider>
      <RouterProvider router={router} />
    </ConfigProvider>
  )
}

if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch((err) => {
    console.warn('SW registration failed:', err)
  })

  // Listen for messages from the Service Worker.
  navigator.serviceWorker.addEventListener('message', (event) => {
    const data = event.data
    if (!data || !data.type) return

    switch (data.type) {
      case 'NAVIGATE':
        // Navigate to the URL from a notification click.
        if (data.url) {
          router.navigate({ to: data.url })
        }
        break

      case 'INTERACTION_RESPONDED':
        // An interaction was responded to from a push notification action.
        // The event subscription will pick up the change automatically,
        // but we can log it for debugging.
        console.info('Interaction responded from notification:', data.interactionId, data.response)
        break
    }
  })
}
