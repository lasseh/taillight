<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api, ApiError } from '@/lib/api'
import type { ApiKeyInfo } from '@/types/auth'

const keys = ref<ApiKeyInfo[]>([])
const loading = ref(true)
const loadError = ref('')
const authDisabled = ref(false)

// Creation state
const showCreate = ref(false)
const newKeyName = ref('')
const newKeyExpires = ref('90d')
const newKeyError = ref('')
const createdKey = ref('')
const copied = ref(false)
const creating = ref(false)

const scopeOptions = [
  { label: 'ingest', value: 'ingest', desc: 'POST applog events' },
  { label: 'read', value: 'read', desc: 'read all endpoints' },
  { label: 'admin', value: 'admin', desc: 'full access' },
]
const newKeyScopes = ref<string[]>(['read'])

const expirationOptions = [
  { label: '30 days', value: '30d' },
  { label: '60 days', value: '60d' },
  { label: '90 days', value: '90d' },
  { label: '1 year', value: '1y' },
  { label: 'No expiration', value: 'never' },
]

// Revocation state
const confirmRevoke = ref<string | null>(null)
const revokeError = ref('')

const activeKeys = computed(() =>
  keys.value.filter((k) => !k.revoked_at && (!k.expires_at || new Date(k.expires_at) > new Date())),
)
const inactiveKeys = computed(() =>
  keys.value.filter((k) => k.revoked_at || (k.expires_at && new Date(k.expires_at) <= new Date())),
)

function keyStatus(k: ApiKeyInfo): 'revoked' | 'expired' {
  if (k.revoked_at) return 'revoked'
  return 'expired'
}

function formatDate(ts: string): string {
  return new Date(ts).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function timeAgo(ts: string): string {
  const seconds = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  const months = Math.floor(days / 30)
  if (months < 12) return `${months}mo ago`
  return `${Math.floor(months / 12)}y ago`
}

function expiresIn(ts: string): string {
  const diff = new Date(ts).getTime() - Date.now()
  if (diff <= 0) return 'expired'
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))
  if (days < 1) return 'today'
  if (days === 1) return 'in 1 day'
  if (days < 30) return `in ${days} days`
  const months = Math.floor(days / 30)
  if (months < 12) return `in ${months}mo`
  return `in ${Math.floor(months / 12)}y`
}

function isExpiringSoon(ts: string | undefined): boolean {
  if (!ts) return false
  const days = (new Date(ts).getTime() - Date.now()) / (1000 * 60 * 60 * 24)
  return days > 0 && days <= 30
}

function toggleScope(scope: string) {
  const idx = newKeyScopes.value.indexOf(scope)
  if (idx >= 0) {
    newKeyScopes.value.splice(idx, 1)
  } else {
    newKeyScopes.value.push(scope)
  }
}

async function fetchKeys() {
  try {
    const res = await api.listKeys()
    keys.value = res.data
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      authDisabled.value = true
    } else {
      loadError.value = e instanceof ApiError ? e.message : 'Failed to load keys'
    }
  } finally {
    loading.value = false
  }
}

function computeExpiresAt(): string | undefined {
  if (newKeyExpires.value === 'never') return undefined
  const now = Date.now()
  const days: Record<string, number> = { '30d': 30, '60d': 60, '90d': 90, '1y': 365 }
  const d = days[newKeyExpires.value]
  if (!d) return undefined
  return new Date(now + d * 86400000).toISOString()
}

async function createKey() {
  newKeyError.value = ''
  createdKey.value = ''
  copied.value = false

  const name = newKeyName.value.trim()
  if (!name) {
    newKeyError.value = 'name is required'
    return
  }
  if (newKeyScopes.value.length === 0) {
    newKeyError.value = 'at least one scope is required'
    return
  }

  creating.value = true
  try {
    const expiresAt = computeExpiresAt()
    const res = await api.createKey({ name, scopes: newKeyScopes.value, expires_at: expiresAt })
    createdKey.value = res.key
    keys.value.unshift(res.key_info)
    newKeyName.value = ''
    newKeyScopes.value = ['read']
    newKeyExpires.value = '90d'
  } catch (e) {
    newKeyError.value = e instanceof ApiError ? e.message : 'Failed to create key'
  } finally {
    creating.value = false
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(createdKey.value)
    copied.value = true
  } catch {
    copied.value = false
  }
}

function dismissCreated() {
  createdKey.value = ''
  showCreate.value = false
  copied.value = false
}

async function revokeKey(id: string) {
  revokeError.value = ''
  try {
    await api.revokeKey(id)
    await fetchKeys()
  } catch (e) {
    revokeError.value = e instanceof ApiError ? e.message : 'Failed to revoke key'
  }
  confirmRevoke.value = null
}

onMounted(fetchKeys)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-4xl space-y-5">

        <!-- Page header -->
        <div class="flex items-center justify-between">
          <div>
            <h2 class="text-t-fg text-base font-semibold">API Keys</h2>
            <p class="text-t-fg-dark mt-1 text-sm">manage keys used to authenticate with the api — <a href="/api/docs" target="_blank" class="text-t-blue hover:brightness-125">api docs</a></p>
          </div>
          <button
            v-if="!showCreate && !authDisabled"
            class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
            @click="showCreate = true"
          >
            + create key
          </button>
        </div>

        <!-- Loading / error / auth disabled -->
        <div v-if="loading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
        <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>
        <div v-else-if="authDisabled" class="bg-t-bg-dark border-t-border rounded border px-5 py-10 text-center">
          <svg class="text-t-fg-gutter mx-auto mb-3 h-8 w-8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
            <path d="M7 11V7a5 5 0 0 1 10 0v4" />
          </svg>
          <p class="text-t-fg-dark text-sm">authentication is disabled</p>
          <p class="text-t-fg-gutter mt-1 text-sm">enable auth in config.yaml to manage API keys</p>
          <code class="text-t-fg-dark bg-t-bg mt-3 inline-block rounded px-3 py-1.5 font-mono text-xs">auth_enabled: true</code>
        </div>

        <template v-else>
          <!-- Created key banner -->
          <div v-if="createdKey" class="bg-t-bg-dark border-t-border rounded border p-5">
            <div class="flex items-start gap-3">
              <svg class="text-t-yellow mt-0.5 h-5 w-5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
                <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
              </svg>
              <div class="min-w-0 flex-1">
                <p class="text-t-yellow text-sm font-semibold">copy your api key now — it won't be shown again</p>
                <div class="mt-3 flex items-center gap-2">
                  <code class="bg-t-bg border-t-border text-t-green min-w-0 flex-1 overflow-x-auto border px-3 py-2 font-mono text-sm">{{ createdKey }}</code>
                  <button
                    class="border-t-border shrink-0 border px-4 py-2 text-sm transition-all"
                    :class="copied ? 'text-t-green' : 'text-t-fg hover:brightness-125 bg-t-bg-highlight'"
                    @click="copyKey"
                  >
                    {{ copied ? 'copied' : 'copy' }}
                  </button>
                </div>
                <button class="text-t-fg-dark hover:text-t-fg mt-3 text-sm" @click="dismissCreated">dismiss</button>
              </div>
            </div>
          </div>

          <!-- Create key form -->
          <Transition name="slide">
            <div v-if="showCreate && !createdKey" class="bg-t-bg-dark border-t-border rounded border p-5">
              <h3 class="text-t-fg mb-4 text-sm font-semibold">Create a new API key</h3>
              <div class="space-y-4">
                <label class="block">
                  <span class="text-t-fg-dark text-sm">name</span>
                  <input
                    v-model="newKeyName"
                    type="text"
                    placeholder="e.g. production-ingester"
                    class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-full max-w-sm border px-3 py-2 text-sm outline-none"
                    @keydown.enter="createKey"
                  />
                </label>
                <label class="block">
                  <span class="text-t-fg-dark text-sm">scopes</span>
                  <div class="mt-1.5 flex flex-wrap gap-2">
                    <button
                      v-for="opt in scopeOptions"
                      :key="opt.value"
                      class="border px-3 py-1.5 text-sm transition-all"
                      :class="
                        newKeyScopes.includes(opt.value)
                          ? 'border-t-blue text-t-blue'
                          : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                      "
                      @click="toggleScope(opt.value)"
                    >
                      {{ opt.label }}
                      <span class="text-t-fg-gutter ml-1 text-xs">{{ opt.desc }}</span>
                    </button>
                  </div>
                </label>
                <label class="block">
                  <span class="text-t-fg-dark text-sm">expiration</span>
                  <div class="mt-1.5 flex flex-wrap gap-2">
                    <button
                      v-for="opt in expirationOptions"
                      :key="opt.value"
                      class="border px-3 py-1.5 text-sm transition-all"
                      :class="
                        newKeyExpires === opt.value
                          ? 'border-t-blue text-t-blue'
                          : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                      "
                      @click="newKeyExpires = opt.value"
                    >
                      {{ opt.label }}
                    </button>
                  </div>
                </label>
                <div class="flex items-center gap-3 pt-1">
                  <button
                    class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                    :disabled="creating"
                    @click="createKey"
                  >
                    {{ creating ? 'generating...' : 'generate key' }}
                  </button>
                  <button
                    class="text-t-fg-dark hover:text-t-fg text-sm transition-colors"
                    @click="showCreate = false; newKeyName = ''; newKeyError = ''"
                  >
                    cancel
                  </button>
                  <span v-if="newKeyError" class="text-t-red text-sm">{{ newKeyError }}</span>
                </div>
              </div>
            </div>
          </Transition>

          <!-- Revoke error -->
          <div v-if="revokeError" class="text-t-red text-sm px-5 py-2">{{ revokeError }}</div>

          <!-- Active keys -->
          <div class="bg-t-bg-dark border-t-border rounded border">
            <div class="border-t-border border-b px-5 py-2.5">
              <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
                Active Keys
                <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ activeKeys.length }}</span>
              </h3>
            </div>

            <!-- Table header -->
            <div v-if="activeKeys.length > 0" class="text-t-fg-gutter border-t-border flex border-b px-5 py-2 text-xs uppercase tracking-wider">
              <span class="w-40 shrink-0">Name</span>
              <span class="w-32 shrink-0">Key</span>
              <span class="w-36 shrink-0">Scopes</span>
              <span class="w-28 shrink-0">Created</span>
              <span class="w-28 shrink-0">Last Used</span>
              <span class="min-w-0 flex-1">Expires</span>
              <span class="w-20 shrink-0 text-right">Action</span>
            </div>

            <!-- Key rows -->
            <div class="divide-t-border divide-y">
              <div
                v-for="key in activeKeys"
                :key="key.id"
                class="hover:bg-t-bg-hover flex items-center px-5 py-3 text-sm transition-colors"
              >
                <div class="w-40 shrink-0">
                  <span class="text-t-fg font-medium">{{ key.name }}</span>
                </div>
                <div class="w-32 shrink-0">
                  <span class="text-t-teal font-mono">{{ key.key_prefix }}...</span>
                </div>
                <div class="w-36 shrink-0">
                  <div class="flex flex-wrap gap-1">
                    <span
                      v-for="scope in key.scopes"
                      :key="scope"
                      class="inline-block rounded px-1.5 py-0.5 text-xs"
                      :class="{
                        'bg-t-purple/10 text-t-purple': scope === 'admin',
                        'bg-t-blue/10 text-t-blue': scope === 'read',
                        'bg-t-green/10 text-t-green': scope === 'ingest',
                      }"
                    >
                      {{ scope }}
                    </span>
                  </div>
                </div>
                <div class="w-28 shrink-0">
                  <span class="text-t-fg-dark" :title="formatDate(key.created_at)">{{ timeAgo(key.created_at) }}</span>
                </div>
                <div class="w-28 shrink-0">
                  <span v-if="key.last_used_at" class="text-t-fg-dark" :title="formatDate(key.last_used_at)">{{ timeAgo(key.last_used_at) }}</span>
                  <span v-else class="text-t-fg-gutter">never</span>
                </div>
                <div class="min-w-0 flex-1">
                  <span v-if="!key.expires_at" class="text-t-fg-gutter">never</span>
                  <span
                    v-else
                    :class="isExpiringSoon(key.expires_at) ? 'text-t-yellow' : 'text-t-fg-dark'"
                    :title="formatDate(key.expires_at)"
                  >
                    {{ expiresIn(key.expires_at) }}
                  </span>
                </div>
                <div class="w-20 shrink-0 text-right">
                  <button
                    v-if="confirmRevoke !== key.id"
                    class="text-t-fg-dark hover:text-t-red text-sm transition-colors"
                    @click="confirmRevoke = key.id"
                  >
                    revoke
                  </button>
                  <span v-else class="flex items-center justify-end gap-2">
                    <button class="text-t-red hover:brightness-125 text-sm font-semibold" @click="revokeKey(key.id)">yes</button>
                    <button class="text-t-fg-dark hover:text-t-fg text-sm" @click="confirmRevoke = null">no</button>
                  </span>
                </div>
              </div>
            </div>

            <!-- Empty state -->
            <div v-if="activeKeys.length === 0" class="px-5 py-10 text-center">
              <svg class="text-t-fg-gutter mx-auto mb-3 h-8 w-8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
              </svg>
              <p class="text-t-fg-dark text-sm">no active api keys</p>
              <button
                v-if="!showCreate"
                class="text-t-blue mt-2 text-sm hover:brightness-125"
                @click="showCreate = true"
              >
                create your first key
              </button>
            </div>
          </div>

          <!-- Inactive keys -->
          <div v-if="inactiveKeys.length > 0" class="bg-t-bg-dark border-t-border rounded border">
            <div class="border-t-border border-b px-5 py-2.5">
              <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
                Inactive Keys
                <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ inactiveKeys.length }}</span>
              </h3>
            </div>

            <div class="divide-t-border divide-y">
              <div
                v-for="key in inactiveKeys"
                :key="key.id"
                class="flex items-center px-5 py-3 text-sm opacity-60"
              >
                <div class="w-44 shrink-0">
                  <span class="text-t-fg">{{ key.name }}</span>
                </div>
                <div class="w-36 shrink-0">
                  <span class="text-t-fg-dark font-mono">{{ key.key_prefix }}...</span>
                </div>
                <div class="w-36 shrink-0">
                  <span class="text-t-fg-dark">{{ timeAgo(key.created_at) }}</span>
                </div>
                <div class="w-36 shrink-0">
                  <span v-if="key.last_used_at" class="text-t-fg-dark">{{ timeAgo(key.last_used_at) }}</span>
                  <span v-else class="text-t-fg-gutter">never</span>
                </div>
                <div class="min-w-0 flex-1">
                  <span
                    class="inline-block rounded px-1.5 py-0.5 text-xs uppercase"
                    :class="keyStatus(key) === 'expired' ? 'bg-t-yellow/10 text-t-yellow' : 'bg-t-red/10 text-t-red'"
                  >
                    {{ keyStatus(key) }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </template>

      </div>
    </div>
  </div>
</template>

<style scoped>
.slide-enter-active,
.slide-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.slide-enter-from,
.slide-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
