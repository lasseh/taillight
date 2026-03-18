<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { api, ApiError } from '@/lib/api'
import type { AdminUser } from '@/types/auth'

const auth = useAuthStore()

const users = ref<AdminUser[]>([])
const loading = ref(true)
const error = ref('')

// Create user form
const showCreate = ref(false)
const newUsername = ref('')
const newPassword = ref('')
const newConfirm = ref('')
const newIsAdmin = ref(false)
const createError = ref('')
const creating = ref(false)

// Reset password state (keyed by user ID)
const resetId = ref<string | null>(null)
const resetPassword = ref('')
const resetConfirm = ref('')
const resetError = ref('')
const resetting = ref(false)

// Feedback
const feedback = ref('')

function formatDate(iso?: string): string {
  if (!iso) return '--'
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

async function loadUsers() {
  loading.value = true
  error.value = ''
  try {
    const res = await api.listUsers()
    users.value = res.data
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Failed to load users'
  } finally {
    loading.value = false
  }
}

async function createUser() {
  createError.value = ''
  if (!newUsername.value.trim()) {
    createError.value = 'username is required'
    return
  }
  if (newPassword.value.length < 8) {
    createError.value = 'password must be at least 8 characters'
    return
  }
  if (newPassword.value !== newConfirm.value) {
    createError.value = 'passwords do not match'
    return
  }
  creating.value = true
  try {
    const res = await api.createUser({
      username: newUsername.value.trim(),
      password: newPassword.value,
      is_admin: newIsAdmin.value,
    })
    users.value.push(res.user)
    newUsername.value = ''
    newPassword.value = ''
    newConfirm.value = ''
    newIsAdmin.value = false
    showCreate.value = false
    showFeedback('user created')
  } catch (e) {
    createError.value = e instanceof ApiError ? e.message : 'Failed to create user'
  } finally {
    creating.value = false
  }
}

async function toggleActive(u: AdminUser) {
  if (u.id === auth.user?.id) return
  try {
    await api.setUserActive(u.id, !u.is_active)
    u.is_active = !u.is_active
    showFeedback(u.is_active ? 'user activated' : 'user deactivated')
  } catch (e) {
    showFeedback(e instanceof ApiError ? e.message : 'Failed to update user', true)
  }
}

async function revokeSessionsFor(u: AdminUser) {
  try {
    await api.revokeUserSessions(u.id)
    showFeedback(`sessions revoked for ${u.username}`)
  } catch (e) {
    showFeedback(e instanceof ApiError ? e.message : 'Failed to revoke sessions', true)
  }
}

function openReset(id: string) {
  resetId.value = id
  resetPassword.value = ''
  resetConfirm.value = ''
  resetError.value = ''
}

async function submitReset() {
  resetError.value = ''
  if (resetPassword.value.length < 8) {
    resetError.value = 'password must be at least 8 characters'
    return
  }
  if (resetPassword.value !== resetConfirm.value) {
    resetError.value = 'passwords do not match'
    return
  }
  if (!resetId.value) return
  resetting.value = true
  try {
    await api.adminResetPassword(resetId.value, resetPassword.value)
    resetId.value = null
    showFeedback('password reset')
  } catch (e) {
    resetError.value = e instanceof ApiError ? e.message : 'Failed to reset password'
  } finally {
    resetting.value = false
  }
}

function showFeedback(msg: string, isError = false) {
  feedback.value = (isError ? '! ' : '') + msg
  setTimeout(() => { feedback.value = '' }, 3000)
}

onMounted(loadUsers)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-4xl space-y-5">

        <!-- Page header -->
        <div class="flex items-center justify-between">
          <div>
            <h2 class="text-t-fg text-base font-semibold">User Management</h2>
            <p class="text-t-fg-dark mt-1 text-sm">create, deactivate, and manage user accounts</p>
          </div>
          <button
            class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-3 py-1.5 text-xs transition-all"
            @click="showCreate = !showCreate"
          >
            {{ showCreate ? 'cancel' : '+ new user' }}
          </button>
        </div>

        <!-- Feedback bar -->
        <div
          v-if="feedback"
          class="text-xs px-3 py-2 rounded border"
          :class="feedback.startsWith('!') ? 'text-t-red border-t-red/30 bg-t-red/5' : 'text-t-green border-t-green/30 bg-t-green/5'"
        >
          {{ feedback.replace(/^! /, '') }}
        </div>

        <!-- Create user form -->
        <div v-if="showCreate" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-xs font-semibold uppercase tracking-wide">
            Create User
          </h3>
          <div class="space-y-4 p-5">
            <label class="block">
              <span class="text-t-fg-dark text-sm">username</span>
              <input
                v-model="newUsername"
                type="text"
                autocomplete="off"
                class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-full max-w-sm border px-3 py-2 text-sm outline-none"
                placeholder="username"
              />
            </label>
            <label class="block">
              <span class="text-t-fg-dark text-sm">password</span>
              <input
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-full max-w-sm border px-3 py-2 text-sm outline-none"
                placeholder="min 8 characters"
              />
            </label>
            <label class="block">
              <span class="text-t-fg-dark text-sm">confirm password</span>
              <input
                v-model="newConfirm"
                type="password"
                autocomplete="new-password"
                class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-full max-w-sm border px-3 py-2 text-sm outline-none"
              />
            </label>
            <label class="flex items-center gap-2">
              <input v-model="newIsAdmin" type="checkbox" class="accent-t-blue" />
              <span class="text-t-fg-dark text-sm">admin</span>
            </label>
            <div class="flex items-center gap-3 pt-1">
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="creating"
                @click="createUser"
              >
                {{ creating ? 'creating...' : 'create user' }}
              </button>
              <span v-if="createError" class="text-t-red text-sm">{{ createError }}</span>
            </div>
          </div>
        </div>

        <!-- Loading / error -->
        <div v-if="loading" class="text-t-fg-dark text-sm">Loading users...</div>
        <div v-else-if="error" class="text-t-red text-sm">{{ error }}</div>

        <!-- User table -->
        <div v-else class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-5 py-2.5 text-xs font-semibold uppercase tracking-wide">
            Users ({{ users.length }})
          </h3>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr class="text-t-fg-dark border-t-border border-b text-left text-xs uppercase tracking-wide">
                  <th class="px-5 py-2"></th>
                  <th class="px-3 py-2">Username</th>
                  <th class="px-3 py-2 hidden sm:table-cell">Email</th>
                  <th class="px-3 py-2">Role</th>
                  <th class="px-3 py-2">Status</th>
                  <th class="px-3 py-2 hidden md:table-cell">Last Login</th>
                  <th class="px-3 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="u in users"
                  :key="u.id"
                  class="border-t-border border-b last:border-b-0"
                >
                  <!-- Avatar -->
                  <td class="px-5 py-2.5">
                    <img
                      v-if="u.gravatar_url"
                      :src="u.gravatar_url"
                      alt=""
                      class="h-7 w-7 rounded-full border border-t-border"
                    />
                    <div v-else class="bg-t-bg-highlight border-t-border flex h-7 w-7 items-center justify-center rounded-full border text-xs font-bold text-t-fg-dark">
                      {{ u.username.charAt(0).toUpperCase() }}
                    </div>
                  </td>

                  <!-- Username -->
                  <td class="px-3 py-2.5 text-t-fg font-mono">{{ u.username }}</td>

                  <!-- Email -->
                  <td class="px-3 py-2.5 text-t-fg-dark hidden sm:table-cell">{{ u.email || '--' }}</td>

                  <!-- Role -->
                  <td class="px-3 py-2.5">
                    <span
                      v-if="u.is_admin"
                      class="text-t-yellow border-t-yellow/30 bg-t-yellow/10 rounded border px-1.5 py-0.5 text-[10px] font-semibold uppercase"
                    >admin</span>
                    <span v-else class="text-t-fg-dark text-xs">user</span>
                  </td>

                  <!-- Status -->
                  <td class="px-3 py-2.5">
                    <button
                      class="rounded border px-1.5 py-0.5 text-[10px] font-semibold uppercase transition-colors"
                      :class="
                        u.is_active
                          ? 'text-t-green border-t-green/30 bg-t-green/10 hover:bg-t-green/20'
                          : 'text-t-red border-t-red/30 bg-t-red/10 hover:bg-t-red/20'
                      "
                      :disabled="u.id === auth.user?.id"
                      :title="u.id === auth.user?.id ? 'cannot toggle own account' : (u.is_active ? 'deactivate' : 'activate')"
                      @click="toggleActive(u)"
                    >
                      {{ u.is_active ? 'active' : 'inactive' }}
                    </button>
                  </td>

                  <!-- Last login -->
                  <td class="px-3 py-2.5 text-t-fg-dark text-xs hidden md:table-cell">{{ formatDate(u.last_login_at) }}</td>

                  <!-- Actions -->
                  <td class="px-3 py-2.5 text-right">
                    <div class="flex items-center justify-end gap-1">
                      <button
                        class="text-t-fg-dark hover:text-t-fg border-t-border hover:bg-t-bg-hover rounded border px-2 py-1 text-[10px] transition-colors"
                        title="Force logout"
                        @click="revokeSessionsFor(u)"
                      >
                        logout
                      </button>
                      <button
                        class="text-t-fg-dark hover:text-t-fg border-t-border hover:bg-t-bg-hover rounded border px-2 py-1 text-[10px] transition-colors"
                        title="Reset password"
                        @click="openReset(u.id)"
                      >
                        reset pw
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Inline reset password form -->
          <div v-if="resetId" class="border-t-border border-t p-5">
            <h4 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">
              Reset Password for {{ users.find(u => u.id === resetId)?.username }}
            </h4>
            <div class="flex flex-wrap items-end gap-3">
              <label class="block">
                <span class="text-t-fg-dark text-xs">new password</span>
                <input
                  v-model="resetPassword"
                  type="password"
                  autocomplete="new-password"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-48 border px-3 py-1.5 text-sm outline-none"
                  placeholder="min 8 chars"
                />
              </label>
              <label class="block">
                <span class="text-t-fg-dark text-xs">confirm</span>
                <input
                  v-model="resetConfirm"
                  type="password"
                  autocomplete="new-password"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-blue mt-1 block w-48 border px-3 py-1.5 text-sm outline-none"
                />
              </label>
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-3 py-1.5 text-xs transition-all"
                :disabled="resetting"
                @click="submitReset"
              >
                {{ resetting ? 'resetting...' : 'reset' }}
              </button>
              <button
                class="text-t-fg-dark hover:text-t-fg text-xs transition-colors"
                @click="resetId = null"
              >
                cancel
              </button>
            </div>
            <span v-if="resetError" class="text-t-red mt-2 block text-xs">{{ resetError }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
