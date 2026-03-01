import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listScripts,
  createScript,
  updateScript,
  deleteScript,
  syncScriptsFromDir,
  executeScript,
  getScriptExecutionResult,
} from '@taskguild/proto/taskguild/v1/script-ScriptService_connectquery.ts'
import type { ScriptDefinition } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import { Terminal, Plus, Trash2, Edit2, RefreshCw, X, Save, Cloud, Play, CheckCircle, XCircle, Loader2 } from 'lucide-react'
import { useTransport } from '@connectrpc/connect-query'
import { createClient } from '@connectrpc/connect'
import { ScriptService } from '@taskguild/proto/taskguild/v1/script_pb.ts'

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

interface ExecutionResult {
  scriptId: string
  requestId: string
  completed: boolean
  success?: boolean
  exitCode?: number
  stdout?: string
  stderr?: string
  errorMessage?: string
}

export function ScriptList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(listScripts, { projectId })
  const createMut = useMutation(createScript)
  const updateMut = useMutation(updateScript)
  const deleteMut = useMutation(deleteScript)
  const syncMut = useMutation(syncScriptsFromDir)
  const executeMut = useMutation(executeScript)

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<ScriptFormData>(emptyForm)
  const [executionResults, setExecutionResults] = useState<Map<string, ExecutionResult>>(new Map())

  const transport = useTransport()
  const pollingRef = useRef<Map<string, NodeJS.Timeout>>(new Map())

  const scripts = data?.scripts ?? []

  // Cleanup polling intervals on unmount
  useEffect(() => {
    return () => {
      pollingRef.current.forEach((interval) => clearInterval(interval))
    }
  }, [])

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
      { onSuccess: () => refetch() },
    )
  }

  const handleExecute = (script: ScriptDefinition) => {
    // Set pending state
    setExecutionResults(prev => {
      const next = new Map(prev)
      next.set(script.id, {
        scriptId: script.id,
        requestId: '',
        completed: false,
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
            })
            return next
          })

          // Poll for result
          const client = createClient(ScriptService, transport)
          const pollInterval = setInterval(async () => {
            try {
              const result = await client.getScriptExecutionResult({ requestId })
              if (result.completed) {
                clearInterval(pollInterval)
                pollingRef.current.delete(script.id)
                setExecutionResults(prev => {
                  const next = new Map(prev)
                  next.set(script.id, {
                    scriptId: script.id,
                    requestId,
                    completed: true,
                    success: result.success,
                    exitCode: result.exitCode,
                    stdout: result.stdout,
                    stderr: result.stderr,
                    errorMessage: result.errorMessage,
                  })
                  return next
                })
              }
            } catch {
              // Continue polling
            }
          }, 1000)

          pollingRef.current.set(script.id, pollInterval)

          // Stop polling after 5 minutes
          setTimeout(() => {
            if (pollingRef.current.has(script.id)) {
              clearInterval(pollingRef.current.get(script.id))
              pollingRef.current.delete(script.id)
              setExecutionResults(prev => {
                const next = new Map(prev)
                const existing = next.get(script.id)
                if (existing && !existing.completed) {
                  next.set(script.id, {
                    ...existing,
                    completed: true,
                    success: false,
                    errorMessage: 'Execution timed out (no result received within 5 minutes)',
                  })
                }
                return next
              })
            }
          }, 5 * 60 * 1000)
        },
        onError: (err) => {
          setExecutionResults(prev => {
            const next = new Map(prev)
            next.set(script.id, {
              scriptId: script.id,
              requestId: '',
              completed: true,
              success: false,
              errorMessage: err.message,
            })
            return next
          })
        },
      },
    )
  }

  const clearResult = (scriptId: string) => {
    setExecutionResults(prev => {
      const next = new Map(prev)
      next.delete(scriptId)
      return next
    })
  }

  const mutation = formMode === 'create' ? createMut : updateMut

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-2">
          <Terminal className="w-5 h-5 text-green-400" />
          <h2 className="text-xl font-bold text-white">Scripts</h2>
          <span className="text-xs text-gray-500 bg-slate-800 rounded-full px-2 py-0.5">
            {scripts.length}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleSync}
            disabled={syncMut.isPending}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-gray-400 hover:text-white border border-slate-700 hover:border-slate-600 rounded-lg transition-colors disabled:opacity-50"
            title="Sync scripts from .claude/scripts/ directory"
          >
            <RefreshCw className={`w-4 h-4 ${syncMut.isPending ? 'animate-spin' : ''}`} />
            Sync from Repo
          </button>
          <button
            onClick={openCreate}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-green-600 hover:bg-green-500 text-white rounded-lg transition-colors"
          >
            <Plus className="w-4 h-4" />
            New Script
          </button>
        </div>
      </div>

      {syncMut.isSuccess && (
        <div className="mb-4 px-3 py-2 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm">
          Synced: {syncMut.data?.created ?? 0} created, {syncMut.data?.updated ?? 0} updated
        </div>
      )}

      {/* Script Form */}
      {formMode && (
        <form onSubmit={handleSubmit} className="bg-slate-900 border border-slate-800 rounded-xl p-5 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-lg font-semibold text-white">
              {formMode === 'create' ? 'New Script' : 'Edit Script'}
            </h3>
            <button type="button" onClick={closeForm} className="text-gray-500 hover:text-gray-300 transition-colors">
              <X className="w-5 h-5" />
            </button>
          </div>

          <div className="space-y-4">
            {/* Name & Description row */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs text-gray-400 mb-1">Name *</label>
                <input
                  type="text"
                  required
                  value={form.name}
                  onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-green-500"
                  placeholder="e.g. deploy"
                />
                <p className="text-[10px] text-gray-600 mt-0.5">Script name (used as identifier)</p>
              </div>
              <div>
                <label className="block text-xs text-gray-400 mb-1">Description</label>
                <input
                  type="text"
                  value={form.description}
                  onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-green-500"
                  placeholder="What this script does"
                />
              </div>
            </div>

            {/* Filename */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">Filename</label>
              <input
                type="text"
                value={form.filename}
                onChange={e => setForm(prev => ({ ...prev, filename: e.target.value }))}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-green-500"
                placeholder={form.name ? `${form.name}.sh` : 'e.g. deploy.sh'}
              />
              <p className="text-[10px] text-gray-600 mt-0.5">Defaults to name.sh if empty</p>
            </div>

            {/* Content */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">Script Content *</label>
              <textarea
                required
                value={form.content}
                onChange={e => setForm(prev => ({ ...prev, content: e.target.value }))}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-green-500 min-h-[200px] font-mono"
                placeholder={'#!/bin/bash\necho "Hello from script"'}
              />
              <p className="text-[10px] text-gray-600 mt-0.5">Shell script to execute on the agent-manager machine.</p>
            </div>
          </div>

          {mutation.error && (
            <p className="text-red-400 text-sm mt-3">{mutation.error.message}</p>
          )}

          <div className="flex justify-end gap-2 mt-4">
            <button
              type="button"
              onClick={closeForm}
              className="px-3 py-1.5 text-sm text-gray-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={mutation.isPending || !form.name || !form.content}
              className="flex items-center gap-1.5 px-4 py-1.5 text-sm bg-green-600 hover:bg-green-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
            >
              <Save className="w-3.5 h-3.5" />
              {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
            </button>
          </div>
        </form>
      )}

      {/* Script Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading scripts...</p>}

      <div className="space-y-3">
        {scripts.map(script => {
          const result = executionResults.get(script.id)
          return (
            <div
              key={script.id}
              className="bg-slate-900 border border-slate-800 rounded-xl p-4 hover:border-slate-700 transition-colors"
            >
              <div className="flex items-start justify-between">
                <div className="flex items-start gap-3 flex-1 min-w-0">
                  <Terminal className="w-5 h-5 text-green-400 mt-0.5 shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <h3 className="text-sm font-semibold text-white truncate">{script.name}</h3>
                      <span className="text-[10px] text-gray-500 bg-slate-800 rounded px-1.5 py-0.5 font-mono shrink-0">
                        {script.filename}
                      </span>
                      {script.isSynced && (
                        <span className="flex items-center gap-0.5 text-[10px] text-blue-400 bg-blue-500/10 border border-blue-500/20 rounded-full px-1.5 py-0.5 shrink-0">
                          <Cloud className="w-2.5 h-2.5" />
                          synced
                        </span>
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
                  <button
                    onClick={() => handleExecute(script)}
                    disabled={result && !result.completed}
                    className="flex items-center gap-1 px-2.5 py-1.5 text-sm bg-green-600 hover:bg-green-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
                    title="Run script"
                  >
                    {result && !result.completed ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Play className="w-3.5 h-3.5" />
                    )}
                    Run
                  </button>
                  <button
                    onClick={() => openEdit(script)}
                    className="p-1.5 text-gray-500 hover:text-green-400 transition-colors rounded-lg hover:bg-slate-800"
                    title="Edit"
                  >
                    <Edit2 className="w-3.5 h-3.5" />
                  </button>
                  <button
                    onClick={() => handleDelete(script.id)}
                    disabled={deleteMut.isPending}
                    className="p-1.5 text-gray-500 hover:text-red-400 transition-colors rounded-lg hover:bg-slate-800 disabled:opacity-50"
                    title="Delete"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>

              {/* Execution Result */}
              {result && result.completed && (
                <div className={`mt-3 border rounded-lg p-3 ${
                  result.success
                    ? 'border-green-500/20 bg-green-500/5'
                    : 'border-red-500/20 bg-red-500/5'
                }`}>
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      {result.success ? (
                        <CheckCircle className="w-4 h-4 text-green-400" />
                      ) : (
                        <XCircle className="w-4 h-4 text-red-400" />
                      )}
                      <span className={`text-sm font-medium ${result.success ? 'text-green-400' : 'text-red-400'}`}>
                        {result.success ? 'Success' : 'Failed'}
                        {result.exitCode !== undefined && ` (exit code: ${result.exitCode})`}
                      </span>
                    </div>
                    <button
                      onClick={() => clearResult(script.id)}
                      className="text-gray-500 hover:text-gray-300 transition-colors"
                    >
                      <X className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  {result.errorMessage && (
                    <div className="text-xs text-red-400 font-mono bg-slate-900/50 rounded p-2 mb-2 whitespace-pre-wrap">
                      {result.errorMessage}
                    </div>
                  )}
                  {result.stdout && (
                    <div className="mb-2">
                      <span className="text-[10px] text-gray-500 uppercase tracking-wider">stdout</span>
                      <pre className="text-xs text-gray-300 font-mono bg-slate-900/50 rounded p-2 mt-0.5 whitespace-pre-wrap max-h-[200px] overflow-y-auto">
                        {result.stdout}
                      </pre>
                    </div>
                  )}
                  {result.stderr && (
                    <div>
                      <span className="text-[10px] text-gray-500 uppercase tracking-wider">stderr</span>
                      <pre className="text-xs text-red-300 font-mono bg-slate-900/50 rounded p-2 mt-0.5 whitespace-pre-wrap max-h-[200px] overflow-y-auto">
                        {result.stderr}
                      </pre>
                    </div>
                  )}
                </div>
              )}

              {/* Execution Pending */}
              {result && !result.completed && (
                <div className="mt-3 border border-blue-500/20 bg-blue-500/5 rounded-lg p-3">
                  <div className="flex items-center gap-2">
                    <Loader2 className="w-4 h-4 text-blue-400 animate-spin" />
                    <span className="text-sm text-blue-400">Executing script...</span>
                  </div>
                </div>
              )}
            </div>
          )
        })}

        {!isLoading && scripts.length === 0 && !formMode && (
          <div className="text-center py-12 text-gray-500">
            <Terminal className="w-8 h-8 mx-auto mb-3 opacity-30" />
            <p className="text-sm">No scripts defined yet.</p>
            <p className="text-xs mt-1">Create scripts or sync from your repository's .claude/scripts/ directory.</p>
          </div>
        )}
      </div>
    </div>
  )
}
