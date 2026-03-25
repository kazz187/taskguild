import { useState, useEffect } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getPermissions, updatePermissions, syncPermissionsFromDir } from '@taskguild/proto/taskguild/v1/permission-PermissionService_connectquery.ts'
import { Shield, Plus, X, Save, RefreshCw } from 'lucide-react'
import { Button, Input, Select, Badge } from '../atoms/index.ts'
import { Card, PageHeading } from '../molecules/index.ts'

type PermissionCategory = 'allow' | 'ask' | 'deny'

const COMMON_RULES = [
  'Read', 'Write', 'Edit', 'Glob', 'Grep',
  'Bash(git *)', 'Bash(npm test *)', 'Bash(npm install *)',
  'Bash(npm run *)', 'WebSearch', 'WebFetch', 'Task', 'NotebookEdit',
]

const CATEGORY_CONFIG: Record<PermissionCategory, {
  label: string
  description: string
  bg: string
  border: string
  text: string
  badgeColor: 'green' | 'amber' | 'red'
}> = {
  allow: {
    label: 'Allow',
    description: 'Tools and commands automatically permitted without confirmation',
    bg: 'bg-green-500/10',
    border: 'border-green-500/20',
    text: 'text-green-400',
    badgeColor: 'green',
  },
  ask: {
    label: 'Ask',
    description: 'Tools and commands that require user confirmation before execution',
    bg: 'bg-amber-500/10',
    border: 'border-amber-500/20',
    text: 'text-amber-400',
    badgeColor: 'amber',
  },
  deny: {
    label: 'Deny',
    description: 'Tools and commands that are always blocked',
    bg: 'bg-red-500/10',
    border: 'border-red-500/20',
    text: 'text-red-400',
    badgeColor: 'red',
  },
}

export function PermissionList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(getPermissions, { projectId })
  const updateMut = useMutation(updatePermissions)
  const syncMut = useMutation(syncPermissionsFromDir)

  const [allow, setAllow] = useState<string[]>([])
  const [ask, setAsk] = useState<string[]>([])
  const [deny, setDeny] = useState<string[]>([])
  const [dirty, setDirty] = useState(false)
  const [newRule, setNewRule] = useState('')
  const [newCategory, setNewCategory] = useState<PermissionCategory>('allow')

  // Sync from server data.
  useEffect(() => {
    if (data?.permissions) {
      setAllow([...(data.permissions.allow ?? [])])
      setAsk([...(data.permissions.ask ?? [])])
      setDeny([...(data.permissions.deny ?? [])])
      setDirty(false)
    }
  }, [data])

  const getList = (cat: PermissionCategory) => {
    switch (cat) {
      case 'allow': return allow
      case 'ask': return ask
      case 'deny': return deny
    }
  }

  const setList = (cat: PermissionCategory, rules: string[]) => {
    switch (cat) {
      case 'allow': setAllow(rules); break
      case 'ask': setAsk(rules); break
      case 'deny': setDeny(rules); break
    }
    setDirty(true)
  }

  const addRule = (rule: string, category: PermissionCategory) => {
    const trimmed = rule.trim()
    if (!trimmed) return
    const list = getList(category)
    if (list.includes(trimmed)) return
    setList(category, [...list, trimmed])
    setNewRule('')
  }

  const removeRule = (category: PermissionCategory, rule: string) => {
    const list = getList(category)
    setList(category, list.filter(r => r !== rule))
  }

  const handleSave = () => {
    updateMut.mutate(
      { projectId, allow, ask, deny },
      {
        onSuccess: () => {
          refetch()
          setDirty(false)
        },
      },
    )
  }

  const handleSync = () => {
    syncMut.mutate(
      { projectId },
      { onSuccess: () => refetch() },
    )
  }

  const handleAddSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    addRule(newRule, newCategory)
  }

  const totalRules = allow.length + ask.length + deny.length

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <PageHeading icon={Shield} title="Permissions" iconColor="text-cyan-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {totalRules}
          </Badge>
          {dirty && (
            <Badge color="amber" size="xs" pill variant="outline">
              unsaved
            </Badge>
          )}
        </PageHeading>
        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            icon={<RefreshCw className={`w-4 h-4 ${syncMut.isPending ? 'animate-spin' : ''}`} />}
            onClick={handleSync}
            disabled={syncMut.isPending}
            title="Sync permissions from .claude/settings.json"
            className="border border-slate-700 hover:border-slate-600"
          >
            <span className="hidden sm:inline">Sync from Repo</span>
            <span className="sm:hidden">Sync</span>
          </Button>
          <Button
            variant="primary"
            size="md"
            icon={<Save className="w-3.5 h-3.5" />}
            onClick={handleSave}
            disabled={!dirty || updateMut.isPending}
            className="font-medium"
          >
            {updateMut.isPending ? 'Saving...' : 'Save'}
          </Button>
        </div>
      </div>

      {updateMut.error && (
        <Card variant="error" className="text-sm">
          {updateMut.error.message}
        </Card>
      )}

      {syncMut.error && (
        <Card variant="error" className="text-sm">
          {syncMut.error.message}
        </Card>
      )}

      {syncMut.isSuccess && (
        <Card variant="success" className="text-sm">
          Synced permissions from .claude/settings.json
        </Card>
      )}

      {/* Add Rule Form */}
      <Card className="space-y-3">
        <h2 className="text-sm font-medium text-gray-300">Add Rule</h2>
        <form onSubmit={handleAddSubmit} className="flex gap-2">
          <Input
            value={newRule}
            onChange={(e) => setNewRule(e.target.value)}
            placeholder='e.g. Read, Bash(git *), Bash(npm test *)'
            className="flex-1 min-w-0"
          />
          <div className="shrink-0">
            <Select
              selectSize="md"
              value={newCategory}
              onChange={(e) => setNewCategory(e.target.value as PermissionCategory)}
            >
              <option value="allow">Allow</option>
              <option value="ask">Ask</option>
              <option value="deny">Deny</option>
            </Select>
          </div>
          <Button
            type="submit"
            variant="secondary"
            size="md"
            icon={<Plus className="w-3.5 h-3.5" />}
            disabled={!newRule.trim()}
            className="bg-slate-700 hover:bg-slate-600 shrink-0"
          >
            Add
          </Button>
        </form>

        {/* Quick-add presets */}
        <div>
          <p className="text-xs text-gray-500 mb-1.5">Quick add:</p>
          <div className="flex flex-wrap gap-1">
            {COMMON_RULES.map((rule) => {
              const alreadyExists = allow.includes(rule) || ask.includes(rule) || deny.includes(rule)
              return (
                <Button
                  key={rule}
                  variant="ghost"
                  size="xs"
                  onClick={() => addRule(rule, newCategory)}
                  disabled={alreadyExists}
                  className="bg-slate-800 text-gray-400 hover:text-white border border-slate-700"
                >
                  {rule}
                </Button>
              )
            })}
          </div>
        </div>
      </Card>

      {/* Loading */}
      {isLoading && (
        <p className="text-gray-400 text-sm">Loading permissions...</p>
      )}

      {/* Permission Categories */}
      {!isLoading && (
        <div className="space-y-4">
          {(['allow', 'ask', 'deny'] as PermissionCategory[]).map((category) => {
            const config = CATEGORY_CONFIG[category]
            const rules = getList(category)
            return (
              <div
                key={category}
                className={`${config.bg} border ${config.border} rounded-xl p-4`}
              >
                <div className="flex items-center justify-between mb-2">
                  <div>
                    <h3 className={`text-sm font-semibold ${config.text}`}>
                      {config.label}
                      <span className="text-gray-500 font-normal ml-2">
                        ({rules.length})
                      </span>
                    </h3>
                    <p className="text-xs text-gray-500 mt-0.5">
                      {config.description}
                    </p>
                  </div>
                </div>
                {rules.length === 0 ? (
                  <p className="text-xs text-gray-600 italic">No rules defined</p>
                ) : (
                  <div className="flex flex-wrap gap-1.5 mt-2">
                    {rules.map((rule) => (
                      <Badge
                        key={rule}
                        color={config.badgeColor}
                        size="sm"
                        variant="outline"
                        className="rounded-lg"
                      >
                        {rule}
                        <button
                          onClick={() => removeRule(category, rule)}
                          className="hover:text-white transition-colors ml-0.5"
                        >
                          <X className="w-3 h-3" />
                        </button>
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {/* Empty State */}
      {!isLoading && totalRules === 0 && (
        <div className="text-center py-8 text-gray-500">
          <Shield className="w-8 h-8 mx-auto mb-3 opacity-30" />
          <p className="text-sm">No permission rules defined yet.</p>
          <p className="text-xs mt-1 text-gray-600">
            Add rules above to control which tools and commands are allowed, require confirmation, or are denied.
          </p>
        </div>
      )}
    </div>
  )
}
