import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import compression from 'vite-plugin-compression'

export default defineConfig({
  plugins: [
    react(),
    compression({ algorithm: 'gzip', ext: '.gz' }),
  ],

  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('@monaco-editor') || id.includes('monaco-editor')) {
              return 'vendor-monaco'
            }
            if (id.includes('y-monaco') || id.includes('y-websocket') || id.includes('yjs')) {
              return 'vendor-yjs'
            }
            if (id.includes('react-router-dom')) {
              return 'vendor-react'
            }
            if (id.includes('react-dom') || id.includes('react/')) {
              return 'vendor-react'
            }
            if (id.includes('react-resizable-panels')) {
              return 'vendor-ui'
            }
          }
        },
      },
    },
    chunkSizeWarningLimit: 600,
  },
})