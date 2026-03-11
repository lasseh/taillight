// Runtime configuration for API URL.
// In production, this is injected via window.__CONFIG__ from index.html.
// Falls back to same-origin (proxy mode) if not set.

interface AppConfig {
  apiUrl: string
}

declare global {
  interface Window {
    __CONFIG__?: Partial<AppConfig>
  }
}

const defaultConfig: AppConfig = {
  apiUrl: '', // Empty = same origin (relative URLs)
}

export const config: AppConfig = {
  ...defaultConfig,
  ...window.__CONFIG__,
}
