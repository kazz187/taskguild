import { useState } from 'react'
import { Settings, X } from 'lucide-react'
import { useConfig } from './ConfigProvider.tsx'
import { Button, Input } from '../atoms/index.ts'
import { FormField } from '../molecules/index.ts'

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

      <FormField label="URL" labelSize="xs">
        <Input
          type="url"
          required
          inputSize="xs"
          value={apiBaseUrl}
          onChange={(e) => setApiBaseUrl(e.target.value)}
          className="rounded"
        />
      </FormField>

      <FormField label="API Key" labelSize="xs">
        <Input
          type="password"
          required
          inputSize="xs"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          className="rounded"
        />
      </FormField>

      <Button
        type="submit"
        variant="primary"
        size="xs"
        disabled={!apiBaseUrl || !apiKey}
        className="w-full font-medium"
      >
        {saved ? 'Saved!' : 'Save'}
      </Button>
    </form>
  )
}
