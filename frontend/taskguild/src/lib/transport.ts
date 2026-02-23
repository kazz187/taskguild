import { createConnectTransport } from "@connectrpc/connect-web"
import type { Transport } from "@connectrpc/connect"
import { type AppConfig, getEffectiveConfig } from "./config"

export function createAppTransport(config: AppConfig): Transport {
  return createConnectTransport({
    baseUrl: config.apiBaseUrl,
    interceptors: [
      (next) => async (req) => {
        if (config.apiKey) {
          req.header.set("X-API-Key", config.apiKey)
        }
        return next(req)
      },
    ],
  })
}

// Default transport for initial render (uses localStorage config or env defaults)
export const transport = createAppTransport(getEffectiveConfig())
