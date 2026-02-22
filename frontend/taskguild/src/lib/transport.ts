import { createConnectTransport } from "@connectrpc/connect-web"

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:3100"
const API_KEY = import.meta.env.VITE_API_KEY ?? ""

export const transport = createConnectTransport({
  baseUrl: API_BASE_URL,
  interceptors: [
    (next) => async (req) => {
      if (API_KEY) {
        req.header.set("X-API-Key", API_KEY)
      }
      return next(req)
    },
  ],
})
