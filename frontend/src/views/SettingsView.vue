<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useNotifications } from '@/composables/useNotifications'
import { api, ApiError } from '@/lib/api'
import { features } from '@/config'
import { severityLabels } from '@/lib/constants'
import type { UserPreferences } from '@/types/auth'

const auth = useAuthStore()
const { supported: notifSupported, permission: notifPermission, enabled: notifEnabled, requestPermission, setEnabled } = useNotifications()

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordError = ref('')
const passwordSuccess = ref('')
const saving = ref(false)

const emailInput = ref(auth.user?.email ?? '')
const emailSaving = ref(false)
const emailError = ref('')
const emailSuccess = ref('')

const emailDirty = computed(() => emailInput.value !== (auth.user?.email ?? ''))

// --- Browser notification preferences ---
const notifSrvlogEnabled = ref(true)
const notifSrvlogSeverity = ref(2)
const notifNetlogEnabled = ref(true)
const notifNetlogSeverity = ref(2)
const notifApplogEnabled = ref(true)
const notifApplogLevels = ref<Record<string, boolean>>({
  DEBUG: false,
  INFO: false,
  WARN: false,
  ERROR: true,
  FATAL: true,
})

const notifSaving = ref(false)
const notifError = ref('')
const notifSuccess = ref('')

function loadNotifPrefs() {
  const prefs = auth.user?.preferences?.browser_notifications
  if (prefs?.srvlog) {
    notifSrvlogEnabled.value = prefs.srvlog.enabled
    notifSrvlogSeverity.value = prefs.srvlog.max_severity
  }
  if (prefs?.netlog) {
    notifNetlogEnabled.value = prefs.netlog.enabled
    notifNetlogSeverity.value = prefs.netlog.max_severity
  }
  if (prefs?.applog) {
    notifApplogEnabled.value = prefs.applog.enabled
    const levelsSet = new Set(prefs.applog.levels)
    for (const lvl of Object.keys(notifApplogLevels.value)) {
      notifApplogLevels.value[lvl] = levelsSet.has(lvl)
    }
  }
}

onMounted(loadNotifPrefs)

const notifDirty = computed(() => {
  const prefs = auth.user?.preferences?.browser_notifications
  const origSrvlog = { enabled: prefs?.srvlog?.enabled ?? true, max_severity: prefs?.srvlog?.max_severity ?? 2 }
  const origNetlog = { enabled: prefs?.netlog?.enabled ?? true, max_severity: prefs?.netlog?.max_severity ?? 2 }
  const origApplog = { enabled: prefs?.applog?.enabled ?? true, levels: prefs?.applog?.levels ?? ['ERROR', 'FATAL'] }

  if (notifSrvlogEnabled.value !== origSrvlog.enabled || notifSrvlogSeverity.value !== origSrvlog.max_severity) return true
  if (notifNetlogEnabled.value !== origNetlog.enabled || notifNetlogSeverity.value !== origNetlog.max_severity) return true
  if (notifApplogEnabled.value !== origApplog.enabled) return true

  const currentLevels = Object.entries(notifApplogLevels.value).filter(([, v]) => v).map(([k]) => k).sort()
  const origLevelsSorted = [...origApplog.levels].sort()
  if (currentLevels.length !== origLevelsSorted.length || currentLevels.some((l, i) => l !== origLevelsSorted[i])) return true

  return false
})

async function saveNotifPrefs() {
  notifError.value = ''
  notifSuccess.value = ''
  notifSaving.value = true

  const selectedLevels = Object.entries(notifApplogLevels.value).filter(([, v]) => v).map(([k]) => k)

  const preferences: UserPreferences = {
    browser_notifications: {
      srvlog: { enabled: notifSrvlogEnabled.value, max_severity: notifSrvlogSeverity.value },
      netlog: { enabled: notifNetlogEnabled.value, max_severity: notifNetlogSeverity.value },
      applog: { enabled: notifApplogEnabled.value, levels: selectedLevels },
    },
  }

  try {
    const res = await api.updatePreferences(preferences)
    auth.user = res.user
    notifSuccess.value = 'notification preferences saved'
    setTimeout(() => { notifSuccess.value = '' }, 3000)
  } catch (e) {
    notifError.value = e instanceof ApiError ? e.message : 'failed to save preferences'
  } finally {
    notifSaving.value = false
  }
}

// --- Helpers ---
function formatDate(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

async function saveEmail() {
  emailError.value = ''
  emailSuccess.value = ''
  if (!emailInput.value.trim()) {
    emailError.value = 'email is required'
    return
  }
  emailSaving.value = true
  try {
    const res = await api.updateEmail(emailInput.value.trim())
    auth.user = res.user
    emailInput.value = res.user.email ?? ''
    emailSuccess.value = 'email updated'
    setTimeout(() => { emailSuccess.value = '' }, 3000)
  } catch (e) {
    emailError.value = e instanceof ApiError ? e.message : 'Failed to update email'
  } finally {
    emailSaving.value = false
  }
}

async function changePassword() {
  passwordError.value = ''
  passwordSuccess.value = ''

  if (!currentPassword.value) {
    passwordError.value = 'current password is required'
    return
  }
  if (newPassword.value.length < 8) {
    passwordError.value = 'new password must be at least 8 characters'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = 'passwords do not match'
    return
  }

  if (!auth.user) return

  saving.value = true
  try {
    await api.updatePassword(auth.user.id, newPassword.value, currentPassword.value)
    passwordSuccess.value = 'password updated — logging out...'
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    // Backend kills all sessions on password change, so log out locally.
    setTimeout(() => auth.logout(), 1500)
  } catch (e) {
    passwordError.value = e instanceof ApiError ? e.message : 'Failed to update password'
  } finally {
    saving.value = false
  }
}

const severityOptions = Object.entries(severityLabels).map(([k, v]) => ({ value: Number(k), label: `${k} — ${v}` }))
const applogLevelOrder = ['DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-4xl space-y-5">

        <!-- Page header -->
        <div>
          <h2 class="text-t-fg text-base font-semibold">User Settings</h2>
          <p class="text-t-fg-dark mt-1 text-sm">manage your profile and credentials</p>
        </div>

        <!-- Profile header -->
        <div class="bg-t-bg-dark border-t-border rounded border p-5">
          <div class="flex items-center gap-5">
            <img
              v-if="auth.user?.gravatar_url"
              :src="auth.user.gravatar_url"
              alt="avatar"
              class="h-20 w-20 rounded border border-t-border"
            />
            <div v-else class="bg-t-bg-highlight border-t-border flex h-20 w-20 items-center justify-center rounded border text-2xl font-bold text-t-fg-dark">
              {{ auth.user?.username?.charAt(0)?.toUpperCase() }}
            </div>
            <div>
              <h2 class="text-t-fg text-base font-semibold">{{ auth.user?.username }}</h2>
              <p v-if="auth.user?.email" class="text-t-fg-dark text-sm">{{ auth.user.email }}</p>
            </div>
          </div>
        </div>

        <!-- Account details -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-xs font-semibold uppercase tracking-wide">
            Account
          </h3>
          <dl class="grid grid-cols-[auto_1fr] text-sm">
            <dt class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-right">username</dt>
            <dd class="text-t-fg border-t-border border-b px-5 py-2.5 font-mono">{{ auth.user?.username }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-right">email</dt>
            <dd class="text-t-fg border-t-border border-b px-5 py-2.5">
              <div class="flex items-center gap-2">
                <input
                  v-model="emailInput"
                  type="email"
                  placeholder="you@example.com"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue w-full max-w-sm border px-3 py-1.5 text-sm outline-none"
                />
                <button
                  v-if="emailDirty"
                  class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-3 py-1.5 text-xs transition-all"
                  :disabled="emailSaving"
                  @click="saveEmail"
                >
                  {{ emailSaving ? 'saving...' : 'save' }}
                </button>
              </div>
              <span v-if="emailError" class="text-t-red mt-1 block text-xs">{{ emailError }}</span>
              <span v-if="emailSuccess" class="text-t-green mt-1 block text-xs">{{ emailSuccess }}</span>
            </dd>

            <dt class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-right">member since</dt>
            <dd class="text-t-fg border-t-border border-b px-5 py-2.5">{{ formatDate(auth.user?.created_at) }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-right">last login</dt>
            <dd class="text-t-fg border-t-border border-b px-5 py-2.5">{{ formatDate(auth.user?.last_login_at) }}</dd>
          </dl>
        </div>

        <!-- Browser Notifications -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-xs font-semibold uppercase tracking-wide">
            Browser Notifications
          </h3>
          <div class="space-y-4 p-5">

            <!-- Master toggle + permission -->
            <div class="flex items-center justify-between">
              <div>
                <p class="text-t-fg text-sm font-medium">enable browser notifications</p>
                <p class="text-t-fg-dark text-xs">receive alerts for log events in this browser</p>
              </div>
              <div class="flex items-center gap-3">
                <button
                  v-if="notifSupported && notifPermission !== 'granted'"
                  class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-3 py-1.5 text-xs transition-all"
                  @click="requestPermission"
                >
                  allow notifications
                </button>
                <label class="relative inline-flex cursor-pointer items-center">
                  <input
                    type="checkbox"
                    :checked="notifEnabled"
                    class="peer sr-only"
                    :disabled="!notifSupported"
                    @change="setEnabled(!notifEnabled)"
                  />
                  <div class="peer h-5 w-9 rounded-full bg-t-fg-gutter after:absolute after:left-[2px] after:top-[2px] after:h-4 after:w-4 after:rounded-full after:bg-white after:transition-all peer-checked:bg-t-blue peer-checked:after:translate-x-full peer-disabled:opacity-50"></div>
                </label>
              </div>
            </div>

            <div v-if="!notifSupported" class="text-t-fg-dark text-xs">
              browser notifications are not supported in this browser
            </div>
            <div v-else-if="notifPermission === 'denied'" class="text-t-red text-xs">
              notifications are blocked — enable them in your browser settings
            </div>

            <!-- Per-log-type settings -->
            <div v-if="notifSupported && notifEnabled" class="border-t-border space-y-3 border-t pt-4">

              <!-- Srvlog -->
              <div v-if="features.srvlog" class="flex items-center gap-4">
                <label class="relative inline-flex cursor-pointer items-center">
                  <input v-model="notifSrvlogEnabled" type="checkbox" class="peer sr-only" />
                  <div class="peer h-5 w-9 rounded-full bg-t-fg-gutter after:absolute after:left-[2px] after:top-[2px] after:h-4 after:w-4 after:rounded-full after:bg-white after:transition-all peer-checked:bg-t-blue peer-checked:after:translate-x-full"></div>
                </label>
                <span class="text-t-fg w-16 text-sm font-medium">srvlog</span>
                <select
                  v-model="notifSrvlogSeverity"
                  :disabled="!notifSrvlogEnabled"
                  class="bg-t-bg border-t-border text-t-fg disabled:opacity-40 border px-2 py-1 text-xs outline-none"
                >
                  <option v-for="opt in severityOptions" :key="opt.value" :value="opt.value">
                    &le; {{ opt.label }}
                  </option>
                </select>
              </div>

              <!-- Netlog -->
              <div v-if="features.netlog" class="flex items-center gap-4">
                <label class="relative inline-flex cursor-pointer items-center">
                  <input v-model="notifNetlogEnabled" type="checkbox" class="peer sr-only" />
                  <div class="peer h-5 w-9 rounded-full bg-t-fg-gutter after:absolute after:left-[2px] after:top-[2px] after:h-4 after:w-4 after:rounded-full after:bg-white after:transition-all peer-checked:bg-t-blue peer-checked:after:translate-x-full"></div>
                </label>
                <span class="text-t-fg w-16 text-sm font-medium">netlog</span>
                <select
                  v-model="notifNetlogSeverity"
                  :disabled="!notifNetlogEnabled"
                  class="bg-t-bg border-t-border text-t-fg disabled:opacity-40 border px-2 py-1 text-xs outline-none"
                >
                  <option v-for="opt in severityOptions" :key="opt.value" :value="opt.value">
                    &le; {{ opt.label }}
                  </option>
                </select>
              </div>

              <!-- Applog -->
              <div v-if="features.applog" class="flex items-start gap-4">
                <label class="relative mt-0.5 inline-flex cursor-pointer items-center">
                  <input v-model="notifApplogEnabled" type="checkbox" class="peer sr-only" />
                  <div class="peer h-5 w-9 rounded-full bg-t-fg-gutter after:absolute after:left-[2px] after:top-[2px] after:h-4 after:w-4 after:rounded-full after:bg-white after:transition-all peer-checked:bg-t-blue peer-checked:after:translate-x-full"></div>
                </label>
                <span class="text-t-fg mt-0.5 w-16 text-sm font-medium">applog</span>
                <div class="flex flex-wrap gap-2">
                  <label
                    v-for="lvl in applogLevelOrder"
                    :key="lvl"
                    class="flex items-center gap-1 text-xs"
                    :class="notifApplogEnabled ? 'text-t-fg' : 'text-t-fg-dark opacity-40'"
                  >
                    <input
                      v-model="notifApplogLevels[lvl]"
                      type="checkbox"
                      :disabled="!notifApplogEnabled"
                      class="accent-t-blue"
                    />
                    {{ lvl }}
                  </label>
                </div>
              </div>

              <!-- Save -->
              <div class="flex items-center gap-3 pt-2">
                <button
                  v-if="notifDirty"
                  class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                  :disabled="notifSaving"
                  @click="saveNotifPrefs"
                >
                  {{ notifSaving ? 'saving...' : 'save notification preferences' }}
                </button>
                <span v-if="notifError" class="text-t-red text-sm">{{ notifError }}</span>
                <span v-if="notifSuccess" class="text-t-green text-sm">{{ notifSuccess }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- Change password -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-xs font-semibold uppercase tracking-wide">
            Change Password
          </h3>
          <form class="space-y-4 p-5" @submit.prevent="changePassword">
            <label class="block">
              <span class="text-t-fg-dark text-sm">current password</span>
              <input
                v-model="currentPassword"
                type="password"
                autocomplete="current-password"
                class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-full max-w-sm border px-3 py-2 text-sm outline-none"
              />
            </label>
            <label class="block">
              <span class="text-t-fg-dark text-sm">new password</span>
              <input
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-full max-w-sm border px-3 py-2 text-sm outline-none"
              />
            </label>
            <label class="block">
              <span class="text-t-fg-dark text-sm">confirm password</span>
              <input
                v-model="confirmPassword"
                type="password"
                autocomplete="new-password"
                class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-full max-w-sm border px-3 py-2 text-sm outline-none"
              />
            </label>

            <div class="flex items-center gap-3 pt-1">
              <button
                type="submit"
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="saving"
              >
                {{ saving ? 'updating...' : 'update password' }}
              </button>
              <span v-if="passwordError" class="text-t-red text-sm">{{ passwordError }}</span>
              <span v-if="passwordSuccess" class="text-t-green text-sm">{{ passwordSuccess }}</span>
            </div>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>
