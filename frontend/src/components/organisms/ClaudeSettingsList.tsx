import { useState, useEffect } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { getClaudeSettings, updateClaudeSettings, syncClaudeSettingsFromDir } from '@taskguild/proto/taskguild/v1/claude_settings-ClaudeSettingsService_connectquery.ts'
import { Settings, Save, RefreshCw } from 'lucide-react'
import { Button, Input } from '../atoms/index.ts'
import { Card } from '../molecules/index.ts'

export function ClaudeSettingsList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(getClaudeSettings, { projectId })
  const updateMut = useMutation(updateClaudeSettings)
  const syncMut = useMutation(syncClaudeSettingsFromDir)

  const [language, setLanguage] = useState('')
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (data?.settings) {
      setLanguage(data.settings.language ?? '')
      setDirty(false)
    }
  }, [data])

  const handleLanguageChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setLanguage(e.target.value)
    setDirty(true)
  }

  const handleSave = () => {
    updateMut.mutate(
      { projectId, language },
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
      { projectId, directory: '.' },
      { onSuccess: () => refetch() },
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-violet-500/10 rounded-lg">
            <Settings className="w-5 h-5 text-violet-400" />
          </div>
          <div>
            <h1 className="text-lg font-semibold text-white">Claude Settings</h1>
            <p className="text-xs text-gray-500">
              Configure .claude/settings.json for this project
              {dirty && <span className="text-amber-400 ml-2">* unsaved changes</span>}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            icon={<RefreshCw className={`w-4 h-4 ${syncMut.isPending ? 'animate-spin' : ''}`} />}
            onClick={handleSync}
            disabled={syncMut.isPending}
            title="Sync settings from .claude/settings.json"
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
          Synced settings from .claude/settings.json
        </Card>
      )}

      {isLoading && (
        <p className="text-gray-400 text-sm">Loading settings...</p>
      )}

      {!isLoading && (
        <Card className="space-y-4">
          <div>
            <label htmlFor="language" className="block text-sm font-medium text-gray-300 mb-1.5">
              Language
            </label>
            <Input
              id="language"
              value={language}
              onChange={handleLanguageChange}
              placeholder="e.g. Japanese, English, etc."
            />
            <p className="text-xs text-gray-500 mt-1">
              The language Claude should use when communicating.
            </p>
          </div>
        </Card>
      )}
    </div>
  )
}
