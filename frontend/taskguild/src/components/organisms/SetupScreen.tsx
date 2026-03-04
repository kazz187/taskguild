import { useState } from 'react'
import { FolderKanban } from 'lucide-react'
import { useConfig } from './ConfigProvider.tsx'
import { getDefaultConfig } from '@/lib/config'
import { Button, Input } from '../atoms/index.ts'
import { Card, FormField } from '../molecules/index.ts'

export function SetupScreen() {
  const { updateConfig } = useConfig()
  const defaults = getDefaultConfig()
  const [apiBaseUrl, setApiBaseUrl] = useState(defaults.apiBaseUrl)
  const [apiKey, setApiKey] = useState(defaults.apiKey)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    updateConfig({ apiBaseUrl: apiBaseUrl.replace(/\/+$/, ''), apiKey })
  }

  return (
    <div className="min-h-dvh bg-slate-950 flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        <div className="flex items-center justify-center gap-3 mb-8">
          <FolderKanban className="w-10 h-10 text-cyan-400" />
          <h1 className="text-3xl font-bold text-white">TaskGuild</h1>
        </div>

        <Card>
          <form onSubmit={handleSubmit} className="space-y-5">
            <div className="text-center">
              <h2 className="text-lg font-semibold text-white mb-1">
                Configure Connection
              </h2>
              <p className="text-sm text-gray-400">
                Enter your TaskGuild server URL and API key to get started.
              </p>
            </div>

            <FormField
              label="API Base URL"
              labelSize="sm"
            >
              <Input
                type="url"
                required
                value={apiBaseUrl}
                onChange={(e) => setApiBaseUrl(e.target.value)}
                placeholder="http://localhost:3100"
              />
            </FormField>

            <FormField
              label="API Key"
              labelSize="sm"
            >
              <Input
                type="password"
                required
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="Enter your API key"
              />
            </FormField>

            <Button
              type="submit"
              variant="primary"
              size="md"
              disabled={!apiBaseUrl || !apiKey}
              className="w-full font-medium"
            >
              Connect
            </Button>
          </form>
        </Card>
      </div>
    </div>
  )
}
