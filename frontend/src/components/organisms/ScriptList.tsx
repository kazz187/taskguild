import { useState, useCallback, useMemo } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listScripts,
  createScript,
  updateScript,
  deleteScript,
  syncScriptsFromDir,
} from '@taskguild/proto/taskguild/v1/script-ScriptService_connectquery.ts'
import {
  requestScriptComparison,
  getScriptComparison,
  resolveScriptConflict,
} from '@taskguild/proto/taskguild/v1/agent_manager-AgentManagerService_connectquery.ts'
import type { ScriptDefinition } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import type { ScriptDiff } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { ScriptDiffType, ScriptResolutionChoice } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { EventType } from '@taskguild/proto/taskguild/v1/event_pb.ts'
import { Terminal, Plus, Trash2, Edit2, X, Cloud, Play, Square, CheckCircle, XCircle, StopCircle, Loader2, Layers, Copy, AlertTriangle, Server, Monitor } from 'lucide-react'
import { useEventSubscription } from '@/hooks/useEventSubscription'
import { useTemplateIntegration } from '@/hooks/useTemplateIntegration.ts'
import { Button, Badge, MutationError } from '../atoms/index.ts'
import { Card, Modal, PageHeading, EmptyState, SyncButton } from '../molecules/index.ts'
import { emptyForm, scriptToForm, diffTypeLabel } from './ScriptListUtils'
import type { ScriptFormData } from './ScriptListUtils'
import { ScriptFormModal } from './ScriptFormModal.tsx'
import { LogOutput } from './LogOutput'
import { useScriptExecution } from './useScriptExecution'
import { SaveAsTemplateDialog } from './SaveAsTemplateDialog.tsx'
import { TemplatePickerDialog } from './TemplatePickerDialog.tsx'

export function ScriptList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(listScripts, { projectId })
  const createMut = useMutation(createScript)
  const updateMut = useMutation(updateScript)
  const deleteMut = useMutation(deleteScript)
  const syncMut = useMutation(syncScriptsFromDir)

  // Script comparison
  const requestComparisonMut = useMutation(requestScriptComparison)
  const { data: comparisonData, refetch: refetchComparison } = useQuery(getScriptComparison, { projectId })
  const resolveConflictMut = useMutation(resolveScriptConflict)

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<ScriptFormData>(emptyForm)

  // Template integration
  const { saveDialog, setSaveDialog, openSaveDialog, pickerOpen, openPicker, closePicker } = useTemplateIntegration()

  // Diff resolution dialog state
  const [diffDialog, setDiffDialog] = useState<ScriptDiff | null>(null)
  // When resolving from a Run click, store the script to execute after resolution
  const [pendingExecuteScript, setPendingExecuteScript] = useState<ScriptDefinition | null>(null)

  // Script execution hook
  const { executionResults, doExecute, handleStop, clearResult, stopMut } = useScriptExecution(projectId)

  const scripts = data?.scripts ?? []
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
      { projectId },
      {
        onSuccess: () => {
          refetch()
          // After syncing from repo, automatically trigger comparison with agent.
          requestComparisonMut.mutate({ projectId })
        },
      },
    )
  }

  const handleCreateFromTemplate = (tmpl: Template) => {
    if (!tmpl.scriptConfig) return
    closePicker()
    setFormMode('create')
    setEditingId(null)
    setForm({
      name: tmpl.scriptConfig.name,
      description: tmpl.scriptConfig.description,
      filename: tmpl.scriptConfig.filename,
      content: tmpl.scriptConfig.content,
    })
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

  const mutation = formMode === 'create' ? createMut : updateMut

  // Agent-only diffs (scripts that exist only on agent, not in server DB).
  const agentOnlyDiffs = diffs.filter(d => d.diffType === ScriptDiffType.AGENT_ONLY)

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <PageHeading icon={Terminal} title="Scripts" iconColor="text-green-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {scripts.length}
          </Badge>
          {diffs.length > 0 && (
            <Badge color="amber" size="xs" variant="outline" pill icon={<AlertTriangle className="w-2.5 h-2.5" />}>
              {diffs.length} diff{diffs.length > 1 ? 's' : ''}
            </Badge>
          )}
        </PageHeading>
        <div className="flex items-center gap-2">
          <SyncButton
            onClick={handleSync}
            isPending={syncMut.isPending || requestComparisonMut.isPending}
            title="Sync scripts from .taskguild/scripts/ directory"
          />
          <Button
            variant="ghost"
            size="sm"
            onClick={openPicker}
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

      {/* Script Form Modal */}
      {formMode && (
        <ScriptFormModal
          formMode={formMode}
          form={form}
          setForm={setForm}
          onSubmit={handleSubmit}
          onClose={closeForm}
          isPending={mutation.isPending}
          error={mutation.error}
        />
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
                    onClick={() => openSaveDialog(script)}
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
          <EmptyState
            icon={Terminal}
            message="No scripts defined yet."
            hint="Create scripts or sync from your repository's .taskguild/scripts/ directory."
          />
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

              <MutationError error={resolveConflictMut.error} />
            </div>
          )}
        </Modal.Body>
      </Modal>

      {/* Template Picker Dialog */}
      <TemplatePickerDialog
        open={pickerOpen}
        entityType="script"
        entityLabel="Script"
        icon={Terminal}
        iconColor="text-green-400"
        onSelect={handleCreateFromTemplate}
        onClose={closePicker}
      />

      {/* Save as Template Dialog */}
      <SaveAsTemplateDialog
        dialog={saveDialog}
        setDialog={setSaveDialog}
        entityType="script"
        onSaved={() => {}}
      />
    </div>
  )
}
