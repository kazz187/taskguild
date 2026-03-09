// ============================================================================
// TaskGuild Service Worker
//
// Handles push notifications with action buttons for responding to
// interactions (permission requests and questions) directly from the
// notification without opening the app.
//
// Platform support:
//   - Chrome Android: Full action button support (up to 3 buttons) + inline reply
//   - Firefox Android: Action buttons (up to 2)
//   - iOS Safari: No action buttons; tap to open app (fallback)
//   - Desktop Chrome: Action buttons supported
// ============================================================================

/**
 * Respond to an interaction via the token-based API endpoint.
 * Uses a single-use token so no API key is exposed to the SW.
 *
 * @param {string} apiBaseUrl - Backend base URL
 * @param {string} responseToken - Single-use response token
 * @param {string} response - The response value (e.g. "allow", "deny")
 * @returns {Promise<boolean>} Whether the response was successful
 */
async function respondToInteraction(apiBaseUrl, responseToken, response) {
  try {
    const url = `${apiBaseUrl}/taskguild.v1.InteractionService/RespondToInteractionByToken`
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: responseToken, response }),
    })
    return res.ok
  } catch (err) {
    console.error('SW: Failed to respond to interaction', err)
    return false
  }
}

/**
 * Navigate an existing client window or open a new one.
 *
 * @param {string} url - The URL path to navigate to
 */
async function navigateToUrl(url) {
  const windowClients = await clients.matchAll({ type: 'window', includeUncontrolled: true })
  for (const client of windowClients) {
    if (client.url.includes(self.location.origin)) {
      client.focus()
      client.postMessage({ type: 'NAVIGATE', url })
      return
    }
  }
  return clients.openWindow(url)
}

// ============================================================================
// Push event — show notification with action buttons when available
// ============================================================================
self.addEventListener('push', (event) => {
  if (!event.data) return

  let payload
  try {
    payload = event.data.json()
  } catch {
    payload = { title: 'TaskGuild', body: event.data.text() }
  }

  const options = {
    body: payload.body || '',
    icon: '/favicon.ico',
    badge: '/favicon.ico',
    tag: payload.tag || 'taskguild',
    requireInteraction: true,
    data: {
      url: payload.url || '/',
      interactionId: payload.interactionId,
      responseToken: payload.responseToken,
      apiBaseUrl: payload.apiBaseUrl,
      type: payload.type,
    },
  }

  // Build notification actions from payload.
  if (payload.actions && payload.actions.length > 0) {
    options.actions = payload.actions.map((a) => {
      const actionDef = { action: a.action, title: a.title }
      // Chrome Android supports type: "text" for inline reply.
      if (a.type === 'text') {
        actionDef.type = 'text'
        actionDef.placeholder = 'Type your reply...'
      }
      return actionDef
    })
  }

  event.waitUntil(
    self.registration.showNotification(payload.title || 'TaskGuild', options)
  )
})

// ============================================================================
// Notification click — handle action buttons or body tap
// ============================================================================
self.addEventListener('notificationclick', (event) => {
  const notification = event.notification
  const data = notification.data || {}
  const action = event.action

  notification.close()

  // If no action (user tapped the notification body), navigate to the app.
  if (!action) {
    event.waitUntil(navigateToUrl(data.url || '/'))
    return
  }

  // An action button was pressed. Try to respond via the API.
  if (!data.responseToken || !data.apiBaseUrl) {
    // No token available — fall back to opening the app.
    event.waitUntil(navigateToUrl(data.url || '/'))
    return
  }

  // Determine the response value.
  // - Permission Request: action names match values (allow, always_allow, deny)
  // - Question: action is "option_N" which maps to the option value
  // - Reply: inline text from event.reply (Chrome Android)
  let responseValue = action

  // Handle inline text reply (Chrome Android).
  if (action === 'reply' && event.reply) {
    responseValue = event.reply
  }

  event.waitUntil(
    respondToInteraction(data.apiBaseUrl, data.responseToken, responseValue).then((success) => {
      if (success) {
        // Notify open clients that the interaction was responded to.
        clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windowClients) => {
          for (const client of windowClients) {
            client.postMessage({
              type: 'INTERACTION_RESPONDED',
              interactionId: data.interactionId,
              response: responseValue,
            })
          }
        })
      } else {
        // API call failed — fall back to opening the app.
        navigateToUrl(data.url || '/')
      }
    })
  )
})
