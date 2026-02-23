import { useState } from 'react'
import { Settings, X, Server, Key } from 'lucide-react'
import { useConfig } from './ConfigProvider'

export function SidebarConfig() {
  const { config, updateConfig } = useConfig()
  const [open, setOpen] = useState(false)
  const [apiBaseUrl, setApiBaseUrl] = useState(config.apiBaseUrl)
  const [apiKey, setApiKey] = useState(config.apiKey)
  const [saved, setSaved] = useState(false)

  const handleOpen = () => {
    setApiBaseUrl(config.apiBaseUrl)
    setApiKey(config.apiKey)
    setSaved(false)
    setOpen(true)
  }

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault()
    updateConfig({ apiBaseUrl: apiBaseUrl.replace(/\/+$/, ''), apiKey })
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  if (!open) {
    return (
      <button
        onClick={handleOpen}
        className="w-full flex items-center gap-2 px-3 py-2 text-xs text-gray-500 hover:text-gray-300 hover:bg-slate-800/40 rounded-lg transition-colors"
      >
        <Settings className="w-3.5 h-3.5" />
        Configuration
      </button>
    )
  }

  return (
    <form onSubmit={handleSave} className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] uppercase tracking-wider text-gray-500 font-semibold px-1">
          Configuration
        </span>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="text-gray-500 hover:text-gray-300 transition-colors"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      <div>
        <label className="flex items-center gap-1 text-[11px] text-gray-500 mb-1 px-1">
          <Server className="w-3 h-3" />
          URL
        </label>
        <input
          type="url"
          required
          value={apiBaseUrl}
          onChange={(e) => setApiBaseUrl(e.target.value)}
          className="w-full px-2 py-1 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500 transition-colors"
        />
      </div>

      <div>
        <label className="flex items-center gap-1 text-[11px] text-gray-500 mb-1 px-1">
          <Key className="w-3 h-3" />
          API Key
        </label>
        <input
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          className="w-full px-2 py-1 bg-slate-800 border border-slate-700 rounded text-white text-xs focus:outline-none focus:border-cyan-500 transition-colors"
        />
      </div>

      <button
        type="submit"
        disabled={!apiBaseUrl}
        className="w-full py-1 text-xs font-medium bg-cyan-600 hover:bg-cyan-500 disabled:opacity-50 text-white rounded transition-colors"
      >
        {saved ? 'Saved!' : 'Save'}
      </button>
    </form>
  )
}
