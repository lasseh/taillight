<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api, ApiError } from '@/lib/api'
import type { NotificationRule, NotificationChannel } from '@/types/notification'
import { useFocusTrap } from '@/composables/useFocusTrap'

const rules = ref<NotificationRule[]>([])
const channels = ref<NotificationChannel[]>([])
const loading = ref(true)
const loadError = ref('')

// Modal state.
const showModal = ref(false)
const editing = ref<NotificationRule | null>(null)
const saving = ref(false)
const saveError = ref('')

// Form fields.
const formName = ref('')
const formEnabled = ref(true)
const formEventKind = ref<'srvlog' | 'netlog' | 'applog'>('srvlog')
const formHostname = ref('')
const formProgramname = ref('')
const formSeverity = ref('')
const formSeverityMax = ref('')
const formFacility = ref('')
const formSyslogTag = ref('')
const formMsgID = ref('')
const formService = ref('')
const formComponent = ref('')
const formHost = ref('')
const formLevel = ref('')
const formSearch = ref('')
const formChannelIDs = ref<number[]>([])
const formBurstWindow = ref(10)
const formCooldownSeconds = ref(60)

const modalEl = ref<HTMLElement | null>(null)
useFocusTrap(modalEl)

// Delete state.
const confirmDelete = ref<number | null>(null)
const deleteError = ref('')

const enabledRules = computed(() => rules.value.filter(r => r.enabled))
const disabledRules = computed(() => rules.value.filter(r => !r.enabled))

const severityOptions = [
  { value: '0', label: 'Emergency (0)' },
  { value: '1', label: 'Alert (1)' },
  { value: '2', label: 'Critical (2)' },
  { value: '3', label: 'Error (3)' },
  { value: '4', label: 'Warning (4)' },
  { value: '5', label: 'Notice (5)' },
  { value: '6', label: 'Informational (6)' },
  { value: '7', label: 'Debug (7)' },
]

const levelOptions = ['FATAL', 'ERROR', 'WARN', 'INFO', 'DEBUG']

function channelName(id: number): string {
  return channels.value.find(c => c.id === id)?.name || `#${id}`
}

function channelType(id: number): string {
  return channels.value.find(c => c.id === id)?.type || ''
}

function channelBadgeClass(type_: string): string {
  if (type_ === 'slack') return 'bg-t-purple/10 text-t-purple'
  if (type_ === 'ntfy') return 'bg-t-teal/10 text-t-teal'
  return 'bg-t-blue/10 text-t-blue'
}

function channelDotClass(type_: string): string {
  if (type_ === 'slack') return 'bg-t-purple'
  if (type_ === 'ntfy') return 'bg-t-teal'
  return 'bg-t-blue'
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${Math.floor(seconds / 3600)}h`
}

async function fetchData() {
  try {
    const [rulesRes, channelsRes] = await Promise.all([
      api.listRules(),
      api.listChannels(),
    ])
    rules.value = rulesRes.data
    channels.value = channelsRes.data
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : 'Failed to load data'
  } finally {
    loading.value = false
  }
}

function resetForm() {
  formName.value = ''
  formEnabled.value = true
  formEventKind.value = 'srvlog'
  formHostname.value = ''
  formProgramname.value = ''
  formSeverity.value = ''
  formSeverityMax.value = ''
  formFacility.value = ''
  formSyslogTag.value = ''
  formMsgID.value = ''
  formService.value = ''
  formComponent.value = ''
  formHost.value = ''
  formLevel.value = ''
  formSearch.value = ''
  formChannelIDs.value = []
  formBurstWindow.value = 10
  formCooldownSeconds.value = 60
}

function openCreate() {
  editing.value = null
  resetForm()
  saveError.value = ''
  showModal.value = true
}

function openEdit(rule: NotificationRule) {
  editing.value = rule
  formName.value = rule.name
  formEnabled.value = rule.enabled
  formEventKind.value = rule.event_kind
  formHostname.value = rule.hostname || ''
  formProgramname.value = rule.programname || ''
  formSeverity.value = rule.severity != null ? String(rule.severity) : ''
  formSeverityMax.value = rule.severity_max != null ? String(rule.severity_max) : ''
  formFacility.value = rule.facility != null ? String(rule.facility) : ''
  formSyslogTag.value = rule.syslogtag || ''
  formMsgID.value = rule.msgid || ''
  formService.value = rule.service || ''
  formComponent.value = rule.component || ''
  formHost.value = rule.host || ''
  formLevel.value = rule.level || ''
  formSearch.value = rule.search || ''
  formChannelIDs.value = [...(rule.channel_ids ?? [])]
  formBurstWindow.value = rule.burst_window
  formCooldownSeconds.value = rule.cooldown_seconds
  saveError.value = ''
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  editing.value = null
}

function toggleChannel(id: number) {
  const idx = formChannelIDs.value.indexOf(id)
  if (idx >= 0) {
    formChannelIDs.value.splice(idx, 1)
  } else {
    formChannelIDs.value.push(id)
  }
}

function buildRule(): Partial<NotificationRule> {
  const rule: Partial<NotificationRule> = {
    name: formName.value.trim(),
    enabled: formEnabled.value,
    event_kind: formEventKind.value,
    search: formSearch.value || undefined,
    channel_ids: formChannelIDs.value,
    burst_window: formBurstWindow.value,
    cooldown_seconds: formCooldownSeconds.value,
  }

  if (formEventKind.value === 'srvlog' || formEventKind.value === 'netlog') {
    if (formHostname.value) rule.hostname = formHostname.value
    if (formProgramname.value) rule.programname = formProgramname.value
    if (formSeverity.value) rule.severity = Number(formSeverity.value)
    if (formSeverityMax.value) rule.severity_max = Number(formSeverityMax.value)
    if (formFacility.value) rule.facility = Number(formFacility.value)
    if (formSyslogTag.value) rule.syslogtag = formSyslogTag.value
    if (formMsgID.value) rule.msgid = formMsgID.value
  } else {
    if (formService.value) rule.service = formService.value
    if (formComponent.value) rule.component = formComponent.value
    if (formHost.value) rule.host = formHost.value
    if (formLevel.value) rule.level = formLevel.value
  }

  return rule
}

async function saveRule() {
  saveError.value = ''
  if (!formName.value.trim()) {
    saveError.value = 'name is required'
    return
  }
  if (formChannelIDs.value.length === 0) {
    saveError.value = 'select at least one channel'
    return
  }

  saving.value = true
  try {
    const body = buildRule()
    if (editing.value) {
      const res = await api.updateRule(editing.value.id, body)
      const idx = rules.value.findIndex(r => r.id === editing.value!.id)
      if (idx >= 0) rules.value[idx] = res.data
    } else {
      const res = await api.createRule(body)
      rules.value.push(res.data)
    }
    closeModal()
  } catch (e) {
    saveError.value = e instanceof ApiError ? e.message : 'Failed to save rule'
  } finally {
    saving.value = false
  }
}

async function deleteRule(id: number) {
  deleteError.value = ''
  try {
    await api.deleteRule(id)
    rules.value = rules.value.filter(r => r.id !== id)
  } catch (e) {
    deleteError.value = e instanceof ApiError ? e.message : 'Failed to delete rule'
  }
  confirmDelete.value = null
}

onMounted(fetchData)
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <p class="text-t-fg-dark text-sm">rules define which events trigger notifications</p>
      <button
        class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
        @click="openCreate"
      >
        + add rule
      </button>
    </div>

    <!-- Loading / error -->
    <div v-if="loading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
    <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>

    <template v-else>
      <div v-if="deleteError" class="text-t-red text-sm px-5 py-2">{{ deleteError }}</div>

      <!-- Rules list -->
      <div class="bg-t-bg-dark border-t-border rounded border">
        <div class="border-t-border border-b px-5 py-2.5">
          <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
            Rules
            <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ rules.length }}</span>
          </h3>
        </div>

        <!-- Table header -->
        <div v-if="rules.length > 0" class="text-t-fg-gutter border-t-border flex border-b px-5 py-2 text-xs uppercase tracking-wider">
          <span class="w-8 shrink-0"></span>
          <span class="w-48 shrink-0">Name</span>
          <span class="w-24 shrink-0">Kind</span>
          <span class="w-40 shrink-0">Channels</span>
          <span class="w-28 shrink-0">Burst/Cool</span>
          <span class="min-w-0 flex-1">Filters</span>
          <span class="w-32 shrink-0 text-right">Actions</span>
        </div>

        <!-- Rule rows -->
        <div class="divide-t-border divide-y">
          <div
            v-for="rule in [...enabledRules, ...disabledRules]"
            :key="rule.id"
            class="hover:bg-t-bg-hover flex items-center px-5 py-3 text-sm transition-colors"
            :class="{ 'opacity-50': !rule.enabled }"
          >
            <div class="w-8 shrink-0">
              <span
                class="inline-block h-2 w-2 rounded-full"
                :class="rule.enabled ? 'bg-t-green' : 'bg-t-fg-gutter'"
              />
            </div>
            <div class="w-48 shrink-0">
              <span class="text-t-fg font-medium">{{ rule.name }}</span>
            </div>
            <div class="w-24 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs uppercase"
                :class="rule.event_kind === 'applog' ? 'bg-t-magenta/10 text-t-magenta' : rule.event_kind === 'netlog' ? 'bg-t-blue/10 text-t-blue' : 'bg-t-teal/10 text-t-teal'"
              >
                {{ rule.event_kind }}
              </span>
            </div>
            <div class="w-40 shrink-0">
              <span
                v-for="cid in rule.channel_ids"
                :key="cid"
                class="mr-1 inline-block rounded px-1.5 py-0.5 text-xs"
                :class="channelBadgeClass(channelType(cid))"
              >
                {{ channelName(cid) }}
              </span>
            </div>
            <div class="w-28 shrink-0">
              <span class="text-t-fg-dark text-xs">{{ formatDuration(rule.burst_window) }} / {{ formatDuration(rule.cooldown_seconds) }}</span>
            </div>
            <div class="min-w-0 flex-1 truncate">
              <span class="text-t-fg-gutter text-xs">
                <template v-if="rule.event_kind === 'srvlog' || rule.event_kind === 'netlog'">
                  {{ [rule.hostname, rule.programname, rule.search].filter(Boolean).join(', ') || 'all events' }}
                </template>
                <template v-else>
                  {{ [rule.service, rule.component, rule.level, rule.search].filter(Boolean).join(', ') || 'all events' }}
                </template>
              </span>
            </div>
            <div class="w-32 shrink-0 flex items-center justify-end gap-3">
              <button
                class="text-t-blue/70 hover:text-t-blue text-xs transition-colors"
                @click="openEdit(rule)"
              >
                edit
              </button>
              <template v-if="confirmDelete !== rule.id">
                <button
                  class="text-t-red/70 hover:text-t-red text-xs transition-colors"
                  @click="confirmDelete = rule.id"
                >
                  delete
                </button>
              </template>
              <template v-else>
                <button class="text-t-red hover:brightness-125 text-xs font-semibold" @click="deleteRule(rule.id)">yes</button>
                <button class="text-t-fg-dark hover:text-t-fg text-xs" @click="confirmDelete = null">no</button>
              </template>
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="rules.length === 0" class="px-5 py-10 text-center">
          <p class="text-t-fg-dark text-sm">no notification rules configured</p>
          <button
            class="text-t-yellow mt-2 text-sm hover:brightness-125"
            @click="openCreate"
          >
            create your first rule
          </button>
        </div>
      </div>
    </template>

    <!-- Modal overlay -->
    <Teleport to="body">
      <Transition name="modal">
        <div
          v-if="showModal"
          class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 pt-10 pb-10"
          @click.self="closeModal"
        >
          <div ref="modalEl" class="bg-t-bg-dark border-t-border w-full max-w-2xl rounded border shadow-xl">
            <!-- Modal header -->
            <div class="border-t-border flex items-center justify-between border-b px-5 py-3">
              <h3 class="text-t-fg text-sm font-semibold">{{ editing ? 'Edit Rule' : 'Add Rule' }}</h3>
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
                  placeholder="e.g. critical-srvlog-alerts"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 text-sm outline-none"
                />
              </label>

              <!-- Event kind -->
              <label class="block">
                <span class="text-t-fg-dark text-sm">Event Kind</span>
                <div class="mt-1.5 flex gap-2">
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formEventKind === 'srvlog' ? 'border-t-teal text-t-teal' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                    @click="formEventKind = 'srvlog'"
                  >
                    Srvlog
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formEventKind === 'netlog' ? 'border-t-blue text-t-blue' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                    @click="formEventKind = 'netlog'"
                  >
                    Netlog
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formEventKind === 'applog' ? 'border-t-magenta text-t-magenta' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                    @click="formEventKind = 'applog'"
                  >
                    AppLog
                  </button>
                </div>
              </label>

              <!-- Enabled -->
              <label class="flex items-center gap-2">
                <input v-model="formEnabled" type="checkbox" class="accent-t-yellow" />
                <span class="text-t-fg-dark text-sm">Enabled</span>
              </label>

              <!-- Srvlog/Netlog filters (shared syslog fields) -->
              <template v-if="formEventKind === 'srvlog' || formEventKind === 'netlog'">
                <div class="border-t-border space-y-3 border-t pt-3">
                  <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider">{{ formEventKind === 'netlog' ? 'Netlog' : 'Srvlog' }} Filters</span>
                  <div class="grid grid-cols-2 gap-3">
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Hostname</span>
                      <input
                        v-model="formHostname"
                        type="text"
                        placeholder="router* or exact"
                        class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      />
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Program</span>
                      <input
                        v-model="formProgramname"
                        type="text"
                        placeholder="e.g. rpd, sshd"
                        class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      />
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Severity (exact)</span>
                      <select
                        v-model="formSeverity"
                        class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      >
                        <option value="">any</option>
                        <option v-for="opt in severityOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                      </select>
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Severity max</span>
                      <select
                        v-model="formSeverityMax"
                        class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      >
                        <option value="">any</option>
                        <option v-for="opt in severityOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                      </select>
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Syslog Tag</span>
                      <input
                        v-model="formSyslogTag"
                        type="text"
                        placeholder="e.g. rpd[1234]"
                        class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      />
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Message ID</span>
                      <input
                        v-model="formMsgID"
                        type="text"
                        placeholder="e.g. BGP_PREFIX_*"
                        class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      />
                    </label>
                  </div>
                </div>
              </template>

              <!-- AppLog filters -->
              <template v-if="formEventKind === 'applog'">
                <div class="border-t-border space-y-3 border-t pt-3">
                  <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider">AppLog Filters</span>
                  <div class="grid grid-cols-2 gap-3">
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Service</span>
                      <input
                        v-model="formService"
                        type="text"
                        placeholder="e.g. api-gateway"
                        class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      />
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Component</span>
                      <input
                        v-model="formComponent"
                        type="text"
                        placeholder="e.g. auth"
                        class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      />
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Host</span>
                      <input
                        v-model="formHost"
                        type="text"
                        placeholder="e.g. web1.example.com"
                        class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      />
                    </label>
                    <label class="block">
                      <span class="text-t-fg-dark text-xs">Level (minimum)</span>
                      <select
                        v-model="formLevel"
                        class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                      >
                        <option value="">any</option>
                        <option v-for="l in levelOptions" :key="l" :value="l">{{ l }}</option>
                      </select>
                    </label>
                  </div>
                </div>
              </template>

              <!-- Search (shared) -->
              <label class="block">
                <span class="text-t-fg-dark text-sm">Message Search</span>
                <input
                  v-model="formSearch"
                  type="text"
                  placeholder="text to search in message body"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 text-sm outline-none"
                />
              </label>

              <!-- Channels -->
              <div class="border-t-border space-y-2 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider">Channels</span>
                <div v-if="channels.length === 0" class="text-t-fg-gutter text-sm">
                  no channels configured — create one first
                </div>
                <div v-else class="flex flex-wrap gap-2">
                  <button
                    v-for="ch in channels"
                    :key="ch.id"
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formChannelIDs.includes(ch.id)
                        ? 'border-t-yellow text-t-yellow'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                    "
                    @click="toggleChannel(ch.id)"
                  >
                    <span
                      class="mr-1.5 inline-block h-1.5 w-1.5 rounded-full"
                      :class="channelDotClass(ch.type)"
                    />
                    {{ ch.name }}
                  </button>
                </div>
              </div>

              <!-- Burst / Cooldown -->
              <div class="border-t-border space-y-3 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider">Anti-Spam</span>
                <div class="grid grid-cols-2 gap-3">
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Burst Window (seconds)</span>
                    <input
                      v-model.number="formBurstWindow"
                      type="number"
                      min="0"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Cooldown (seconds)</span>
                    <input
                      v-model.number="formCooldownSeconds"
                      type="number"
                      min="0"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>
                </div>
              </div>
            </div>

            <!-- Modal footer -->
            <div class="border-t-border flex items-center gap-3 border-t px-5 py-3">
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="saving"
                @click="saveRule"
              >
                {{ saving ? 'saving...' : editing ? 'save changes' : 'create rule' }}
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
