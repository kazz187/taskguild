const STORAGE_KEY = "taskguild-config"

export interface AppConfig {
  apiBaseUrl: string
  apiKey: string
}

const defaultConfig: AppConfig = {
  apiBaseUrl: import.meta.env.VITE_API_BASE_URL ?? "http://localhost:3100",
  apiKey: import.meta.env.VITE_API_KEY ?? "",
}

export function getConfig(): AppConfig | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<AppConfig>
    if (!parsed.apiBaseUrl) return null
    return {
      apiBaseUrl: parsed.apiBaseUrl,
      apiKey: parsed.apiKey ?? "",
    }
  } catch {
    return null
  }
}

export function saveConfig(config: AppConfig): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(config))
}

export function hasConfig(): boolean {
  return getConfig() !== null
}

export function getEffectiveConfig(): AppConfig {
  return getConfig() ?? defaultConfig
}

export function getDefaultConfig(): AppConfig {
  return { ...defaultConfig }
}
