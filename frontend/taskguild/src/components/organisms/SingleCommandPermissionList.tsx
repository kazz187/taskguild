import { useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listSingleCommandPermissions,
  createSingleCommandPermission,
  updateSingleCommandPermission,
  deleteSingleCommandPermission,
} from '@taskguild/proto/taskguild/v1/single_command_permission-SingleCommandPermissionService_connectquery.ts'
import type { SingleCommandPermission } from '@taskguild/proto/taskguild/v1/single_command_permission_pb.ts'
import { Terminal, Plus, Trash2, Edit2, X, Save, Check } from 'lucide-react'
import { Button, Input, Select, Badge } from '../atoms/index.ts'
import { Card, FormField } from '../molecules/index.ts'

type PermissionType = 'command' | 'redirect'

interface FormData {
  pattern: string
  type: PermissionType
  label: string
}

const emptyForm: FormData = {
  pattern: '',
  type: 'command',
  label: '',
}

export function SingleCommandPermissionList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(listSingleCommandPermissions, { projectId })
  const createMut = useMutation(createSingleCommandPermission)
  const updateMut = useMutation(updateSingleCommandPermission)
  const deleteMut = useMutation(deleteSingleCommandPermission)

  const [showAddForm, setShowAddForm] = useState(false)
  const [form, setForm] = useState<FormData>(emptyForm)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<FormData>(emptyForm)
  const [validationError, setValidationError] = useState<string | null>(null)

  const permissions = data?.permissions ?? []

  const validatePattern = (pattern: string): string | null => {
    if (!pattern.trim()) return 'Pattern is required'
    try {
      new RegExp(pattern)
    } catch {
      return 'Invalid regex pattern'
    }
    return null
  }

  const checkDuplicate = (pattern: string, excludeId?: string): boolean => {
    return permissions.some(p => p.pattern === pattern && p.id !== excludeId)
  }

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault()
    const error = validatePattern(form.pattern)
    if (error) {
      setValidationError(error)
      return
    }
    if (checkDuplicate(form.pattern)) {
      setValidationError('A rule with this pattern already exists')
      return
    }
    setValidationError(null)
    createMut.mutate(
      { projectId, pattern: form.pattern, type: form.type, label: form.label },
      {
        onSuccess: () => {
          setForm(emptyForm)
          setShowAddForm(false)
          refetch()
        },
      },
    )
  }

  const openEdit = (p: SingleCommandPermission) => {
    setEditingId(p.id)
    setEditForm({ pattern: p.pattern, type: p.type as PermissionType, label: p.label })
    setValidationError(null)
  }

  const cancelEdit = () => {
    setEditingId(null)
    setEditForm(emptyForm)
    setValidationError(null)
  }

  const handleUpdate = (id: string) => {
    const error = validatePattern(editForm.pattern)
    if (error) {
      setValidationError(error)
      return
    }
    if (checkDuplicate(editForm.pattern, id)) {
      setValidationError('A rule with this pattern already exists')
      return
    }
    setValidationError(null)
    updateMut.mutate(
      { id, pattern: editForm.pattern, type: editForm.type, label: editForm.label },
      {
        onSuccess: () => {
          cancelEdit()
          refetch()
        },
      },
    )
  }

  const handleDelete = (id: string) => {
    if (!confirm('Delete this permission rule?')) return
    deleteMut.mutate({ id }, { onSuccess: () => refetch() })
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-purple-500/10 rounded-lg">
            <Terminal className="w-5 h-5 text-purple-400" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-white">Single Command Allow List</h2>
            <p className="text-xs text-gray-500">
              {permissions.length} rule{permissions.length !== 1 ? 's' : ''} defined
            </p>
          </div>
        </div>
        <Button
          variant="primary"
          size="sm"
          onClick={() => { setShowAddForm(true); setValidationError(null) }}
          icon={<Plus className="w-4 h-4" />}
          className="bg-purple-600 hover:bg-purple-500"
          disabled={showAddForm}
        >
          Add Rule
        </Button>
      </div>

      {/* Add Rule Form */}
      {showAddForm && (
        <form onSubmit={handleCreate}>
          <Card className="p-4">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-white">New Rule</h3>
              <Button
                variant="ghost"
                size="sm"
                iconOnly
                onClick={() => { setShowAddForm(false); setForm(emptyForm); setValidationError(null) }}
                type="button"
                icon={<X className="w-4 h-4" />}
              />
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <FormField label="Pattern *" hint="Regex pattern (e.g. ^git\\s+status$)">
                <Input
                  type="text"
                  required
                  value={form.pattern}
                  onChange={e => { setForm(prev => ({ ...prev, pattern: e.target.value })); setValidationError(null) }}
                  placeholder="^git\\s+.*$"
                  className="focus:border-purple-500 font-mono text-sm"
                />
              </FormField>
              <FormField label="Type">
                <Select
                  value={form.type}
                  onChange={e => setForm(prev => ({ ...prev, type: e.target.value as PermissionType }))}
                  selectSize="md"
                >
                  <option value="command">command</option>
                  <option value="redirect">redirect</option>
                </Select>
              </FormField>
              <FormField label="Label" hint="Human-readable description">
                <Input
                  type="text"
                  value={form.label}
                  onChange={e => setForm(prev => ({ ...prev, label: e.target.value }))}
                  placeholder="e.g. git status"
                  className="focus:border-purple-500"
                />
              </FormField>
            </div>

            {validationError && !editingId && (
              <p className="text-red-400 text-xs mt-2">{validationError}</p>
            )}
            {createMut.error && (
              <p className="text-red-400 text-xs mt-2">{createMut.error.message}</p>
            )}

            <div className="flex justify-end gap-2 mt-3">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => { setShowAddForm(false); setForm(emptyForm); setValidationError(null) }}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                variant="primary"
                size="sm"
                disabled={createMut.isPending || !form.pattern.trim()}
                icon={<Plus className="w-3.5 h-3.5" />}
                className="bg-purple-600 hover:bg-purple-500"
              >
                {createMut.isPending ? 'Adding...' : 'Add'}
              </Button>
            </div>
          </Card>
        </form>
      )}

      {/* Loading */}
      {isLoading && <p className="text-gray-400 text-sm">Loading rules...</p>}

      {/* Rules List */}
      {!isLoading && permissions.length > 0 && (
        <div className="space-y-2">
          {permissions.map(perm => (
            <Card key={perm.id} className="hover:border-slate-700 transition-colors">
              {editingId === perm.id ? (
                /* Inline Edit Mode */
                <div>
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                    <FormField label="Pattern *">
                      <Input
                        type="text"
                        value={editForm.pattern}
                        onChange={e => { setEditForm(prev => ({ ...prev, pattern: e.target.value })); setValidationError(null) }}
                        placeholder="^git\\s+.*$"
                        className="focus:border-purple-500 font-mono text-sm"
                      />
                    </FormField>
                    <FormField label="Type">
                      <Select
                        value={editForm.type}
                        onChange={e => setEditForm(prev => ({ ...prev, type: e.target.value as PermissionType }))}
                        selectSize="md"
                      >
                        <option value="command">command</option>
                        <option value="redirect">redirect</option>
                      </Select>
                    </FormField>
                    <FormField label="Label">
                      <Input
                        type="text"
                        value={editForm.label}
                        onChange={e => setEditForm(prev => ({ ...prev, label: e.target.value }))}
                        placeholder="e.g. git status"
                        className="focus:border-purple-500"
                      />
                    </FormField>
                  </div>
                  {validationError && editingId && (
                    <p className="text-red-400 text-xs mt-2">{validationError}</p>
                  )}
                  {updateMut.error && (
                    <p className="text-red-400 text-xs mt-2">{updateMut.error.message}</p>
                  )}
                  <div className="flex justify-end gap-2 mt-3">
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={cancelEdit}
                    >
                      Cancel
                    </Button>
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={() => handleUpdate(perm.id)}
                      disabled={updateMut.isPending || !editForm.pattern.trim()}
                      icon={<Check className="w-3.5 h-3.5" />}
                      className="bg-purple-600 hover:bg-purple-500"
                    >
                      {updateMut.isPending ? 'Saving...' : 'Save'}
                    </Button>
                  </div>
                </div>
              ) : (
                /* Display Mode */
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 flex-1 min-w-0">
                    <Terminal className="w-4 h-4 text-purple-400 shrink-0" />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <code className="text-sm text-white font-mono truncate">
                          {perm.pattern}
                        </code>
                        <Badge
                          color={perm.type === 'command' ? 'blue' : 'orange'}
                          size="xs"
                          variant="outline"
                          pill
                        >
                          {perm.type}
                        </Badge>
                      </div>
                      {perm.label && (
                        <p className="text-xs text-gray-400 mt-0.5">{perm.label}</p>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-1 shrink-0 ml-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      iconOnly
                      onClick={() => openEdit(perm)}
                      title="Edit"
                      className="hover:text-purple-400"
                      icon={<Edit2 className="w-3.5 h-3.5" />}
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      iconOnly
                      onClick={() => handleDelete(perm.id)}
                      disabled={deleteMut.isPending}
                      title="Delete"
                      className="hover:text-red-400"
                      icon={<Trash2 className="w-3.5 h-3.5" />}
                    />
                  </div>
                </div>
              )}
            </Card>
          ))}
        </div>
      )}

      {/* Empty State */}
      {!isLoading && permissions.length === 0 && !showAddForm && (
        <div className="text-center py-8 text-gray-500">
          <Terminal className="w-8 h-8 mx-auto mb-3 opacity-30" />
          <p className="text-sm">No single command permission rules defined yet.</p>
          <p className="text-xs mt-1 text-gray-600">
            Add regex-based rules to allow specific shell commands without confirmation.
          </p>
        </div>
      )}
    </div>
  )
}
