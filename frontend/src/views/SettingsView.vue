<script setup lang="ts">
import { ref, computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { api, ApiError } from '@/lib/api'

const auth = useAuthStore()

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
    await api.updatePassword(auth.user.id, newPassword.value)
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

        <!-- Change password -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-xs font-semibold uppercase tracking-wide">
            Change Password
          </h3>
          <div class="space-y-4 p-5">
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
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="saving"
                @click="changePassword"
              >
                {{ saving ? 'updating...' : 'update password' }}
              </button>
              <span v-if="passwordError" class="text-t-red text-sm">{{ passwordError }}</span>
              <span v-if="passwordSuccess" class="text-t-green text-sm">{{ passwordSuccess }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
