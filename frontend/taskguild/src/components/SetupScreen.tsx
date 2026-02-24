import { useState } from 'react'
import { FolderKanban, Server, Key } from 'lucide-react'
import { useConfig } from './ConfigProvider'
import { getDefaultConfig } from '@/lib/config'

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
    <div className="min-h-screen bg-slate-950 flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        <div className="flex items-center justify-center gap-3 mb-8">
          <FolderKanban className="w-10 h-10 text-cyan-400" />
          <h1 className="text-3xl font-bold text-white">TaskGuild</h1>
        </div>

        <form
          onSubmit={handleSubmit}
          className="bg-slate-900 border border-slate-800 rounded-xl p-6 space-y-5"
        >
          <div className="text-center">
            <h2 className="text-lg font-semibold text-white mb-1">
              Configure Connection
            </h2>
            <p className="text-sm text-gray-400">
              Enter your TaskGuild server URL and API key to get started.
            </p>
          </div>

          <div>
            <label className="flex items-center gap-1.5 text-sm text-gray-400 mb-1.5">
              <Server className="w-3.5 h-3.5" />
              API Base URL
            </label>
            <input
              type="url"
              required
              value={apiBaseUrl}
              onChange={(e) => setApiBaseUrl(e.target.value)}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500 transition-colors"
              placeholder="http://localhost:3100"
            />
          </div>

          <div>
            <label className="flex items-center gap-1.5 text-sm text-gray-400 mb-1.5">
              <Key className="w-3.5 h-3.5" />
              API Key
            </label>
            <input
              type="password"
              required
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white text-sm focus:outline-none focus:border-cyan-500 transition-colors"
              placeholder="Enter your API key"
            />
          </div>

          <button
            type="submit"
            disabled={!apiBaseUrl || !apiKey}
            className="w-full py-2.5 text-sm font-medium bg-cyan-600 hover:bg-cyan-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg transition-colors"
          >
            Connect
          </button>
        </form>
      </div>
    </div>
  )
}
