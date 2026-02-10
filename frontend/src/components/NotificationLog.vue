<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, ApiError } from '@/lib/api'
import type { NotificationLogEntry, NotificationChannel, NotificationRule } from '@/types/notification'

const entries = ref<NotificationLogEntry[]>([])
const channels = ref<NotificationChannel[]>([])
const rules = ref<NotificationRule[]>([])
const loading = ref(true)
const loadError = ref('')

// Filter state.
const filterRuleID = ref('')
const filterChannelID = ref('')
const filterStatus = ref('')
const filterRange = ref('24h')

const rangeOptions = [
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
]

function channelName(id: number): string {
  return channels.value.find(c => c.id === id)?.name || `#${id}`
}

function ruleName(id: number): string {
  return rules.value.find(r => r.id === id)?.name || `#${id}`
}

function formatDate(ts: string): string {
  return new Date(ts).toLocaleString(undefined, {
    month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  })
}

function statusClass(status: string): string {
  switch (status) {
    case 'sent': return 'bg-t-green/10 text-t-green'
    case 'failed': return 'bg-t-red/10 text-t-red'
    case 'rate_limited': return 'bg-t-yellow/10 text-t-yellow'
    case 'circuit_open': return 'bg-t-yellow/10 text-t-yellow'
    default: return 'bg-t-fg-gutter/10 text-t-fg-gutter'
  }
}

function buildParams(): URLSearchParams {
  const p = new URLSearchParams()
  if (filterRuleID.value) p.set('rule_id', filterRuleID.value)
  if (filterChannelID.value) p.set('channel_id', filterChannelID.value)
  if (filterStatus.value) p.set('status', filterStatus.value)

  const now = new Date()
  let from: Date
  switch (filterRange.value) {
    case '1h': from = new Date(now.getTime() - 3600000); break
    case '6h': from = new Date(now.getTime() - 6 * 3600000); break
    case '24h': from = new Date(now.getTime() - 24 * 3600000); break
    case '7d': from = new Date(now.getTime() - 7 * 24 * 3600000); break
    case '30d': from = new Date(now.getTime() - 30 * 24 * 3600000); break
    default: from = new Date(now.getTime() - 24 * 3600000)
  }
  p.set('from', from.toISOString())
  p.set('to', now.toISOString())

  return p
}

async function fetchData() {
  loading.value = true
  loadError.value = ''
  try {
    const [logRes, channelsRes, rulesRes] = await Promise.all([
      api.listNotificationLog(buildParams()),
      api.listChannels(),
      api.listRules(),
    ])
    entries.value = logRes.data
    channels.value = channelsRes.data
    rules.value = rulesRes.data
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : 'Failed to load log'
  } finally {
    loading.value = false
  }
}

async function applyFilters() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await api.listNotificationLog(buildParams())
    entries.value = res.data
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : 'Failed to load log'
  } finally {
    loading.value = false
  }
}

onMounted(fetchData)
</script>

<template>
  <div class="space-y-4">
    <!-- Filters -->
    <div class="bg-t-bg-dark border-t-border flex flex-wrap items-end gap-3 rounded border px-5 py-3">
      <label class="block">
        <span class="text-t-fg-dark text-xs">Time Range</span>
        <div class="mt-1 flex gap-1">
          <button
            v-for="opt in rangeOptions"
            :key="opt.value"
            class="border px-2 py-1 text-xs transition-all"
            :class="filterRange === opt.value ? 'border-t-yellow text-t-yellow' : 'border-t-border text-t-fg-dark hover:text-t-fg'"
            @click="filterRange = opt.value; applyFilters()"
          >
            {{ opt.label }}
          </button>
        </div>
      </label>

      <label class="block">
        <span class="text-t-fg-dark text-xs">Rule</span>
        <select
          v-model="filterRuleID"
          class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block border px-2 py-1 text-xs outline-none"
          @change="applyFilters()"
        >
          <option value="">all</option>
          <option v-for="r in rules" :key="r.id" :value="String(r.id)">{{ r.name }}</option>
        </select>
      </label>

      <label class="block">
        <span class="text-t-fg-dark text-xs">Channel</span>
        <select
          v-model="filterChannelID"
          class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block border px-2 py-1 text-xs outline-none"
          @change="applyFilters()"
        >
          <option value="">all</option>
          <option v-for="c in channels" :key="c.id" :value="String(c.id)">{{ c.name }}</option>
        </select>
      </label>

      <label class="block">
        <span class="text-t-fg-dark text-xs">Status</span>
        <select
          v-model="filterStatus"
          class="bg-t-bg border-t-border text-t-fg focus:border-t-yellow mt-1 block border px-2 py-1 text-xs outline-none"
          @change="applyFilters()"
        >
          <option value="">all</option>
          <option value="sent">sent</option>
          <option value="failed">failed</option>
          <option value="rate_limited">rate limited</option>
          <option value="circuit_open">circuit open</option>
        </select>
      </label>
    </div>

    <!-- Loading / error -->
    <div v-if="loading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
    <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>

    <template v-else>
      <!-- Log table -->
      <div class="bg-t-bg-dark border-t-border rounded border">
        <div class="border-t-border border-b px-5 py-2.5">
          <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
            Notification Log
            <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ entries.length }} entries</span>
          </h3>
        </div>

        <!-- Table header -->
        <div v-if="entries.length > 0" class="text-t-fg-gutter border-t-border flex border-b px-5 py-2 text-xs uppercase tracking-wider">
          <span class="w-40 shrink-0">Time</span>
          <span class="w-20 shrink-0">Status</span>
          <span class="w-36 shrink-0">Rule</span>
          <span class="w-28 shrink-0">Channel</span>
          <span class="w-20 shrink-0">Events</span>
          <span class="w-20 shrink-0">Duration</span>
          <span class="min-w-0 flex-1">Reason</span>
        </div>

        <!-- Log rows -->
        <div class="divide-t-border divide-y">
          <div
            v-for="entry in entries"
            :key="entry.id"
            class="hover:bg-t-bg-hover flex items-center px-5 py-2.5 text-sm transition-colors"
          >
            <div class="w-40 shrink-0">
              <span class="text-t-fg-dark text-xs" :title="entry.created_at">{{ formatDate(entry.created_at) }}</span>
            </div>
            <div class="w-20 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="statusClass(entry.status)"
              >
                {{ entry.status }}
              </span>
            </div>
            <div class="w-36 shrink-0">
              <span class="text-t-fg text-xs">{{ ruleName(entry.rule_id) }}</span>
            </div>
            <div class="w-28 shrink-0">
              <span class="text-t-fg-dark text-xs">{{ channelName(entry.channel_id) }}</span>
            </div>
            <div class="w-20 shrink-0">
              <span class="text-t-fg-dark text-xs">{{ entry.event_count }}</span>
            </div>
            <div class="w-20 shrink-0">
              <span class="text-t-fg-dark text-xs">{{ entry.duration_ms }}ms</span>
            </div>
            <div class="min-w-0 flex-1 truncate">
              <span v-if="entry.reason" class="text-t-fg-gutter text-xs">{{ entry.reason }}</span>
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="entries.length === 0" class="px-5 py-10 text-center">
          <p class="text-t-fg-dark text-sm">no log entries for the selected filters</p>
        </div>
      </div>
    </template>
  </div>
</template>
