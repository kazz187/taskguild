import { QueryClient } from "@tanstack/react-query"

// The app relies on the singleton event stream + optimistic cache updates to
// keep data fresh, so a long staleTime is safe and avoids redundant
// background refetches. `refetchOnWindowFocus` is disabled for the same
// reason: we don't need to re-hit the server when the user refocuses the tab.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      gcTime: 5 * 60_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})
