<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, ApiError } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import { useFocusTrap } from '@/composables/useFocusTrap'
import { feedBadgeClass, feedDisplayLabel } from '@/lib/analysis-format'
import type {
  AnalysisFeed,
  AnalysisFrequency,
  AnalysisSchedule,
  CreateAnalysisScheduleRequest,
} from '@/types/analysis'
import type { NotificationChannel } from '@/types/notification'

const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.is_admin === true)

const schedules = ref<AnalysisSchedule[]>([])
const loading = ref(true)
const loadError = ref('')

const showModal = ref(false)
const editing = ref<AnalysisSchedule | null>(null)
const saving = ref(false)
const saveError = ref('')

const formName = ref('')
const formEnabled = ref(true)
const formFeed = ref<AnalysisFeed>('netlog')
const formFrequency = ref<AnalysisFrequency>('daily')
const formDayOfWeek = ref(1)
const formDayOfMonth = ref(1)
const formTimeOfDay = ref('03:00')
const formTimezone = ref('UTC')
const formNotifyChannelIds = ref<number[]>([])

// Email notification channels available as report recipients. Only email-type
// channels render an analysis report, so the picker is restricted to them.
const emailChannels = ref<NotificationChannel[]>([])

const modalEl = ref<HTMLElement | null>(null)
useFocusTrap(modalEl)

// Bind Escape only while the modal is open so other Escape handlers (filter
// bars etc.) aren't shadowed when the page is idle.
function handleEscape(e: KeyboardEvent) {
  if (e.key === 'Escape') closeModal()
}
watch(showModal, (open) => {
  if (open) {
    window.addEventListener('keydown', handleEscape)
  } else {
    window.removeEventListener('keydown', handleEscape)
  }
})
// Ensure the window listener is torn down if the component unmounts while the
// modal is still open (AnalysisView is not KeepAlive-cached, so this fires).
onUnmounted(() => window.removeEventListener('keydown', handleEscape))

const confirmDelete = ref<number | null>(null)
const deleteError = ref('')

// Keyed by schedule id so triggering several rows in quick succession doesn't
// have one outcome silently overwrite another.
const runResults = ref<Record<number, { success: boolean; message: string }>>({})
const running = ref<number | null>(null)

const enabledSchedules = computed(() => schedules.value.filter((s) => s.enabled))
const disabledSchedules = computed(() => schedules.value.filter((s) => !s.enabled))

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

const dayOfWeekLabels = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
]

const feedOptions: { value: AnalysisFeed; label: string }[] = [
  { value: 'netlog', label: 'Netlog' },
  { value: 'srvlog', label: 'Srvlog' },
  { value: 'all', label: 'All syslog' },
]

async function fetchData() {
  try {
    const res = await api.listAnalysisSchedules()
    schedules.value = res.data
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : 'failed to load schedules'
  } finally {
    loading.value = false
  }
}

async function fetchChannels() {
  try {
    const res = await api.listChannels()
    emailChannels.value = res.data.filter((c) => c.type === 'email')
  } catch {
    // Non-fatal: the recipient picker just shows the "no channels" hint.
    emailChannels.value = []
  }
}

function openCreate() {
  editing.value = null
  formName.value = ''
  formEnabled.value = true
  formFeed.value = 'netlog'
  formFrequency.value = 'daily'
  formDayOfWeek.value = 1
  formDayOfMonth.value = 1
  formTimeOfDay.value = '03:00'
  formTimezone.value = 'UTC'
  formNotifyChannelIds.value = []
  saveError.value = ''
  showModal.value = true
}

function openEdit(s: AnalysisSchedule) {
  editing.value = s
  formName.value = s.name
  formEnabled.value = s.enabled
  formFeed.value = s.feed
  formFrequency.value = s.frequency
  formDayOfWeek.value = s.day_of_week ?? 1
  formDayOfMonth.value = s.day_of_month ?? 1
  formTimeOfDay.value = s.time_of_day
  formTimezone.value = s.timezone
  formNotifyChannelIds.value = [...(s.notify_channel_ids ?? [])]
  saveError.value = ''
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  editing.value = null
  saveError.value = ''
}

function buildPayload(): CreateAnalysisScheduleRequest {
  const payload: CreateAnalysisScheduleRequest = {
    name: formName.value.trim(),
    enabled: formEnabled.value,
    feed: formFeed.value,
    frequency: formFrequency.value,
    time_of_day: formTimeOfDay.value,
    timezone: formTimezone.value,
    notify_channel_ids: [...formNotifyChannelIds.value],
  }
  if (formFrequency.value === 'weekly') payload.day_of_week = formDayOfWeek.value
  if (formFrequency.value === 'monthly') payload.day_of_month = formDayOfMonth.value
  return payload
}

async function saveSchedule() {
  saveError.value = ''
  if (!formName.value.trim()) {
    saveError.value = 'name is required'
    return
  }
  saving.value = true
  try {
    const body = buildPayload()
    if (editing.value) {
      const res = await api.updateAnalysisSchedule(editing.value.id, body)
      const idx = schedules.value.findIndex((s) => s.id === editing.value!.id)
      if (idx >= 0) schedules.value[idx] = res.data
    } else {
      const res = await api.createAnalysisSchedule(body)
      schedules.value.push(res.data)
    }
    closeModal()
  } catch (e) {
    saveError.value = e instanceof ApiError ? e.message : 'failed to save schedule'
  } finally {
    saving.value = false
  }
}

async function deleteSchedule(id: number) {
  deleteError.value = ''
  try {
    await api.deleteAnalysisSchedule(id)
    schedules.value = schedules.value.filter((s) => s.id !== id)
  } catch (e) {
    deleteError.value = e instanceof ApiError ? e.message : 'failed to delete schedule'
  }
  confirmDelete.value = null
}

async function runSchedule(id: number) {
  running.value = id
  delete runResults.value[id]
  try {
    await api.runAnalysisSchedule(id)
    runResults.value = { ...runResults.value, [id]: { success: true, message: 'queued' } }
  } catch (e) {
    let message = 'failed'
    if (e instanceof ApiError) {
      if (e.code === 'duplicate_report') message = 'already pending'
      else if (e.code === 'queue_full') message = 'queue full'
      else if (e.code === 'scheduler_disabled') message = 'scheduler disabled'
      else message = e.message
    }
    runResults.value = { ...runResults.value, [id]: { success: false, message } }
  } finally {
    running.value = null
  }
}

function frequencyBadgeClass(freq: string): string {
  switch (freq) {
    case 'daily':
      return 'bg-t-green/10 text-t-green'
    case 'weekly':
      return 'bg-t-blue/10 text-t-blue'
    case 'monthly':
      return 'bg-t-purple/10 text-t-purple'
    default:
      return 'bg-t-fg-dark/10 text-t-fg-dark'
  }
}

function formatLastRun(ts?: string | null): string {
  if (!ts) return 'never'
  const seconds = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

function toggleChannel(id: number) {
  const idx = formNotifyChannelIds.value.indexOf(id)
  if (idx >= 0) formNotifyChannelIds.value.splice(idx, 1)
  else formNotifyChannelIds.value.push(id)
}

onMounted(() => {
  fetchData()
  fetchChannels()
})
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <p class="text-t-fg-dark text-sm">recurring analysis runs stored in the database</p>
      <button
        v-if="isAdmin"
        class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
        @click="openCreate"
      >
        + add schedule
      </button>
    </div>

    <div v-if="loading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
    <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>

    <template v-else>
      <div v-if="deleteError" class="text-t-red px-5 py-2 text-sm">{{ deleteError }}</div>

      <div class="bg-t-bg-dark border-t-border rounded border">
        <div class="border-t-border border-b px-5 py-2.5">
          <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
            Schedules
            <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{
              schedules.length
            }}</span>
          </h3>
        </div>

        <div
          v-if="schedules.length > 0"
          class="text-t-fg-gutter border-t-border flex border-b px-5 py-2 text-xs uppercase tracking-wider"
        >
          <span class="w-8 shrink-0"></span>
          <span class="w-44 shrink-0">Name</span>
          <span class="w-20 shrink-0">Feed</span>
          <span class="w-24 shrink-0">Frequency</span>
          <span class="w-24 shrink-0">Time</span>
          <span class="min-w-0 flex-1">Last Run</span>
          <span class="w-40 shrink-0 text-right">Actions</span>
        </div>

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
            <div class="w-20 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="feedBadgeClass(sched.feed)"
              >
                {{ feedDisplayLabel(sched.feed) }}
              </span>
            </div>
            <div class="w-24 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="frequencyBadgeClass(sched.frequency)"
              >
                {{ sched.frequency }}
              </span>
            </div>
            <div class="w-24 shrink-0">
              <span class="text-t-fg-dark text-xs">{{ sched.time_of_day }}</span>
              <span class="text-t-fg-gutter ml-1 text-xs">
                {{ sched.timezone === 'UTC' ? 'UTC' : sched.timezone.split('/').pop() }}
              </span>
            </div>
            <div class="min-w-0 flex-1 truncate">
              <span class="text-t-fg-gutter text-xs">{{ formatLastRun(sched.last_run_at) }}</span>
              <span
                v-if="runResults[sched.id] !== undefined"
                class="ml-2 text-xs"
                :class="runResults[sched.id]!.success ? 'text-t-green' : 'text-t-red'"
              >
                {{ runResults[sched.id]!.message }}
              </span>
            </div>
            <div class="flex w-40 shrink-0 items-center justify-end gap-3">
              <template v-if="isAdmin">
                <button
                  class="text-t-orange/70 hover:text-t-orange text-xs transition-colors"
                  :disabled="running === sched.id"
                  @click="runSchedule(sched.id)"
                >
                  {{ running === sched.id ? 'queuing...' : 'run now' }}
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
                  <button
                    class="text-t-red hover:brightness-125 text-xs font-semibold"
                    @click="deleteSchedule(sched.id)"
                  >
                    yes
                  </button>
                  <button
                    class="text-t-fg-dark hover:text-t-fg text-xs"
                    @click="confirmDelete = null"
                  >
                    no
                  </button>
                </template>
              </template>
            </div>
          </div>
        </div>

        <div v-if="schedules.length === 0" class="px-5 py-10 text-center">
          <p class="text-t-fg-dark text-sm">no analysis schedules configured</p>
          <button
            v-if="isAdmin"
            class="text-t-orange mt-2 text-sm hover:brightness-125"
            @click="openCreate"
          >
            create your first schedule
          </button>
        </div>
      </div>
    </template>

    <Teleport to="body">
      <Transition name="modal">
        <div
          v-if="showModal"
          class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 pt-10 pb-10"
          @click.self="closeModal"
        >
          <div
            ref="modalEl"
            class="bg-t-bg-dark border-t-border w-full max-w-2xl rounded border shadow-xl"
          >
            <div class="border-t-border border-b px-5 py-3">
              <h3 class="text-t-fg text-sm font-semibold">
                {{ editing ? 'Edit Schedule' : 'Add Schedule' }}
              </h3>
            </div>

            <div class="space-y-4 px-5 py-4">
              <label class="block">
                <span class="text-t-fg-dark text-sm">Name</span>
                <input
                  v-model="formName"
                  type="text"
                  placeholder="e.g. nightly-netlog"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-orange mt-1 block w-full border px-3 py-2 text-sm outline-none"
                />
              </label>

              <label class="flex items-center gap-2">
                <input v-model="formEnabled" type="checkbox" class="accent-t-orange" />
                <span class="text-t-fg-dark text-sm">Enabled</span>
              </label>

              <div class="border-t-border space-y-3 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider"
                  >Source</span
                >
                <div class="flex flex-wrap gap-2">
                  <button
                    v-for="opt in feedOptions"
                    :key="opt.value"
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formFeed === opt.value
                        ? 'border-t-orange text-t-orange'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg'
                    "
                    @click="formFeed = opt.value"
                  >
                    {{ opt.label }}
                  </button>
                </div>
              </div>

              <div class="border-t-border space-y-3 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider"
                  >Schedule</span
                >

                <div class="flex gap-2">
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formFrequency === 'daily'
                        ? 'border-t-green text-t-green'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg'
                    "
                    @click="formFrequency = 'daily'"
                  >
                    Daily
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formFrequency === 'weekly'
                        ? 'border-t-blue text-t-blue'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg'
                    "
                    @click="formFrequency = 'weekly'"
                  >
                    Weekly
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formFrequency === 'monthly'
                        ? 'border-t-purple text-t-purple'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg'
                    "
                    @click="formFrequency = 'monthly'"
                  >
                    Monthly
                  </button>
                </div>
                <p class="text-t-fg-gutter text-xs">
                  daily cadence uses the daily prompt; weekly and monthly both use the weekly trend
                  prompt.
                </p>

                <div class="grid grid-cols-2 gap-3">
                  <label v-if="formFrequency === 'weekly'" class="block">
                    <span class="text-t-fg-dark text-xs">Day of Week</span>
                    <select
                      v-model.number="formDayOfWeek"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-orange mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    >
                      <option v-for="(label, idx) in dayOfWeekLabels" :key="idx" :value="idx">
                        {{ label }}
                      </option>
                    </select>
                  </label>

                  <label v-if="formFrequency === 'monthly'" class="block">
                    <span class="text-t-fg-dark text-xs">Day of Month</span>
                    <select
                      v-model.number="formDayOfMonth"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-orange mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    >
                      <option v-for="d in 28" :key="d" :value="d">{{ d }}</option>
                    </select>
                  </label>

                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Time</span>
                    <input
                      v-model="formTimeOfDay"
                      type="time"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-orange mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>

                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Timezone</span>
                    <select
                      v-model="formTimezone"
                      class="bg-t-bg border-t-border text-t-fg focus:border-t-orange mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    >
                      <option v-for="tz in timezones" :key="tz" :value="tz">{{ tz }}</option>
                    </select>
                  </label>
                </div>
              </div>

              <div class="border-t-border space-y-3 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider"
                  >Email report to</span
                >
                <p v-if="emailChannels.length === 0" class="text-t-fg-gutter text-xs">
                  no email notification channels configured — add one under Notifications to email
                  this report.
                </p>
                <div v-else class="flex flex-wrap gap-2">
                  <button
                    v-for="ch in emailChannels"
                    :key="ch.id"
                    type="button"
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formNotifyChannelIds.includes(ch.id)
                        ? 'border-t-orange text-t-orange'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg'
                    "
                    @click="toggleChannel(ch.id)"
                  >
                    {{ ch.name }}
                  </button>
                </div>
                <p v-if="emailChannels.length > 0" class="text-t-fg-gutter text-xs">
                  the completed report is mailed to the selected channels; none selected = no email.
                </p>
              </div>

              <div v-if="saveError" class="text-t-red text-sm">{{ saveError }}</div>
            </div>

            <div class="border-t-border flex items-center justify-end gap-3 border-t px-5 py-3">
              <button class="text-t-fg-dark hover:text-t-fg text-sm" @click="closeModal">
                cancel
              </button>
              <button
                class="bg-t-orange/15 text-t-orange hover:brightness-125 border-t-orange/30 border px-4 py-2 text-sm"
                :disabled="saving"
                @click="saveSchedule"
              >
                {{ saving ? 'saving...' : editing ? 'save changes' : 'create schedule' }}
              </button>
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
