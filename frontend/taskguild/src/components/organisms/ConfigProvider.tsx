import { createContext, useContext, useState, useCallback, useMemo, type ReactNode } from 'react'
import { TransportProvider } from '@connectrpc/connect-query'
import { QueryClientProvider } from '@tanstack/react-query'
import { type AppConfig, getEffectiveConfig, saveConfig, hasConfig } from '@/lib/config'
import { createAppTransport } from '@/lib/transport'
import { queryClient } from '@/lib/query-client'

interface ConfigContextValue {
  config: AppConfig
  isConfigured: boolean
  updateConfig: (config: AppConfig) => void
}

const ConfigContext = createContext<ConfigContextValue | null>(null)

export function useConfig() {
  const ctx = useContext(ConfigContext)
  if (!ctx) throw new Error('useConfig must be used within ConfigProvider')
  return ctx
}

export function ConfigProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<AppConfig>(getEffectiveConfig)
  const [configured, setConfigured] = useState(hasConfig)

  const transport = useMemo(() => createAppTransport(config), [config])

  const updateConfig = useCallback((newConfig: AppConfig) => {
    saveConfig(newConfig)
    setConfig(newConfig)
    setConfigured(true)
    // Clear all cached queries so they refetch with the new transport
    queryClient.clear()
  }, [])

  const value = useMemo<ConfigContextValue>(
    () => ({ config, isConfigured: configured, updateConfig }),
    [config, configured, updateConfig],
  )

  return (
    <ConfigContext.Provider value={value}>
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      </TransportProvider>
    </ConfigContext.Provider>
  )
}
