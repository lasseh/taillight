import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd())
  const apiTarget = env.VITE_API_TARGET || 'http://localhost:8080'

  return {
    plugins: [vue(), tailwindcss()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    test: {
      // Global env stays 'node' (fast for pure-logic tests). DOM-dependent tests
      // (e.g. highlighter/DOMPurify) opt in per-file with `// @vitest-environment jsdom`.
      environment: 'node',
      coverage: {
        provider: 'v8',
        reporter: ['text', 'html'],
        include: ['src/**'],
        exclude: ['src/**/*.d.ts', 'src/**/__tests__/**', 'src/types/**'],
        // No thresholds on purpose — coverage is reported, not gated.
      },
    },
    server: {
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
        },
        '/health': {
          target: apiTarget,
          changeOrigin: true,
        },
      },
    },
  }
})
