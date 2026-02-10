<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api, ApiError } from '@/lib/api'
import type { NotificationChannel } from '@/types/notification'

const channels = ref<NotificationChannel[]>([])
const loading = ref(true)
const loadError = ref('')

// Modal state.
const showModal = ref(false)
const editing = ref<NotificationChannel | null>(null)
const saving = ref(false)
const saveError = ref('')

// Form fields.
const formName = ref('')
const formType = ref<'slack' | 'webhook'>('slack')
const formEnabled = ref(true)
const formWebhookURL = ref('')
const formWebhookMethod = ref('POST')
const formWebhookHeaders = ref('')
const formWebhookTemplate = ref('')

// Delete state.
const confirmDelete = ref<number | null>(null)
const deleteError = ref('')

// Test state.
const testing = ref<number | null>(null)
const testResult = ref<{ channelId: number; success: boolean; message: string } | null>(null)

const enabledChannels = computed(() => channels.value.filter(c => c.enabled))
const disabledChannels = computed(() => channels.value.filter(c => !c.enabled))

function formatDate(ts: string): string {
  return new Date(ts).toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', hour12: false,
  })
}

function typeLabel(type: string): string {
  return type === 'slack' ? 'Slack' : type === 'webhook' ? 'Webhook' : type
}

async function fetchChannels() {
  try {
    const res = await api.listChannels()
    channels.value = res.data
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : 'Failed to load channels'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  formName.value = ''
  formType.value = 'slack'
  formEnabled.value = true
  formWebhookURL.value = ''
  formWebhookMethod.value = 'POST'
  formWebhookHeaders.value = ''
  formWebhookTemplate.value = ''
  saveError.value = ''
  showModal.value = true
}

function openEdit(ch: NotificationChannel) {
  editing.value = ch
  formName.value = ch.name
  formType.value = ch.type
  formEnabled.value = ch.enabled

  if (ch.type === 'slack') {
    formWebhookURL.value = (ch.config as Record<string, string>).webhook_url || ''
  } else if (ch.type === 'webhook') {
    const cfg = ch.config as Record<string, string>
    formWebhookURL.value = cfg.url || ''
    formWebhookMethod.value = cfg.method || 'POST'
    formWebhookHeaders.value = cfg.headers ? JSON.stringify(cfg.headers, null, 2) : ''
    formWebhookTemplate.value = cfg.template || ''
  }
  saveError.value = ''
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  editing.value = null
}

function buildConfig(): Record<string, unknown> {
  if (formType.value === 'slack') {
    return { webhook_url: formWebhookURL.value }
  }
  const cfg: Record<string, unknown> = { url: formWebhookURL.value }
  if (formWebhookMethod.value && formWebhookMethod.value !== 'POST') {
    cfg.method = formWebhookMethod.value
  }
  if (formWebhookHeaders.value.trim()) {
    try {
      cfg.headers = JSON.parse(formWebhookHeaders.value)
    } catch {
      // Will be caught as saveError
    }
  }
  if (formWebhookTemplate.value.trim()) {
    cfg.template = formWebhookTemplate.value
  }
  return cfg
}

async function saveChannel() {
  saveError.value = ''
  const name = formName.value.trim()
  if (!name) {
    saveError.value = 'name is required'
    return
  }
  if (!formWebhookURL.value.trim()) {
    saveError.value = formType.value === 'slack' ? 'webhook URL is required' : 'URL is required'
    return
  }

  saving.value = true
  try {
    const body = {
      name,
      type: formType.value,
      enabled: formEnabled.value,
      config: buildConfig(),
    }

    if (editing.value) {
      const res = await api.updateChannel(editing.value.id, body)
      const idx = channels.value.findIndex(c => c.id === editing.value!.id)
      if (idx >= 0) channels.value[idx] = res.data
    } else {
      const res = await api.createChannel(body)
      channels.value.push(res.data)
    }
    closeModal()
  } catch (e) {
    saveError.value = e instanceof ApiError ? e.message : 'Failed to save channel'
  } finally {
    saving.value = false
  }
}

async function deleteChannel(id: number) {
  deleteError.value = ''
  try {
    await api.deleteChannel(id)
    channels.value = channels.value.filter(c => c.id !== id)
  } catch (e) {
    deleteError.value = e instanceof ApiError ? e.message : 'Failed to delete channel'
  }
  confirmDelete.value = null
}

async function testChannel(id: number) {
  testing.value = id
  testResult.value = null
  try {
    const res = await api.testChannel(id)
    testResult.value = {
      channelId: id,
      success: res.success,
      message: res.success
        ? `OK (${res.duration_ms}ms)`
        : res.error || `HTTP ${res.status_code}`,
    }
  } catch (e) {
    testResult.value = {
      channelId: id,
      success: false,
      message: e instanceof ApiError ? e.message : 'Test failed',
    }
  } finally {
    testing.value = null
  }
}

onMounted(fetchChannels)
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <p class="text-t-fg-dark text-sm">notification destinations (Slack, webhooks)</p>
      <button
        class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
        @click="openCreate"
      >
        + add channel
      </button>
    </div>

    <!-- Loading / error -->
    <div v-if="loading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
    <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>

    <template v-else>
      <!-- Delete error -->
      <div v-if="deleteError" class="text-t-red text-sm px-5 py-2">{{ deleteError }}</div>

      <!-- Enabled channels -->
      <div class="bg-t-bg-dark border-t-border rounded border">
        <div class="border-t-border border-b px-5 py-2.5">
          <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
            Channels
            <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ channels.length }}</span>
          </h3>
        </div>

        <!-- Table header -->
        <div v-if="channels.length > 0" class="text-t-fg-gutter border-t-border flex border-b px-5 py-2 text-xs uppercase tracking-wider">
          <span class="w-8 shrink-0"></span>
          <span class="w-48 shrink-0">Name</span>
          <span class="w-28 shrink-0">Type</span>
          <span class="w-36 shrink-0">Updated</span>
          <span class="min-w-0 flex-1">Status</span>
          <span class="w-48 shrink-0 text-right">Actions</span>
        </div>

        <!-- Channel rows -->
        <div class="divide-t-border divide-y">
          <div
            v-for="ch in [...enabledChannels, ...disabledChannels]"
            :key="ch.id"
            class="hover:bg-t-bg-hover flex items-center px-5 py-3 text-sm transition-colors"
            :class="{ 'opacity-50': !ch.enabled }"
          >
            <div class="w-8 shrink-0">
              <span
                class="inline-block h-2 w-2 rounded-full"
                :class="ch.enabled ? 'bg-t-green' : 'bg-t-fg-gutter'"
              />
            </div>
            <div class="w-48 shrink-0">
              <span class="text-t-fg font-medium">{{ ch.name }}</span>
            </div>
            <div class="w-28 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs uppercase"
                :class="ch.type === 'slack' ? 'bg-t-purple/10 text-t-purple' : 'bg-t-blue/10 text-t-blue'"
              >
                {{ typeLabel(ch.type) }}
              </span>
            </div>
            <div class="w-36 shrink-0">
              <span class="text-t-fg-dark" :title="formatDate(ch.updated_at)">{{ formatDate(ch.updated_at) }}</span>
            </div>
            <div class="min-w-0 flex-1">
              <!-- Test result -->
              <span
                v-if="testResult && testResult.channelId === ch.id"
                class="text-xs"
                :class="testResult.success ? 'text-t-green' : 'text-t-red'"
              >
                {{ testResult.message }}
              </span>
            </div>
            <div class="w-48 shrink-0 flex items-center justify-end gap-3">
              <button
                class="text-t-fg-dark hover:text-t-teal text-xs transition-colors"
                :disabled="testing === ch.id"
                @click="testChannel(ch.id)"
              >
                {{ testing === ch.id ? 'testing...' : 'test' }}
              </button>
              <button
                class="text-t-fg-dark hover:text-t-blue text-xs transition-colors"
                @click="openEdit(ch)"
              >
                edit
              </button>
              <template v-if="confirmDelete !== ch.id">
                <button
                  class="text-t-fg-dark hover:text-t-red text-xs transition-colors"
                  @click="confirmDelete = ch.id"
                >
                  delete
                </button>
              </template>
              <template v-else>
                <button class="text-t-red hover:brightness-125 text-xs font-semibold" @click="deleteChannel(ch.id)">yes</button>
                <button class="text-t-fg-dark hover:text-t-fg text-xs" @click="confirmDelete = null">no</button>
              </template>
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="channels.length === 0" class="px-5 py-10 text-center">
          <p class="text-t-fg-dark text-sm">no notification channels configured</p>
          <button
            class="text-t-yellow mt-2 text-sm hover:brightness-125"
            @click="openCreate"
          >
            add your first channel
          </button>
        </div>
      </div>
    </template>

    <!-- Modal overlay -->
    <Teleport to="body">
      <Transition name="modal">
        <div
          v-if="showModal"
          class="fixed inset-0 z-50 flex items-start justify-center bg-black/50 pt-20"
          @click.self="closeModal"
        >
          <div class="bg-t-bg-dark border-t-border w-full max-w-2xl rounded border shadow-xl">
            <!-- Modal header -->
            <div class="border-t-border flex items-center justify-between border-b px-5 py-3">
              <h3 class="text-t-fg text-sm font-semibold">{{ editing ? 'Edit Channel' : 'Add Channel' }}</h3>
              <button class="text-t-fg-dark hover:text-t-fg text-xs" @click="closeModal">close</button>
            </div>

            <!-- Modal body -->
            <div class="space-y-4 px-5 py-4">
              <!-- Name -->
              <label class="block">
                <span class="text-t-fg-dark text-sm">Name</span>
                <input
                  v-model="formName"
                  type="text"
                  placeholder="e.g. ops-slack"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 text-sm outline-none"
                />
              </label>

              <!-- Type -->
              <label class="block">
                <span class="text-t-fg-dark text-sm">Type</span>
                <div class="mt-1.5 flex gap-2">
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formType === 'slack' ? 'border-t-yellow text-t-yellow' : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'"
                    :disabled="!!editing"
                    @click="formType = 'slack'"
                  >
                    Slack
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formType === 'webhook' ? 'border-t-yellow text-t-yellow' : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'"
                    :disabled="!!editing"
                    @click="formType = 'webhook'"
                  >
                    Webhook
                  </button>
                </div>
              </label>

              <!-- Enabled -->
              <label class="flex items-center gap-2">
                <input v-model="formEnabled" type="checkbox" class="accent-t-yellow" />
                <span class="text-t-fg-dark text-sm">Enabled</span>
              </label>

              <!-- Slack config -->
              <template v-if="formType === 'slack'">
                <label class="block">
                  <span class="text-t-fg-dark text-sm">Webhook URL</span>
                  <input
                    v-model="formWebhookURL"
                    type="url"
                    placeholder="https://hooks.slack.com/services/..."
                    class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 text-sm outline-none"
                  />
                </label>
                <p class="text-t-fg-gutter text-xs">The webhook is tied to the channel you selected when creating it in Slack. To send to a different channel, create a separate webhook.</p>
              </template>

              <!-- Webhook config -->
              <template v-if="formType === 'webhook'">
                <label class="block">
                  <span class="text-t-fg-dark text-sm">URL</span>
                  <input
                    v-model="formWebhookURL"
                    type="url"
                    placeholder="https://example.com/webhook"
                    class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 text-sm outline-none"
                  />
                </label>
                <label class="block">
                  <span class="text-t-fg-dark text-sm">Method</span>
                  <div class="mt-1.5 flex gap-2">
                    <button
                      v-for="m in ['POST', 'PUT']"
                      :key="m"
                      class="border px-3 py-1.5 text-sm transition-all"
                      :class="formWebhookMethod === m ? 'border-t-yellow text-t-yellow' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                      @click="formWebhookMethod = m"
                    >
                      {{ m }}
                    </button>
                  </div>
                </label>
                <label class="block">
                  <span class="text-t-fg-dark text-sm">Headers <span class="text-t-fg-gutter">(JSON, optional)</span></span>
                  <textarea
                    v-model="formWebhookHeaders"
                    rows="3"
                    placeholder='{"Authorization": "Bearer ..."}'
                    class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 font-mono text-sm outline-none"
                  />
                </label>
                <label class="block">
                  <span class="text-t-fg-dark text-sm">Template <span class="text-t-fg-gutter">(Go text/template, optional)</span></span>
                  <textarea
                    v-model="formWebhookTemplate"
                    rows="3"
                    placeholder="Leave empty for default JSON payload"
                    class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 font-mono text-sm outline-none"
                  />
                </label>
              </template>
            </div>

            <!-- Modal footer -->
            <div class="border-t-border flex items-center gap-3 border-t px-5 py-3">
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="saving"
                @click="saveChannel"
              >
                {{ saving ? 'saving...' : editing ? 'save changes' : 'create channel' }}
              </button>
              <button
                class="text-t-fg-dark hover:text-t-fg text-sm transition-colors"
                @click="closeModal"
              >
                cancel
              </button>
              <span v-if="saveError" class="text-t-red text-sm">{{ saveError }}</span>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.15s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
</style>
