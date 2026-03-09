import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listScripts,
  createScript,
  updateScript,
  deleteScript,
  syncScriptsFromDir,
  executeScript,
  stopScriptExecution,
  listActiveExecutions,
} from '@taskguild/proto/taskguild/v1/script-ScriptService_connectquery.ts'
import { saveAsTemplate, listTemplates } from '@taskguild/proto/taskguild/v1/template-TemplateService_connectquery.ts'
import {
  requestScriptComparison,
  getScriptComparison,
  resolveScriptConflict,
} from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import {
  ScriptService,
  StreamScriptExecutionRequestSchema,
  ScriptLogStream,
} from '@taskguild/proto/taskguild/v1/script_pb.ts'
import type { ScriptDefinition, ScriptLogEntry } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import type { ScriptDiff } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { ScriptDiffType, ScriptResolutionChoice } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { Terminal, Plus, Trash2, Edit2, RefreshCw, X, Save, Cloud, Play, Square, CheckCircle, XCircle, StopCircle, Loader2, Layers, Copy, AlertTriangle, Server, Monitor } from 'lucide-react'
import { createClient } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { transport } from '@/lib/transport'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { AutoScrollPre } from './AutoScrollPre'
import { Button, Input, Textarea, Badge } from '../atoms/index.ts'
import { Card, FormField, Modal } from '../molecules/index.ts'

interface ScriptFormData {
  name: string
  description: string
  filename: string
  content: string
}

const emptyForm: ScriptFormData = {
  name: '',
  description: '',
  filename: '',
  content: '',
}

function scriptToForm(s: ScriptDefinition): ScriptFormData {
  return {
    name: s.name,
    description: s.description,
    filename: s.filename,
    content: s.content,
  }
}

interface LogEntry {
  stream: 'stdout' | 'stderr'
  text: string
}

interface ExecutionResult {
  scriptId: string
  requestId: string
  completed: boolean
  success?: boolean
  exitCode?: number
  logEntries: LogEntry[]
  errorMessage?: string
  stoppedByUser?: boolean
}

function protoLogToLocal(entries: ScriptLogEntry[]): LogEntry[] {
  return entries.map(e => ({
    stream: e.stream === ScriptLogStream.STDERR ? 'stderr' : 'stdout',
    text: e.text,
  }))
}

function diffTypeLabel(dt: ScriptDiffType): string {
  switch (dt) {
    case ScriptDiffType.MODIFIED: return 'Modified'
    case ScriptDiffType.AGENT_ONLY: return 'Agent only'
    case ScriptDiffType.SERVER_ONLY: return 'Server only'
    default: return 'Unknown'
  }
}

/** Renders interleaved log entries with stderr in red */
function LogOutput({ entries, className }: { entries: LogEntry[]; className: string }) {
  return (
    <AutoScrollPre
      scrollKey={entries.length}
      className={className}
    >
      {entries.map((entry, i) => (
        <span
          key={i}
          className={entry.stream === 'stderr' ? 'text-red-400' : 'text-gray-300'}
        >
          {entry.text}
        </span>
      ))}
    </AutoScrollPre>
  )
}

export function ScriptList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(listScripts, { projectId })
  const createMut = useMutation(createScript)
  const updateMut = useMutation(updateScript)
  const deleteMut = useMutation(deleteScript)
  const syncMut = useMutation(syncScriptsFromDir)
  const executeMut = useMutation(executeScript)
  const stopMut = useMutation(stopScriptExecution)
  const saveTemplateMut = useMutation(saveAsTemplate)
  const { data: templatesData, refetch: refetchTemplates } = useQuery(listTemplates, { entityType: 'script' })

  // Script comparison
  const requestComparisonMut = useMutation(requestScriptComparison)
  const { data: comparisonData, refetch: refetchComparison } = useQuery(getScriptComparison, { projectId })
  const resolveConflictMut = useMutation(resolveScriptConflict)

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<ScriptFormData>(emptyForm)
  const [executionResults, setExecutionResults] = useState<Map<string, ExecutionResult>>(new Map())

  // Template dialog state
  const [saveTemplateDialog, setSaveTemplateDialog] = useState<{ scriptId: string; name: string; description: string } | null>(null)
  const [templatePickerOpen, setTemplatePickerOpen] = useState(false)

  // Diff resolution dialog state
  const [diffDialog, setDiffDialog] = useState<ScriptDiff | null>(null)
  // When resolving from a Run click, store the script to execute after resolution
  const [pendingExecuteScript, setPendingExecuteScript] = useState<ScriptDefinition | null>(null)

  // Track AbortControllers for active streams.
  const streamAbortRef = useRef<Map<string, AbortController>>(new Map())

  // Track whether we've already reconnected to active executions on mount.
  const reconnectedRef = useRef(false)

  const scripts = data?.scripts ?? []
  const scriptTemplates = templatesData?.templates ?? []
  const diffs = comparisonData?.diffs ?? []

  // Build a lookup map for diffs by script_id and filename.
  const diffByScriptId = useMemo(() => {
    const map = new Map<string, ScriptDiff>()
    for (const d of diffs) {
      if (d.scriptId) map.set(d.scriptId, d)
    }
    return map
  }, [diffs])

  // Subscribe to SCRIPT_COMPARISON events to refetch diffs when comparison completes.
  const comparisonEventTypes = useMemo(() => [EventType.SCRIPT_COMPARISON], [])
  const onComparisonEvent = useCallback(() => {
    refetchComparison()
    refetch()
  }, [refetchComparison, refetch])
  useEventSubscription(comparisonEventTypes, projectId, onComparisonEvent)

  // Cleanup active streams on unmount.
  useEffect(() => {
    return () => {
      streamAbortRef.current.forEach((controller) => controller.abort())
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

  const openCreate = () => {
    setFormMode('create')
    setEditingId(null)
    setForm(emptyForm)
  }

  const openEdit = (s: ScriptDefinition) => {
    setFormMode('edit')
    setEditingId(s.id)
    setForm(scriptToForm(s))
  }

  const closeForm = () => {
    setFormMode(null)
    setEditingId(null)
    setForm(emptyForm)
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const filename = form.filename || form.name + '.sh'
    if (formMode === 'create') {
      createMut.mutate(
        { projectId, ...form, filename },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    } else if (formMode === 'edit' && editingId) {
      updateMut.mutate(
        { id: editingId, ...form, filename },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    }
  }

  const handleDelete = (id: string) => {
    if (!confirm('Delete this script?')) return
    deleteMut.mutate({ id }, { onSuccess: () => refetch() })
  }

  const handleSync = () => {
    syncMut.mutate(
      { projectId, directory: '.' },
      {
        onSuccess: () => {
          refetch()
          // After syncing from repo, automatically trigger comparison with agent.
          requestComparisonMut.mutate({ projectId })
        },
      },
    )
  }

  const handleSaveAsTemplate = (script: ScriptDefinition) => {
    setSaveTemplateDialog({ scriptId: script.id, name: script.name, description: script.description })
  }

  const handleSaveTemplateSubmit = () => {
    if (!saveTemplateDialog) return
    saveTemplateMut.mutate(
      { entityType: 'script', entityId: saveTemplateDialog.scriptId, templateName: saveTemplateDialog.name, templateDescription: saveTemplateDialog.description },
      { onSuccess: () => { setSaveTemplateDialog(null); refetchTemplates() } },
    )
  }

  const handleCreateFromTemplate = (tmpl: Template) => {
    if (!tmpl.scriptConfig) return
    setTemplatePickerOpen(false)
    setFormMode('create')
    setEditingId(null)
    setForm({
      name: tmpl.scriptConfig.name,
      description: tmpl.scriptConfig.description,
      filename: tmpl.scriptConfig.filename,
      content: tmpl.scriptConfig.content,
    })
  }

  const startStream = useCallback(async (scriptId: string, requestId: string) => {
    const client = createClient(ScriptService, transport)
    const controller = new AbortController()
    streamAbortRef.current.set(scriptId, controller)

    try {
      const req = create(StreamScriptExecutionRequestSchema, { requestId })
      for await (const event of client.streamScriptExecution(req, {
        signal: controller.signal,
      })) {
        if (event.event.case === 'output') {
          const chunk = event.event.value
          const newEntries = protoLogToLocal(chunk.entries)
          console.debug('[ScriptStream] output event', { scriptId, requestId, entryCount: newEntries.length })
          setExecutionResults(prev => {
            const next = new Map(prev)
            const existing = next.get(scriptId)
            if (!existing) return next
            next.set(scriptId, {
              ...existing,
              logEntries: [...existing.logEntries, ...newEntries],
            })
            return next
          })
        } else if (event.event.case === 'complete') {
          const result = event.event.value
          console.debug('[ScriptStream] complete event', {
            scriptId, requestId,
            success: result.success,
            logEntryCount: result.logEntries.length,
          })
          const completeEntries = result.logEntries.length > 0
            ? protoLogToLocal(result.logEntries)
            : undefined
          setExecutionResults(prev => {
            const next = new Map(prev)
            const existing = next.get(scriptId)
            next.set(scriptId, {
              scriptId,
              requestId,
              completed: true,
              success: result.success,
              exitCode: result.exitCode,
              logEntries: completeEntries ?? existing?.logEntries ?? [],
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
      setExecutionResults(prev => {
        const next = new Map(prev)
        const existing = next.get(scriptId)
        if (existing && !existing.completed) {
          next.set(scriptId, {
            ...existing,
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
  }, [])

  const doExecute = (script: ScriptDefinition) => {
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
  }

  const handleExecute = (script: ScriptDefinition) => {
    // Check if this script has an unresolved diff.
    const diff = diffByScriptId.get(script.id)
    if (diff) {
      // Show resolution dialog and defer execution.
      setDiffDialog(diff)
      setPendingExecuteScript(script)
      return
    }
    doExecute(script)
  }

  const handleStop = (requestId: string) => {
    stopMut.mutate({ requestId })
  }

  const handleResolveConflict = (diff: ScriptDiff, choice: ScriptResolutionChoice) => {
    resolveConflictMut.mutate(
      {
        projectId,
        scriptId: diff.scriptId,
        scriptName: diff.scriptName,
        filename: diff.filename,
        choice,
        agentContent: choice === ScriptResolutionChoice.AGENT ? diff.agentContent : '',
      },
      {
        onSuccess: () => {
          // Refresh diffs and script list.
          refetchComparison()
          refetch()
          setDiffDialog(null)

          // If we were pending execution, now execute.
          if (pendingExecuteScript) {
            doExecute(pendingExecuteScript)
            setPendingExecuteScript(null)
          }
        },
      },
    )
  }

  const clearResult = (scriptId: string) => {
    // Abort any active stream.
    const controller = streamAbortRef.current.get(scriptId)
    if (controller) {
      controller.abort()
      streamAbortRef.current.delete(scriptId)
    }
    setExecutionResults(prev => {
      const next = new Map(prev)
      next.delete(scriptId)
      return next
    })
  }

  const mutation = formMode === 'create' ? createMut : updateMut

  // Agent-only diffs (scripts that exist only on agent, not in server DB).
  const agentOnlyDiffs = diffs.filter(d => d.diffType === ScriptDiffType.AGENT_ONLY)

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-2">
          <Terminal className="w-5 h-5 text-green-400" />
          <h2 className="text-xl font-bold text-white">Scripts</h2>
          <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5">
            {scripts.length}
          </span>
          {diffs.length > 0 && (
            <Badge color="amber" size="xs" variant="outline" pill icon={<AlertTriangle className="w-2.5 h-2.5" />}>
              {diffs.length} diff{diffs.length > 1 ? 's' : ''}
            </Badge>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleSync}
            disabled={syncMut.isPending || requestComparisonMut.isPending}
            icon={<RefreshCw className={`w-4 h-4 ${(syncMut.isPending || requestComparisonMut.isPending) ? 'animate-spin' : ''}`} />}
            title="Sync scripts from .claude/scripts/ directory"
            className="border border-slate-700 hover:border-slate-600"
          >
            Sync from Repo
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => { refetchTemplates(); setTemplatePickerOpen(true) }}
            icon={<Layers className="w-4 h-4" />}
            title="Create script from template"
            className="border border-slate-700 hover:border-slate-600"
          >
            From Template
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={openCreate}
            icon={<Plus className="w-4 h-4" />}
            className="bg-green-600 hover:bg-green-500"
          >
            New Script
          </Button>
        </div>
      </div>

      {syncMut.isSuccess && (
        <div className="mb-4 px-3 py-2 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm">
          Synced: {syncMut.data?.created ?? 0} created, {syncMut.data?.updated ?? 0} updated
          {requestComparisonMut.isPending && (
            <span className="ml-2 text-blue-400">
              <Loader2 className="w-3 h-3 animate-spin inline mr-1" />
              Comparing with agent...
            </span>
          )}
        </div>
      )}

      {/* Script Form */}
      {formMode && (
        <form onSubmit={handleSubmit}>
          <Card className="p-5 mb-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">
                {formMode === 'create' ? 'New Script' : 'Edit Script'}
              </h3>
              <Button variant="ghost" size="sm" iconOnly onClick={closeForm} type="button" icon={<X className="w-5 h-5" />} />
            </div>

            <div className="space-y-4">
              {/* Name & Description row */}
              <div className="grid grid-cols-2 gap-3">
                <FormField label="Name *" hint="Script name (used as identifier)">
                  <Input
                    type="text"
                    required
                    value={form.name}
                    onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
                    className="focus:border-green-500"
                    placeholder="e.g. deploy"
                  />
                </FormField>
                <FormField label="Description">
                  <Input
                    type="text"
                    value={form.description}
                    onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))}
                    className="focus:border-green-500"
                    placeholder="What this script does"
                  />
                </FormField>
              </div>

              {/* Filename */}
              <FormField label="Filename" hint="Defaults to name.sh if empty">
                <Input
                  type="text"
                  value={form.filename}
                  onChange={e => setForm(prev => ({ ...prev, filename: e.target.value }))}
                  className="focus:border-green-500"
                  placeholder={form.name ? `${form.name}.sh` : 'e.g. deploy.sh'}
                />
              </FormField>

              {/* Content */}
              <FormField label="Script Content *" hint="Shell script to execute on the agent-manager machine.">
                <Textarea
                  required
                  value={form.content}
                  onChange={e => setForm(prev => ({ ...prev, content: e.target.value }))}
                  mono
                  className="focus:border-green-500 min-h-[200px]"
                  placeholder={'#!/bin/bash\necho "Hello from script"'}
                />
              </FormField>
            </div>

            {mutation.error && (
              <p className="text-red-400 text-sm mt-3">{mutation.error.message}</p>
            )}

            <div className="flex justify-end gap-2 mt-4">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={closeForm}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                variant="primary"
                size="sm"
                disabled={mutation.isPending || !form.name || !form.content}
                icon={<Save className="w-3.5 h-3.5" />}
                className="bg-green-600 hover:bg-green-500"
              >
                {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
              </Button>
            </div>
          </Card>
        </form>
      )}

      {/* Script Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading scripts...</p>}

      <div className="space-y-3">
        {scripts.map(script => {
          const result = executionResults.get(script.id)
          const diff = diffByScriptId.get(script.id)
          const isRunning = result && !result.completed
          return (
            <Card
              key={script.id}
              className={`hover:border-slate-700 transition-colors ${diff ? 'border-amber-500/30' : ''}`}
            >
              <div className="flex items-start justify-between">
                <div className="flex items-start gap-3 flex-1 min-w-0">
                  <Terminal className="w-5 h-5 text-green-400 mt-0.5 shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <h3 className="text-sm font-semibold text-white truncate">{script.name}</h3>
                      <Badge color="gray" size="xs" className="font-mono bg-slate-800 text-gray-500">
                        {script.filename}
                      </Badge>
                      {script.isSynced && (
                        <Badge color="blue" size="xs" variant="outline" pill icon={<Cloud className="w-2.5 h-2.5" />}>
                          synced
                        </Badge>
                      )}
                      {diff && (
                        <Badge
                          color="amber"
                          size="xs"
                          variant="outline"
                          pill
                          icon={<AlertTriangle className="w-2.5 h-2.5" />}
                          className="cursor-pointer hover:bg-amber-500/10"
                          onClick={() => { setDiffDialog(diff); setPendingExecuteScript(null) }}
                        >
                          {diffTypeLabel(diff.diffType)}
                        </Badge>
                      )}
                    </div>
                    {script.description && (
                      <p className="text-xs text-gray-400 mb-2">{script.description}</p>
                    )}
                    {script.content && (
                      <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                        {script.content.slice(0, 120)}{script.content.length > 120 ? '...' : ''}
                      </pre>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-1 shrink-0 ml-2">
                  {isRunning ? (
                    <Button
                      variant="danger"
                      size="sm"
                      onClick={() => handleStop(result.requestId)}
                      disabled={stopMut.isPending}
                      icon={<Square className="w-3.5 h-3.5" />}
                      title="Stop script"
                      className="bg-red-600 hover:bg-red-500"
                    >
                      Stop
                    </Button>
                  ) : (
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={() => handleExecute(script)}
                      icon={<Play className="w-3.5 h-3.5" />}
                      title="Run script"
                      className="bg-green-600 hover:bg-green-500"
                    >
                      Run
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    iconOnly
                    onClick={() => handleSaveAsTemplate(script)}
                    title="Save as Template"
                    className="hover:text-amber-400"
                    icon={<Copy className="w-3.5 h-3.5" />}
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    iconOnly
                    onClick={() => openEdit(script)}
                    title="Edit"
                    className="hover:text-green-400"
                    icon={<Edit2 className="w-3.5 h-3.5" />}
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    iconOnly
                    onClick={() => handleDelete(script.id)}
                    disabled={deleteMut.isPending}
                    title="Delete"
                    className="hover:text-red-400"
                    icon={<Trash2 className="w-3.5 h-3.5" />}
                  />
                </div>
              </div>

              {/* Execution Result -- completed */}
              {result && result.completed && (
                <Card variant={result.success ? 'success' : 'error'} className="mt-3">
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      {result.stoppedByUser ? (
                        <StopCircle className="w-4 h-4 text-amber-400" />
                      ) : result.success ? (
                        <CheckCircle className="w-4 h-4 text-green-400" />
                      ) : (
                        <XCircle className="w-4 h-4 text-red-400" />
                      )}
                      <span className={`text-sm font-medium ${
                        result.stoppedByUser ? 'text-amber-400' :
                        result.success ? 'text-green-400' : 'text-red-400'
                      }`}>
                        {result.stoppedByUser
                          ? 'Stopped by user'
                          : result.success
                            ? 'Success'
                            : 'Failed'}
                        {result.exitCode !== undefined && !result.stoppedByUser && ` (exit code: ${result.exitCode})`}
                      </span>
                    </div>
                    <Button
                      variant="ghost"
                      size="xs"
                      iconOnly
                      onClick={() => clearResult(script.id)}
                      icon={<X className="w-3.5 h-3.5" />}
                    />
                  </div>
                  {result.errorMessage && !result.stoppedByUser && (
                    <div className="text-xs text-red-400 font-mono bg-slate-900/50 rounded p-2 mb-2 whitespace-pre-wrap">
                      {result.errorMessage}
                    </div>
                  )}
                  {result.logEntries.length > 0 && (
                    <LogOutput
                      entries={result.logEntries}
                      className="text-xs font-mono bg-slate-900/50 rounded p-2 mt-0.5 whitespace-pre-wrap max-h-[300px] overflow-y-auto"
                    />
                  )}
                </Card>
              )}

              {/* Execution in progress -- streaming output */}
              {result && !result.completed && (
                <div className="mt-3 border border-blue-500/20 bg-blue-500/5 rounded-lg p-3">
                  <div className="flex items-center gap-2 mb-2">
                    <Loader2 className="w-4 h-4 text-blue-400 animate-spin" />
                    <span className="text-sm text-blue-400">Executing script...</span>
                  </div>
                  {result.logEntries.length > 0 && (
                    <LogOutput
                      entries={result.logEntries}
                      className="text-xs font-mono bg-slate-900/50 rounded p-2 mt-0.5 whitespace-pre-wrap max-h-[300px] overflow-y-auto"
                    />
                  )}
                </div>
              )}
            </Card>
          )
        })}

        {/* Agent-only scripts (exist on agent but not on server) */}
        {agentOnlyDiffs.map(diff => (
          <Card
            key={`agent-only-${diff.filename}`}
            className="hover:border-slate-700 transition-colors border-amber-500/30"
          >
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3 flex-1 min-w-0">
                <Terminal className="w-5 h-5 text-amber-400 mt-0.5 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <h3 className="text-sm font-semibold text-white truncate">{diff.scriptName}</h3>
                    <Badge color="gray" size="xs" className="font-mono bg-slate-800 text-gray-500">
                      {diff.filename}
                    </Badge>
                    <Badge color="amber" size="xs" variant="outline" pill icon={<Monitor className="w-2.5 h-2.5" />}>
                      Agent only
                    </Badge>
                  </div>
                  {diff.agentContent && (
                    <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                      {diff.agentContent.slice(0, 120)}{diff.agentContent.length > 120 ? '...' : ''}
                    </pre>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-1 shrink-0 ml-2">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => { setDiffDialog(diff); setPendingExecuteScript(null) }}
                  icon={<AlertTriangle className="w-3.5 h-3.5" />}
                  className="text-amber-400 hover:text-amber-300 border border-amber-500/30"
                >
                  Resolve
                </Button>
              </div>
            </div>
          </Card>
        ))}

        {!isLoading && scripts.length === 0 && agentOnlyDiffs.length === 0 && !formMode && (
          <div className="text-center py-12 text-gray-500">
            <Terminal className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No scripts defined yet.</p>
            <p className="text-xs mt-1">Create scripts or sync from your repository's .claude/scripts/ directory.</p>
          </div>
        )}
      </div>

      {/* Diff Resolution Dialog */}
      <Modal open={!!diffDialog} onClose={() => { setDiffDialog(null); setPendingExecuteScript(null) }} size="lg">
        <Modal.Header onClose={() => { setDiffDialog(null); setPendingExecuteScript(null) }}>
          <div className="flex items-center gap-2">
            <AlertTriangle className="w-5 h-5 text-amber-400" />
            <h3 className="text-lg font-semibold text-white">Script Conflict</h3>
          </div>
        </Modal.Header>
        <Modal.Body>
          {diffDialog && (
            <div className="space-y-4">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-gray-400">Script:</span>
                <span className="text-white font-medium">{diffDialog.scriptName}</span>
                <Badge color="gray" size="xs" className="font-mono">{diffDialog.filename}</Badge>
                <Badge color="amber" size="xs" variant="outline">{diffTypeLabel(diffDialog.diffType)}</Badge>
              </div>

              {pendingExecuteScript && (
                <div className="px-3 py-2 bg-amber-500/10 border border-amber-500/20 rounded-lg text-amber-400 text-sm">
                  This script has local modifications on the agent. Please choose which version to use before execution.
                </div>
              )}

              <div className="grid grid-cols-2 gap-3">
                {/* Server version */}
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Server className="w-4 h-4 text-blue-400" />
                    <span className="text-sm font-medium text-blue-400">Server Version</span>
                  </div>
                  <pre className="text-xs text-gray-300 font-mono bg-slate-900 rounded p-3 whitespace-pre-wrap max-h-[400px] overflow-y-auto border border-slate-700">
                    {diffDialog.serverContent || <span className="text-gray-600 italic">No server version</span>}
                  </pre>
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => handleResolveConflict(diffDialog, ScriptResolutionChoice.SERVER)}
                    disabled={resolveConflictMut.isPending || diffDialog.diffType === ScriptDiffType.AGENT_ONLY}
                    icon={<Server className="w-3.5 h-3.5" />}
                    className="w-full bg-blue-600 hover:bg-blue-500"
                  >
                    {resolveConflictMut.isPending ? 'Resolving...' : 'Use Server Version'}
                  </Button>
                </div>

                {/* Agent version */}
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Monitor className="w-4 h-4 text-green-400" />
                    <span className="text-sm font-medium text-green-400">Agent Version</span>
                  </div>
                  <pre className="text-xs text-gray-300 font-mono bg-slate-900 rounded p-3 whitespace-pre-wrap max-h-[400px] overflow-y-auto border border-slate-700">
                    {diffDialog.agentContent || <span className="text-gray-600 italic">No agent version</span>}
                  </pre>
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => handleResolveConflict(diffDialog, ScriptResolutionChoice.AGENT)}
                    disabled={resolveConflictMut.isPending || diffDialog.diffType === ScriptDiffType.SERVER_ONLY}
                    icon={<Monitor className="w-3.5 h-3.5" />}
                    className="w-full bg-green-600 hover:bg-green-500"
                  >
                    {resolveConflictMut.isPending ? 'Resolving...' : 'Use Agent Version'}
                  </Button>
                </div>
              </div>

              {resolveConflictMut.error && (
                <p className="text-red-400 text-sm">{resolveConflictMut.error.message}</p>
              )}
            </div>
          )}
        </Modal.Body>
      </Modal>

      {/* Template Picker Dialog */}
      <Modal open={templatePickerOpen} onClose={() => setTemplatePickerOpen(false)} size="sm">
        <Modal.Header onClose={() => setTemplatePickerOpen(false)}>
          <h3 className="text-lg font-semibold text-white">Select Script Template</h3>
        </Modal.Header>
        <Modal.Body>
          {scriptTemplates.length === 0 ? (
            <p className="text-gray-500 text-sm text-center py-6">No script templates available.</p>
          ) : (
            <div className="space-y-2">
              {scriptTemplates.map(tmpl => (
                <button key={tmpl.id} onClick={() => handleCreateFromTemplate(tmpl)} className="w-full text-left p-3 bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors">
                  <div className="flex items-center gap-2 mb-1">
                    <Terminal className="w-4 h-4 text-green-400" />
                    <span className="text-sm font-medium text-white">{tmpl.name}</span>
                  </div>
                  {tmpl.description && <p className="text-xs text-gray-400 ml-6">{tmpl.description}</p>}
                </button>
              ))}
            </div>
          )}
        </Modal.Body>
      </Modal>

      {/* Save as Template Dialog */}
      <Modal open={!!saveTemplateDialog} onClose={() => setSaveTemplateDialog(null)} size="sm">
        <Modal.Header onClose={() => setSaveTemplateDialog(null)}>
          <h3 className="text-lg font-semibold text-white">Save as Template</h3>
        </Modal.Header>
        <Modal.Body>
          <FormField label="Template Name">
            <Input type="text" value={saveTemplateDialog?.name ?? ''}
              onChange={e => setSaveTemplateDialog(prev => prev ? { ...prev, name: e.target.value } : null)}
              className="focus:border-amber-500" />
          </FormField>
          <FormField label="Template Description">
            <Input type="text" value={saveTemplateDialog?.description ?? ''}
              onChange={e => setSaveTemplateDialog(prev => prev ? { ...prev, description: e.target.value } : null)}
              className="focus:border-amber-500" />
          </FormField>
          {saveTemplateMut.error && <p className="text-red-400 text-sm mt-3">{saveTemplateMut.error.message}</p>}
          {saveTemplateMut.isSuccess && <p className="text-green-400 text-sm mt-3">Template saved successfully!</p>}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" size="sm" onClick={() => setSaveTemplateDialog(null)}>Cancel</Button>
          <Button
            variant="danger"
            size="sm"
            onClick={handleSaveTemplateSubmit}
            disabled={saveTemplateMut.isPending || !saveTemplateDialog?.name}
            icon={<Save className="w-3.5 h-3.5" />}
          >
            {saveTemplateMut.isPending ? 'Saving...' : 'Save Template'}
          </Button>
        </Modal.Footer>
      </Modal>
    </div>
  )
}
