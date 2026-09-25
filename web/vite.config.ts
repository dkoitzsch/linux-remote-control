import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Dev-only proxy so the Vite dev server (http://localhost:5173) can talk to
// rc-agentd's HTTPS API (self-signed cert) without CORS or manual cert
// trust. In production the built frontend is served by rc-agentd itself
// (same origin), so this block has no effect there.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'https://localhost:8443',
        changeOrigin: true,
        secure: false,
      },
    },
  },
})
