import { useState, useEffect } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getPermissions, updatePermissions } from '@taskguild/proto/taskguild/v1/permission-PermissionService_connectquery.ts'
import { Shield, Plus, X, Save } from 'lucide-react'

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
  badgeBg: string
  badgeBorder: string
}> = {
  allow: {
    label: 'Allow',
    description: 'Tools and commands automatically permitted without confirmation',
    bg: 'bg-green-500/10',
    border: 'border-green-500/20',
    text: 'text-green-400',
    badgeBg: 'bg-green-500/20',
    badgeBorder: 'border-green-500/30',
  },
  ask: {
    label: 'Ask',
    description: 'Tools and commands that require user confirmation before execution',
    bg: 'bg-amber-500/10',
    border: 'border-amber-500/20',
    text: 'text-amber-400',
    badgeBg: 'bg-amber-500/20',
    badgeBorder: 'border-amber-500/30',
  },
  deny: {
    label: 'Deny',
    description: 'Tools and commands that are always blocked',
    bg: 'bg-red-500/10',
    border: 'border-red-500/20',
    text: 'text-red-400',
    badgeBg: 'bg-red-500/20',
    badgeBorder: 'border-red-500/30',
  },
}

export function PermissionList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(getPermissions, { projectId })
  const updateMut = useMutation(updatePermissions)

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

  const handleAddSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    addRule(newRule, newCategory)
  }

  const totalRules = allow.length + ask.length + deny.length

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-cyan-500/10 rounded-lg">
            <Shield className="w-5 h-5 text-cyan-400" />
          </div>
          <div>
            <h1 className="text-lg font-semibold text-white">Permissions</h1>
            <p className="text-xs text-gray-500">
              {totalRules} rule{totalRules !== 1 ? 's' : ''} defined
              {dirty && <span className="text-amber-400 ml-2">* unsaved changes</span>}
            </p>
          </div>
        </div>
        <button
          onClick={handleSave}
          disabled={!dirty || updateMut.isPending}
          className="flex items-center gap-1.5 px-4 py-2 bg-cyan-600 hover:bg-cyan-500 disabled:bg-slate-700 disabled:text-gray-500 text-white text-sm font-medium rounded-lg transition-colors"
        >
          <Save className="w-3.5 h-3.5" />
          {updateMut.isPending ? 'Saving...' : 'Save'}
        </button>
      </div>

      {updateMut.error && (
        <p className="text-red-400 text-sm bg-red-500/10 border border-red-500/20 rounded-lg px-3 py-2">
          {updateMut.error.message}
        </p>
      )}

      {/* Add Rule Form */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl p-4 space-y-3">
        <h2 className="text-sm font-medium text-gray-300">Add Rule</h2>
        <form onSubmit={handleAddSubmit} className="flex gap-2">
          <input
            type="text"
            value={newRule}
            onChange={(e) => setNewRule(e.target.value)}
            placeholder='e.g. Read, Bash(git *), Bash(npm test *)'
            className="flex-1 px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm placeholder:text-gray-500 focus:outline-none focus:border-cyan-500"
          />
          <select
            value={newCategory}
            onChange={(e) => setNewCategory(e.target.value as PermissionCategory)}
            className="px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500"
          >
            <option value="allow">Allow</option>
            <option value="ask">Ask</option>
            <option value="deny">Deny</option>
          </select>
          <button
            type="submit"
            disabled={!newRule.trim()}
            className="flex items-center gap-1 px-3 py-2 bg-slate-700 hover:bg-slate-600 disabled:opacity-40 text-white text-sm rounded-lg transition-colors"
          >
            <Plus className="w-3.5 h-3.5" />
            Add
          </button>
        </form>

        {/* Quick-add presets */}
        <div>
          <p className="text-xs text-gray-500 mb-1.5">Quick add:</p>
          <div className="flex flex-wrap gap-1">
            {COMMON_RULES.map((rule) => {
              const alreadyExists = allow.includes(rule) || ask.includes(rule) || deny.includes(rule)
              return (
                <button
                  key={rule}
                  onClick={() => addRule(rule, newCategory)}
                  disabled={alreadyExists}
                  className="px-2 py-0.5 text-xs bg-slate-800 hover:bg-slate-700 disabled:opacity-30 disabled:cursor-not-allowed text-gray-400 hover:text-white border border-slate-700 rounded transition-colors"
                >
                  {rule}
                </button>
              )
            })}
          </div>
        </div>
      </div>

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
                      <span
                        key={rule}
                        className={`inline-flex items-center gap-1 px-2.5 py-1 ${config.badgeBg} ${config.text} text-xs border ${config.badgeBorder} rounded-lg`}
                      >
                        {rule}
                        <button
                          onClick={() => removeRule(category, rule)}
                          className="hover:text-white transition-colors ml-0.5"
                        >
                          <X className="w-3 h-3" />
                        </button>
                      </span>
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
