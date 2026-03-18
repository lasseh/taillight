<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ApiError } from '@/lib/api'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

// Capture once at setup time so it doesn't change when the route changes
// mid-navigation (e.g. retry timer firing after handleSubmit already navigated).
const redirectTarget = (() => {
  const r = route.query.redirect
  return typeof r === 'string' && r.startsWith('/') ? r : '/'
})()

const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

// Re-check auth periodically while on the login page. Handles auth-disabled
// mode where the API was temporarily unreachable on first load: once the API
// recovers and returns an anonymous user, redirect to home automatically.
let retryTimer: ReturnType<typeof setInterval> | undefined

function startRetryTimer() {
  if (retryTimer) clearInterval(retryTimer)
  let attempts = 0
  retryTimer = setInterval(async () => {
    if (++attempts > 30) {
      clearInterval(retryTimer)
      retryTimer = undefined
      return
    }
    await auth.init()
    if (auth.user) {
      clearInterval(retryTimer)
      retryTimer = undefined
      router.replace(redirectTarget)
    }
  }, 2000)
}

async function handleRetry() {
  await auth.init()
  if (auth.user) {
    router.replace(redirectTarget)
    return
  }
  if (!auth.apiError) return
  startRetryTimer()
}

onMounted(() => {
  if (!auth.user) {
    startRetryTimer()
  }
})

onUnmounted(() => {
  if (retryTimer) clearInterval(retryTimer)
})

async function handleSubmit() {
  error.value = ''
  if (!username.value || !password.value) {
    error.value = 'Username and password are required'
    return
  }

  loading.value = true
  try {
    await auth.login(username.value, password.value)
    router.push(redirectTarget)
  } catch (e) {
    if (e instanceof ApiError && e.status >= 502 && e.status <= 504) {
      error.value = 'API is unreachable — it may be down or restarting'
    } else if (e instanceof ApiError) {
      error.value = e.message
    } else {
      error.value = 'API is unreachable — it may be down or restarting'
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <h1 class="logo">
        <span class="bg-gradient-to-r from-sev-emerg to-sev-alert bg-clip-text text-transparent">[Taillight]</span>
      </h1>
      <template v-if="auth.apiError">
        <p class="subtitle">Cannot connect to server</p>
        <div class="api-error">
          <p class="api-error-code">{{ auth.apiError }}</p>
          <p class="api-error-detail">Retrying automatically in the background...</p>
          <button type="button" class="btn" @click="handleRetry">Retry now</button>
        </div>
      </template>

      <template v-else>
        <p class="subtitle">Sign in to your account</p>

        <form @submit.prevent="handleSubmit" class="form" autocomplete="on">
          <div class="field">
            <label for="username" class="field-label">Username</label>
            <input
              id="username"
              v-model="username"
              type="text"
              autocomplete="username"
              spellcheck="false"
              class="field-input"
            />
          </div>

          <div class="field">
            <div class="field-label-row">
              <label for="password" class="field-label">Password</label>
            </div>
            <input
              id="password"
              v-model="password"
              type="password"
              autocomplete="current-password"
              class="field-input"
            />
          </div>

          <Transition name="err">
            <p v-if="error" class="error" role="alert">{{ error }}</p>
          </Transition>

          <button type="submit" class="btn" :disabled="loading">
            {{ loading ? 'Signing in...' : 'Sign in' }}
          </button>
        </form>
      </template>
    </div>

    <p class="footer-text">Log aggregation &amp; search</p>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: var(--color-t-bg);
  padding: 1rem;
}

.login-card {
  width: 100%;
  max-width: 360px;
  background: var(--color-t-bg-dark);
  border: 1px solid var(--color-t-border);
  border-radius: 12px;
  padding: 2.5rem 2rem 2rem;
}

.logo {
  font-family: var(--font-mono);
  font-size: 1.5rem;
  font-weight: 700;
  text-align: center;
  margin: 0 0 0.5rem;
  line-height: 1;
}

.subtitle {
  font-family: var(--font-mono);
  font-size: 0.8rem;
  color: var(--color-t-fg-dark);
  text-align: center;
  margin: 0 0 2rem;
}

.form {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.field-label-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
}

.field-label {
  font-family: var(--font-mono);
  font-size: 0.75rem;
  color: var(--color-t-fg-dark);
}

.field-input {
  font-family: var(--font-mono);
  font-size: 0.85rem;
  color: var(--color-t-fg);
  background: var(--color-t-bg-highlight);
  border: 1px solid var(--color-t-border);
  border-radius: 6px;
  padding: 0.6rem 0.75rem;
  outline: none;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}

.field-input::placeholder {
  color: var(--color-t-fg-gutter);
}

.field-input:focus {
  border-color: var(--color-t-blue);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-t-blue) 15%, transparent);
}

.field-input::selection {
  background: var(--color-t-blue);
  color: var(--color-t-bg);
}

.api-error {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  text-align: center;
}

.api-error-code {
  font-family: var(--font-mono);
  font-size: 0.8rem;
  color: var(--color-sev-crit);
  margin: 0;
  line-height: 1.4;
}

.api-error-detail {
  font-family: var(--font-mono);
  font-size: 0.75rem;
  color: var(--color-t-fg-dark);
  margin: 0;
  line-height: 1.4;
}

.error {
  font-family: var(--font-mono);
  font-size: 0.75rem;
  color: var(--color-sev-crit);
  text-align: center;
  margin: -0.25rem 0 0;
  line-height: 1.4;
}

.err-enter-active,
.err-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.err-enter-from,
.err-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.btn {
  font-family: var(--font-mono);
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--color-t-fg);
  background: var(--color-t-bg-dark);
  border: 1px solid var(--color-t-fg-gutter);
  border-radius: 6px;
  padding: 0.6rem 0;
  cursor: pointer;
  transition: filter 0.15s ease, opacity 0.15s ease;
  margin-top: 0.5rem;
}

.btn:hover:not(:disabled) {
  filter: brightness(1.25);
}

.btn:active:not(:disabled) {
  filter: brightness(0.9);
}

.btn:disabled {
  opacity: 0.5;
  cursor: default;
}

.footer-text {
  font-family: var(--font-mono);
  font-size: 0.7rem;
  color: var(--color-t-fg-gutter);
  margin-top: 1.5rem;
}
</style>
