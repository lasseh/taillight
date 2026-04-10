<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api, ApiError } from '@/lib/api'
import type { SummarySchedule } from '@/types/summary'
import type { NotificationChannel } from '@/types/notification'
import { useFocusTrap } from '@/composables/useFocusTrap'
import { features } from '@/config'

const schedules = ref<SummarySchedule[]>([])
const channels = ref<NotificationChannel[]>([])
const loading = ref(true)
const loadError = ref('')

const showModal = ref(false)
const editing = ref<SummarySchedule | null>(null)
const saving = ref(false)
const saveError = ref('')

const formName = ref('')
const formEnabled = ref(true)
const formFrequency = ref<'daily' | 'weekly' | 'monthly'>('daily')
const formDayOfWeek = ref<number>(1)
const formDayOfMonth = ref<number>(1)
const formTimeOfDay = ref('07:00')
const formTimezone = ref('UTC')
const formEventKinds = ref<string[]>(['srvlog'])
const formSeverityMax = ref('')
const formHostname = ref('')
const formTopN = ref(25)
const formChannelIDs = ref<number[]>([])

const modalEl = ref<HTMLElement | null>(null)
useFocusTrap(modalEl)

const confirmDelete = ref<number | null>(null)
const deleteError = ref('')

const triggering = ref<number | null>(null)
const triggerResult = ref<{ scheduleId: number; success: boolean; message: string } | null>(null)

const enabledSchedules = computed(() => schedules.value.filter(s => s.enabled))
const disabledSchedules = computed(() => schedules.value.filter(s => !s.enabled))

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

const timezones = [
  'UTC',
  'Europe/Oslo',
  'Europe/London',
  'Europe/Berlin',
  'Europe/Paris',
  'Europe/Helsinki',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'Asia/Tokyo',
  'Asia/Shanghai',
  'Asia/Kolkata',
  'Australia/Sydney',
]

const dayOfWeekLabels = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

function channelName(id: number): string {
  return channels.value.find(c => c.id === id)?.name || `#${id}`
}

function channelType(id: number): string {
  return channels.value.find(c => c.id === id)?.type || ''
}

function channelBadgeClass(type_: string): string {
  if (type_ === 'slack') return 'bg-t-purple/10 text-t-purple'
  if (type_ === 'ntfy') return 'bg-t-teal/10 text-t-teal'
  if (type_ === 'email') return 'bg-t-green/10 text-t-green'
  return 'bg-t-blue/10 text-t-blue'
}

function channelDotClass(type_: string): string {
  if (type_ === 'slack') return 'bg-t-purple'
  if (type_ === 'ntfy') return 'bg-t-teal'
  if (type_ === 'email') return 'bg-t-green'
  return 'bg-t-blue'
}

function kindBadgeClass(kind: string): string {
  if (kind === 'applog') return 'bg-t-magenta/10 text-t-magenta'
  if (kind === 'netlog') return 'bg-t-fuchsia/10 text-t-fuchsia'
  return 'bg-t-teal/10 text-t-teal'
}

function frequencyBadgeClass(freq: string): string {
  if (freq === 'weekly') return 'bg-t-blue/10 text-t-blue'
  if (freq === 'monthly') return 'bg-t-purple/10 text-t-purple'
  return 'bg-t-green/10 text-t-green'
}

function formatLastRun(iso: string | null | undefined): string {
  if (!iso) return 'never'
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

async function fetchData() {
  try {
    const [schedRes, channelsRes] = await Promise.all([
      api.listSummarySchedules(),
      api.listChannels(),
    ])
    schedules.value = schedRes.data
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
  formFrequency.value = 'daily'
  formDayOfWeek.value = 1
  formDayOfMonth.value = 1
  formTimeOfDay.value = '07:00'
  formTimezone.value = 'UTC'
  formEventKinds.value = ['srvlog']
  formSeverityMax.value = ''
  formHostname.value = ''
  formTopN.value = 25
  formChannelIDs.value = []
}

function openCreate() {
  editing.value = null
  resetForm()
  saveError.value = ''
  showModal.value = true
}

function openEdit(sched: SummarySchedule) {
  editing.value = sched
  formName.value = sched.name
  formEnabled.value = sched.enabled
  formFrequency.value = sched.frequency
  formDayOfWeek.value = sched.day_of_week ?? 1
  formDayOfMonth.value = sched.day_of_month ?? 1
  formTimeOfDay.value = sched.time_of_day
  formTimezone.value = sched.timezone
  formEventKinds.value = [...sched.event_kinds]
  formSeverityMax.value = sched.severity_max != null ? String(sched.severity_max) : ''
  formHostname.value = sched.hostname || ''
  formTopN.value = sched.top_n
  formChannelIDs.value = [...(sched.channel_ids ?? [])]
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

function toggleEventKind(kind: string) {
  const idx = formEventKinds.value.indexOf(kind)
  if (idx >= 0) {
    if (formEventKinds.value.length > 1) {
      formEventKinds.value.splice(idx, 1)
    }
  } else {
    formEventKinds.value.push(kind)
  }
}

function buildSchedule(): Partial<SummarySchedule> {
  const sched: Partial<SummarySchedule> = {
    name: formName.value.trim(),
    enabled: formEnabled.value,
    frequency: formFrequency.value,
    time_of_day: formTimeOfDay.value,
    timezone: formTimezone.value,
    event_kinds: formEventKinds.value,
    hostname: formHostname.value || undefined,
    top_n: formTopN.value,
    channel_ids: formChannelIDs.value,
  }

  if (formFrequency.value === 'weekly') {
    sched.day_of_week = formDayOfWeek.value
  }
  if (formFrequency.value === 'monthly') {
    sched.day_of_month = formDayOfMonth.value
  }
  if (formSeverityMax.value !== '') {
    sched.severity_max = Number(formSeverityMax.value)
  }

  return sched
}

async function saveSchedule() {
  saveError.value = ''
  if (!formName.value.trim()) {
    saveError.value = 'name is required'
    return
  }
  if (formChannelIDs.value.length === 0) {
    saveError.value = 'select at least one channel'
    return
  }
  if (formEventKinds.value.length === 0) {
    saveError.value = 'select at least one event kind'
    return
  }

  saving.value = true
  try {
    const body = buildSchedule()
    if (editing.value) {
      const res = await api.updateSummarySchedule(editing.value.id, body)
      const idx = schedules.value.findIndex(s => s.id === editing.value!.id)
      if (idx >= 0) schedules.value[idx] = res.data
    } else {
      const res = await api.createSummarySchedule(body)
      schedules.value.push(res.data)
    }
    closeModal()
  } catch (e) {
    saveError.value = e instanceof ApiError ? e.message : 'Failed to save schedule'
  } finally {
    saving.value = false
  }
}

async function deleteSchedule(id: number) {
  deleteError.value = ''
  try {
    await api.deleteSummarySchedule(id)
    schedules.value = schedules.value.filter(s => s.id !== id)
  } catch (e) {
    deleteError.value = e instanceof ApiError ? e.message : 'Failed to delete schedule'
  }
  confirmDelete.value = null
}

async function triggerSchedule(id: number) {
  triggering.value = id
  triggerResult.value = null
  try {
    await api.triggerSummarySchedule(id)
    triggerResult.value = { scheduleId: id, success: true, message: 'sent' }
  } catch (e) {
    triggerResult.value = {
      scheduleId: id,
      success: false,
      message: e instanceof ApiError ? e.message : 'failed',
    }
  } finally {
    triggering.value = null
  }
}

onMounted(fetchData)
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <p class="text-t-fg-dark text-sm">periodic log digest reports sent to channels</p>
      <button
        class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
        @click="openCreate"
      >
        + add schedule
      </button>
    </div>

    <!-- Loading / error -->
    <div v-if="loading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
    <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>

    <template v-else>
      <div v-if="deleteError" class="text-t-red px-5 py-2 text-sm">{{ deleteError }}</div>

      <!-- Schedules list -->
      <div class="bg-t-bg-dark border-t-border rounded border">
        <div class="border-t-border border-b px-5 py-2.5">
          <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
            Schedules
            <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ schedules.length }}</span>
          </h3>
        </div>

        <!-- Table header -->
        <div v-if="schedules.length > 0" class="text-t-fg-gutter border-t-border flex border-b px-5 py-2 text-xs uppercase tracking-wider">
          <span class="w-8 shrink-0"></span>
          <span class="w-44 shrink-0">Name</span>
          <span class="w-24 shrink-0">Frequency</span>
          <span class="w-28 shrink-0">Kinds</span>
          <span class="w-24 shrink-0">Time</span>
          <span class="w-36 shrink-0">Channels</span>
          <span class="min-w-0 flex-1">Last Run</span>
          <span class="w-40 shrink-0 text-right">Actions</span>
        </div>

        <!-- Schedule rows -->
        <div class="divide-t-border divide-y">
          <div
            v-for="sched in [...enabledSchedules, ...disabledSchedules]"
            :key="sched.id"
            class="hover:bg-t-bg-hover flex items-center px-5 py-3 text-sm transition-colors"
            :class="{ 'opacity-50': !sched.enabled }"
          >
            <div class="w-8 shrink-0">
              <span
                class="inline-block h-2 w-2 rounded-full"
                :class="sched.enabled ? 'bg-t-green' : 'bg-t-fg-gutter'"
              />
            </div>
            <div class="w-44 shrink-0">
              <span class="text-t-fg font-medium">{{ sched.name }}</span>
            </div>
            <div class="w-24 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="frequencyBadgeClass(sched.frequency)"
              >
                {{ sched.frequency }}
              </span>
            </div>
            <div class="w-28 shrink-0">
              <span
                v-for="kind in sched.event_kinds"
                :key="kind"
                class="mr-1 inline-block rounded px-1.5 py-0.5 text-xs uppercase"
                :class="kindBadgeClass(kind)"
              >
                {{ kind }}
              </span>
            </div>
            <div class="w-24 shrink-0">
              <span class="text-t-fg-dark text-xs">{{ sched.time_of_day }}</span>
              <span class="text-t-fg-gutter ml-1 text-xs">{{ sched.timezone === 'UTC' ? 'UTC' : sched.timezone.split('/').pop() }}</span>
            </div>
            <div class="w-36 shrink-0">
              <span
                v-for="cid in sched.channel_ids"
                :key="cid"
                class="mr-1 inline-block rounded px-1.5 py-0.5 text-xs"
                :class="channelBadgeClass(channelType(cid))"
              >
                {{ channelName(cid) }}
              </span>
            </div>
            <div class="min-w-0 flex-1 truncate">
              <span class="text-t-fg-gutter text-xs">{{ formatLastRun(sched.last_run_at) }}</span>
              <span
                v-if="triggerResult?.scheduleId === sched.id"
                class="ml-2 text-xs"
                :class="triggerResult.success ? 'text-t-green' : 'text-t-red'"
              >
                {{ triggerResult.message }}
              </span>
            </div>
            <div class="flex w-40 shrink-0 items-center justify-end gap-3">
              <button
                class="text-t-yellow/70 hover:text-t-yellow text-xs transition-colors"
                :disabled="triggering === sched.id"
                @click="triggerSchedule(sched.id)"
              >
                {{ triggering === sched.id ? 'sending...' : 'send now' }}
              </button>
              <button
                class="text-t-blue/70 hover:text-t-blue text-xs transition-colors"
                @click="openEdit(sched)"
              >
                edit
              </button>
              <template v-if="confirmDelete !== sched.id">
                <button
                  class="text-t-red/70 hover:text-t-red text-xs transition-colors"
                  @click="confirmDelete = sched.id"
                >
                  delete
                </button>
              </template>
              <template v-else>
                <button class="text-t-red hover:brightness-125 text-xs font-semibold" @click="deleteSchedule(sched.id)">yes</button>
                <button class="text-t-fg-dark hover:text-t-fg text-xs" @click="confirmDelete = null">no</button>
              </template>
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="schedules.length === 0" class="px-5 py-10 text-center">
          <p class="text-t-fg-dark text-sm">no summary schedules configured</p>
          <button
            class="text-t-yellow mt-2 text-sm hover:brightness-125"
            @click="openCreate"
          >
            create your first schedule
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
              <h3 class="text-t-fg text-sm font-semibold">{{ editing ? 'Edit Schedule' : 'Add Schedule' }}</h3>
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
                  placeholder="e.g. daily-ops-summary"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 text-sm outline-none"
                />
              </label>

              <!-- Enabled -->
              <label class="flex items-center gap-2">
                <input v-model="formEnabled" type="checkbox" class="accent-t-yellow" />
                <span class="text-t-fg-dark text-sm">Enabled</span>
              </label>

              <!-- Schedule section -->
              <div class="border-t-border space-y-3 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider">Schedule</span>

                <!-- Frequency -->
                <div class="flex gap-2">
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formFrequency === 'daily' ? 'border-t-green text-t-green' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                    @click="formFrequency = 'daily'"
                  >
                    Daily
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formFrequency === 'weekly' ? 'border-t-blue text-t-blue' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                    @click="formFrequency = 'weekly'"
                  >
                    Weekly
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="formFrequency === 'monthly' ? 'border-t-purple text-t-purple' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                    @click="formFrequency = 'monthly'"
                  >
                    Monthly
                  </button>
                </div>

                <div class="grid grid-cols-2 gap-3">
                  <!-- Day of week (weekly) -->
                  <label v-if="formFrequency === 'weekly'" class="block">
                    <span class="text-t-fg-dark text-xs">Day of Week</span>
                    <select
                      v-model.number="formDayOfWeek"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    >
                      <option v-for="(label, idx) in dayOfWeekLabels" :key="idx" :value="idx">{{ label }}</option>
                    </select>
                  </label>

                  <!-- Day of month (monthly) -->
                  <label v-if="formFrequency === 'monthly'" class="block">
                    <span class="text-t-fg-dark text-xs">Day of Month</span>
                    <select
                      v-model.number="formDayOfMonth"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    >
                      <option v-for="d in 28" :key="d" :value="d">{{ d }}</option>
                    </select>
                  </label>

                  <!-- Time -->
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Time</span>
                    <input
                      v-model="formTimeOfDay"
                      type="time"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>

                  <!-- Timezone -->
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Timezone</span>
                    <select
                      v-model="formTimezone"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    >
                      <option v-for="tz in timezones" :key="tz" :value="tz">{{ tz }}</option>
                    </select>
                  </label>
                </div>
              </div>

              <!-- Scope section -->
              <div class="border-t-border space-y-3 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider">Scope</span>

                <!-- Event kinds (multi-select) -->
                <div>
                  <span class="text-t-fg-dark mb-1.5 block text-xs">Event Kinds</span>
                  <div class="flex gap-2">
                    <button
                      v-if="features.srvlog"
                      class="border px-3 py-1.5 text-sm transition-all"
                      :class="formEventKinds.includes('srvlog') ? 'border-t-teal text-t-teal' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                      @click="toggleEventKind('srvlog')"
                    >
                      Srvlog
                    </button>
                    <button
                      v-if="features.netlog"
                      class="border px-3 py-1.5 text-sm transition-all"
                      :class="formEventKinds.includes('netlog') ? 'border-t-fuchsia text-t-fuchsia' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                      @click="toggleEventKind('netlog')"
                    >
                      Netlog
                    </button>
                    <button
                      v-if="features.applog"
                      class="border px-3 py-1.5 text-sm transition-all"
                      :class="formEventKinds.includes('applog') ? 'border-t-magenta text-t-magenta' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
                      @click="toggleEventKind('applog')"
                    >
                      AppLog
                    </button>
                  </div>
                </div>

                <div class="grid grid-cols-2 gap-3">
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Severity (max)</span>
                    <select
                      v-model="formSeverityMax"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    >
                      <option value="">all</option>
                      <option v-for="opt in severityOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                    </select>
                  </label>
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Hostname (optional)</span>
                    <input
                      v-model="formHostname"
                      type="text"
                      placeholder="scope to specific host"
                      class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Top N Issues</span>
                    <input
                      v-model.number="formTopN"
                      type="number"
                      min="1"
                      max="100"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>
                </div>
              </div>

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
            </div>

            <!-- Modal footer -->
            <div class="border-t-border flex items-center gap-3 border-t px-5 py-3">
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="saving"
                @click="saveSchedule"
              >
                {{ saving ? 'saving...' : editing ? 'save changes' : 'create schedule' }}
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
