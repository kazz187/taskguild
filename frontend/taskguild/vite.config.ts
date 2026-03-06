import { defineConfig } from 'vite'
import { devtools } from '@tanstack/devtools-vite'
import tsconfigPaths from 'vite-tsconfig-paths'

import { tanstackRouter } from '@tanstack/router-plugin/vite'

import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

/**
 * Extract the package name from a module ID resolved under node_modules.
 * Handles pnpm's `.pnpm/<pkg>/node_modules/<pkg>` layout as well as
 * regular `node_modules/<pkg>` paths.
 */
function getPackageName(id: string): string | undefined {
  const match = id.match(
    /node_modules\/(?:\.pnpm\/[^/]+\/node_modules\/)?(@[^/]+\/[^/]+|[^/]+)/,
  )
  return match?.[1]
}

const config = defineConfig({
  plugins: [
    devtools(),
    tsconfigPaths({ projects: ['./tsconfig.json'] }),
    tailwindcss(),
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    viteReact(),
  ],
  resolve: {
    dedupe: ['@bufbuild/protobuf'],
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return

          const pkg = getPackageName(id)
          if (!pkg) return

          // React core
          if (['react', 'react-dom', 'scheduler'].includes(pkg)) {
            return 'react-vendor'
          }

          // Protocol Buffers & Connect RPC
          if (
            pkg === '@bufbuild/protobuf' ||
            pkg === '@taskguild/proto' ||
            pkg.startsWith('@connectrpc/')
          ) {
            return 'rpc'
          }

          // TanStack Router & React Query (+ internal deps)
          if (
            pkg.startsWith('@tanstack/') &&
            !pkg.includes('devtools')
          ) {
            return 'router-query'
          }

          // Drag-and-drop
          if (pkg.startsWith('@dnd-kit/')) {
            return 'dnd'
          }
        },
      },
    },
  },
})

export default config
