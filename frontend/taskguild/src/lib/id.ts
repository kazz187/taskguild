/** Number of trailing characters to show when displaying a truncated ID. */
export const SHORT_ID_LENGTH = 8

/** Return the last SHORT_ID_LENGTH characters of an ID for display. */
export function shortId(id: string): string {
  return id.slice(-SHORT_ID_LENGTH)
}
