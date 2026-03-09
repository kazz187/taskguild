import { useEffect } from 'react'

const APP_NAME = 'TaskGuild'

/**
 * Sets the document title to `TaskGuild | {subtitle}`.
 * When subtitle is undefined or empty, the title is just `TaskGuild`.
 * Restores the base title on unmount.
 */
export function useDocumentTitle(subtitle?: string) {
  useEffect(() => {
    document.title = subtitle ? `${APP_NAME} | ${subtitle}` : APP_NAME
    return () => {
      document.title = APP_NAME
    }
  }, [subtitle])
}
