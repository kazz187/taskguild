import { useState, useEffect, useRef, useCallback } from 'react'
import { useMutation } from '@connectrpc/connect-query'
import {
  executeScript,
  stopScriptExecution,
} from '@taskguild/proto/taskguild/v1/script-ScriptService_connectquery.ts'
import {
  ScriptService,
  StreamScriptExecutionRequestSchema,
} from '@taskguild/proto/taskguild/v1/script_pb.ts'
import type { ScriptDefinition } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import { createClient } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { transport } from '@/lib/transport'
import type { ExecutionResult, LogEntry } from './ScriptListUtils'
import { protoLogToLocal } from './ScriptListUtils'

export function useScriptExecution(projectId: string) {
  const executeMut = useMutation(executeScript)
  const stopMut = useMutation(stopScriptExecution)

  const [executionResults, setExecutionResults] = useState<Map<string, ExecutionResult>>(new Map())

  // Mutable log buffers keyed by scriptId. Logs are appended here without
  // triggering React state updates, then a render counter is bumped to
  // re-render at a throttled rate.
  const logBuffersRef = useRef<Map<string, LogEntry[]>>(new Map())
  const renderTimerRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())

  // Track AbortControllers for active streams.
  const streamAbortRef = useRef<Map<string, AbortController>>(new Map())

  // Track whether we've already reconnected to active executions on mount.
  const reconnectedRef = useRef(false)

  // Cleanup active streams and render timers on unmount.
  useEffect(() => {
    return () => {
      streamAbortRef.current.forEach((controller) => controller.abort())
      renderTimerRef.current.forEach((timer) => clearTimeout(timer))
    }
  }, [])

  // Reconnect to active/recent executions on mount (page reload support).
  useEffect(() => {
    if (reconnectedRef.current) return
    reconnectedRef.current = true

    const client = createClient(ScriptService, transport)
    // Use the listActiveExecutions query descriptor to build a manual call
    const fetchActiveExecutions = async () => {
      try {
        const resp = await client.listActiveExecutions({ projectId })
        for (const exec of resp.executions) {
          // Set initial state
          setExecutionResults(prev => {
            const next = new Map(prev)
            if (next.has(exec.scriptId)) return next // already tracked
            next.set(exec.scriptId, {
              scriptId: exec.scriptId,
              requestId: exec.requestId,
              completed: false,
              logEntries: [],
            })
            return next
          })
          // Reconnect to stream (late-joiner will get buffered events)
          startStream(exec.scriptId, exec.requestId)
        }
      } catch (e) {
        console.error('Failed to fetch active executions:', e)
      }
    }
    fetchActiveExecutions()
  }, [projectId]) // eslint-disable-line react-hooks/exhaustive-deps

  // Schedule a throttled re-render for log output updates. Multiple output
  // events within the interval are batched into a single React re-render.
  const scheduleLogRender = useCallback((scriptId: string) => {
    if (renderTimerRef.current.has(scriptId)) return // already scheduled
    renderTimerRef.current.set(scriptId, setTimeout(() => {
      renderTimerRef.current.delete(scriptId)
      // Snapshot the current log buffer into state so React re-renders.
      const entries = logBuffersRef.current.get(scriptId)
      if (entries) {
        setExecutionResults(prev => {
          const next = new Map(prev)
          const existing = next.get(scriptId)
          if (!existing) return next
          // Only update logEntries reference (shallow copy of the array).
          next.set(scriptId, { ...existing, logEntries: entries.slice() })
          return next
        })
      }
    }, 200))
  }, [])

  const startStream = useCallback(async (scriptId: string, requestId: string) => {
    const client = createClient(ScriptService, transport)
    const controller = new AbortController()
    streamAbortRef.current.set(scriptId, controller)

    // Initialize log buffer for this script.
    if (!logBuffersRef.current.has(scriptId)) {
      logBuffersRef.current.set(scriptId, [])
    }

    try {
      const req = create(StreamScriptExecutionRequestSchema, { requestId })
      console.log('[ScriptStream] connecting', { scriptId, requestId })
      for await (const event of client.streamScriptExecution(req, {
        signal: controller.signal,
      })) {
        if (event.event.case === 'output') {
          const chunk = event.event.value
          const newEntries = protoLogToLocal(chunk.entries)
          console.debug('[ScriptStream] received output chunk', { scriptId, entries: newEntries.length })
          // Append to mutable buffer (no React state update per chunk).
          const buf = logBuffersRef.current.get(scriptId)
          if (buf) {
            buf.push(...newEntries)
          }
          // Schedule a throttled re-render.
          scheduleLogRender(scriptId)
        } else if (event.event.case === 'complete') {
          const result = event.event.value
          // Cancel any pending render timer — we'll do a final render now.
          const timer = renderTimerRef.current.get(scriptId)
          if (timer) {
            clearTimeout(timer)
            renderTimerRef.current.delete(scriptId)
          }
          const completeEntries = result.logEntries.length > 0
            ? protoLogToLocal(result.logEntries)
            : undefined
          // Use complete entries or current buffer for final state.
          const finalEntries = completeEntries ?? logBuffersRef.current.get(scriptId)?.slice() ?? []
          setExecutionResults(prev => {
            const next = new Map(prev)
            next.set(scriptId, {
              scriptId,
              requestId,
              completed: true,
              success: result.success,
              exitCode: result.exitCode,
              logEntries: finalEntries,
              errorMessage: result.errorMessage,
              stoppedByUser: result.stoppedByUser,
            })
            return next
          })
        } else {
          console.warn('[ScriptStream] unknown event case', event.event.case)
        }
      }
    } catch (e) {
      if (controller.signal.aborted) return
      console.error('Stream error:', e)
      // Cancel any pending render timer.
      const timer = renderTimerRef.current.get(scriptId)
      if (timer) {
        clearTimeout(timer)
        renderTimerRef.current.delete(scriptId)
      }
      setExecutionResults(prev => {
        const next = new Map(prev)
        const existing = next.get(scriptId)
        if (existing && !existing.completed) {
          next.set(scriptId, {
            ...existing,
            logEntries: logBuffersRef.current.get(scriptId)?.slice() ?? existing.logEntries,
            completed: true,
            success: false,
            errorMessage: e instanceof Error ? e.message : 'Stream connection lost',
          })
        }
        return next
      })
    } finally {
      streamAbortRef.current.delete(scriptId)
    }
  }, [scheduleLogRender])

  const doExecute = useCallback((script: ScriptDefinition) => {
    // Reset log buffer for this script.
    logBuffersRef.current.set(script.id, [])

    // Set pending state.
    setExecutionResults(prev => {
      const next = new Map(prev)
      next.set(script.id, {
        scriptId: script.id,
        requestId: '',
        completed: false,
        logEntries: [],
      })
      return next
    })

    executeMut.mutate(
      { projectId, scriptId: script.id },
      {
        onSuccess: (resp) => {
          const requestId = resp.requestId
          setExecutionResults(prev => {
            const next = new Map(prev)
            next.set(script.id, {
              scriptId: script.id,
              requestId,
              completed: false,
              logEntries: [],
            })
            return next
          })

          // Start server stream for real-time output.
          startStream(script.id, requestId)
        },
        onError: (err) => {
          setExecutionResults(prev => {
            const next = new Map(prev)
            next.set(script.id, {
              scriptId: script.id,
              requestId: '',
              completed: true,
              success: false,
              logEntries: [],
              errorMessage: err.message,
            })
            return next
          })
        },
      },
    )
  }, [executeMut, projectId, startStream])

  const handleStop = useCallback((requestId: string) => {
    stopMut.mutate({ requestId })
  }, [stopMut])

  const clearResult = useCallback((scriptId: string) => {
    // Abort any active stream.
    const controller = streamAbortRef.current.get(scriptId)
    if (controller) {
      controller.abort()
      streamAbortRef.current.delete(scriptId)
    }
    // Clean up log buffer and render timer.
    logBuffersRef.current.delete(scriptId)
    const timer = renderTimerRef.current.get(scriptId)
    if (timer) {
      clearTimeout(timer)
      renderTimerRef.current.delete(scriptId)
    }
    setExecutionResults(prev => {
      const next = new Map(prev)
      next.delete(scriptId)
      return next
    })
  }, [])

  return {
    executionResults,
    doExecute,
    handleStop,
    clearResult,
    stopMut,
  }
}
