// Utilities for parsing tool_input / tool_output JSON stored in TaskLog metadata.
//
// Historically the backend sometimes double-encoded string payloads (calling
// `json.Marshal` on a value that was already a JSON string). As a result some
// stored `tool_output` values parse to a *string* on the first JSON.parse; the
// actual object only emerges after a second parse. This helper handles both
// the new (single-encoded) and legacy (double-encoded) shapes transparently.

/** Sentinel indicating the input was not valid JSON at all. */
export const PARSE_FAILED: unique symbol = Symbol('PARSE_FAILED')

/**
 * Parse a JSON-encoded tool payload, transparently handling the legacy
 * double-encoded form.
 *
 * Behaviour:
 * - Tries JSON.parse(raw). If it fails, returns `PARSE_FAILED`.
 * - If the first parse yields a string, tries JSON.parse(first). If that
 *   succeeds, returns the inner value; otherwise returns the first string.
 * - Otherwise returns the first parse result.
 *
 * Note: a successful parse may legitimately yield `null`; callers must
 * distinguish that from parse failure via `PARSE_FAILED`.
 */
export function safeParseToolJSON(raw: string): unknown | typeof PARSE_FAILED {
  let first: unknown
  try {
    first = JSON.parse(raw)
  } catch {
    return PARSE_FAILED
  }

  if (typeof first === 'string') {
    try {
      return JSON.parse(first)
    } catch {
      return first
    }
  }

  return first
}
