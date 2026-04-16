<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useHostsStore } from '@/stores/hosts'
import { features as getFeatures } from '@/lib/features'
import { formatNumber, formatRelativeTime, lastSeenColorClass } from '@/lib/format'
import LoadingIndicator from '@/components/LoadingIndicator.vue'
import type { HostEntry, HourlyBucket } from '@/types/host'

const router = useRouter()
const store = useHostsStore()
const features = getFeatures()

const rangePresets = [
  { label: '1h', value: '1h' },
  { label: '6h', value: '6h' },
  { label: '12h', value: '12h' },
  { label: '24h', value: '24h' },
  { label: '7d', value: '7d' },
  { label: '30d', value: '30d' },
]

function goToDevice(host: HostEntry) {
  if (host.feed === 'netlog' && features.netlog) {
    router.push({ name: 'netlog-device-detail', params: { hostname: host.hostname } })
  } else {
    router.push({ name: 'srvlog-device-detail', params: { hostname: host.hostname } })
  }
}

function statusDotClass(status: string): string {
  switch (status) {
    case 'critical': return 'bg-t-red'
    case 'warning': return 'bg-t-yellow'
    default: return 'bg-t-green'
  }
}

function trendArrow(trend: number): string {
  if (trend > 1) return '\u25B2'
  if (trend < -1) return '\u25BC'
  return '\u2501'
}

function trendColor(trend: number): string {
  if (trend > 1) return 'text-t-red'
  if (trend < -1) return 'text-t-green'
  return 'text-t-fg-dark'
}

function errorRatio(host: HostEntry): string {
  if (host.total_count === 0) return '0'
  const pct = (host.error_count / host.total_count) * 100
  if (pct < 0.1 && pct > 0) return '<0.1'
  return pct.toFixed(1)
}

// Render sparkline as array of bar heights (0-1) for CSS rendering.
function sparkBars(buckets: HourlyBucket[]): { height: number; hasErrors: boolean }[] {
  if (!buckets.length) return []
  const max = Math.max(...buckets.map((b) => b.count))
  if (max === 0) return buckets.map(() => ({ height: 0.05, hasErrors: false }))
  return buckets.map((b) => ({
    height: Math.max(b.count / max, 0.05),
    hasErrors: b.error_count > 0,
  }))
}

function cycleSortBy() {
  const order: ('errors' | 'total' | 'hostname' | 'last_seen' | 'trend')[] = ['errors', 'total', 'hostname', 'last_seen', 'trend']
  const idx = order.indexOf(store.sortBy)
  const next = order[(idx + 1) % order.length]
  if (next) store.sortBy = next
}

onMounted(() => store.startRefresh())
onUnmounted(() => store.stopRefresh())
</script>

<template>
  <div class="flex h-full flex-col overflow-hidden px-3 py-3">
    <!-- Header -->
    <div class="mb-3 flex items-center gap-3">
      <h1 class="text-t-fg text-sm font-semibold uppercase tracking-wide">Hosts</h1>

      <input
        v-model="store.search"
        type="text"
        placeholder="Filter hostnames..."
        class="bg-t-bg-dark border-t-border text-t-fg placeholder-t-fg-dark w-44 rounded border px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-t-purple"
      />

      <button
        class="text-t-fg-dark rounded px-1.5 py-0.5 text-xs transition-colors hover:text-t-fg"
        @click="cycleSortBy"
      >sort: {{ store.sortBy.replace('_', ' ') }}</button>

      <div class="flex-1"></div>

      <span class="text-t-fg-dark text-xs">{{ store.filteredHosts.length }} hosts</span>
      <span v-if="store.loading" class="text-t-fg-dark animate-pulse text-xs">refreshing...</span>

      <div class="flex gap-1">
        <button
          v-for="p in rangePresets"
          :key="p.value"
          class="rounded px-1.5 py-0.5 text-xs transition-colors"
          :class="store.range_ === p.value ? 'bg-t-bg-highlight text-t-purple' : 'text-t-fg-dark hover:text-t-fg'"
          @click="store.setRange(p.value)"
        >{{ p.label }}</button>
      </div>
    </div>

    <!-- Loading -->
    <LoadingIndicator v-if="store.loading && !store.hosts.length" />

    <!-- Error -->
    <div
      v-else-if="store.error"
      class="bg-t-bg-dark border-t-border rounded border px-6 py-10 text-center text-sm text-t-red"
    >
      Failed to load hosts: {{ store.error }}
    </div>

    <!-- Empty -->
    <div
      v-else-if="!store.filteredHosts.length && store.hosts.length"
      class="bg-t-bg-dark border-t-border rounded border px-6 py-10 text-center text-sm"
    >
      <span class="text-t-fg-dark">No hosts match your filter.</span>
      <button class="text-t-purple ml-2 hover:underline" @click="store.search = ''">Clear</button>
    </div>

    <div
      v-else-if="!store.hosts.length && !store.loading"
      class="bg-t-bg-dark border-t-border rounded border px-6 py-10 text-center text-sm text-t-fg-dark"
    >
      No hosts have sent events in the selected time range.
    </div>

    <!-- Host table -->
    <div v-else class="flex-1 overflow-y-auto">
      <table class="w-full border-collapse">
        <thead class="bg-t-bg sticky top-0 z-10">
          <tr class="border-t-border border-b text-left text-[10px] uppercase tracking-wide text-t-fg-dark">
            <th class="w-3 py-1.5 pl-3 pr-0 font-medium"></th>
            <th class="py-1.5 pl-2 pr-2 font-medium">Hostname</th>
            <th class="px-2 font-medium">Feed</th>
            <th class="px-2 text-right font-medium">Total</th>
            <th class="px-2 text-right font-medium">Errors</th>
            <th class="px-2 text-right font-medium">Err%</th>
            <th class="px-2 text-right font-medium">Trend</th>
            <th class="hidden px-2 font-medium md:table-cell">Activity (24h)</th>
            <th class="px-2 pr-3 text-right font-medium">Last Seen</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="host in store.filteredHosts"
            :key="host.hostname"
            class="border-t-border hover:bg-t-bg-dark cursor-pointer border-b transition-colors"
            @click="goToDevice(host)"
          >
            <!-- Status dot -->
            <td class="py-1.5 pl-3 pr-0">
              <div class="h-2 w-2 rounded-full" :class="statusDotClass(host.status)"></div>
            </td>

            <!-- Hostname -->
            <td class="py-1.5 pl-2 pr-2">
              <span class="text-t-fg text-xs font-medium">{{ host.hostname }}</span>
            </td>

            <!-- Feed -->
            <td class="px-2">
              <span class="flex gap-0.5">
                <span v-if="host.feed === 'srvlog' || host.feed === 'both'" class="rounded bg-t-teal/20 px-1 text-[10px] text-t-teal">S</span>
                <span v-if="host.feed === 'netlog' || host.feed === 'both'" class="rounded bg-t-fuchsia/20 px-1 text-[10px] text-t-fuchsia">N</span>
              </span>
            </td>

            <!-- Total -->
            <td class="px-2 text-right">
              <span class="text-t-fg text-xs">{{ formatNumber(host.total_count) }}</span>
            </td>

            <!-- Errors -->
            <td class="px-2 text-right">
              <span class="text-xs" :class="host.error_count ? 'text-t-red' : 'text-t-fg-dark'">{{ formatNumber(host.error_count) }}</span>
            </td>

            <!-- Error ratio -->
            <td class="px-2 text-right">
              <span class="text-xs" :class="host.error_count ? 'text-t-orange' : 'text-t-fg-dark'">{{ errorRatio(host) }}%</span>
            </td>

            <!-- Trend -->
            <td class="px-2 text-right">
              <span class="text-xs" :class="trendColor(host.trend)">{{ trendArrow(host.trend) }} {{ Math.abs(host.trend).toFixed(0) }}%</span>
            </td>

            <!-- Activity sparkline -->
            <td class="hidden px-2 md:table-cell">
              <div class="flex h-4 items-end gap-px">
                <div
                  v-for="(bar, i) in sparkBars(host.hourly_buckets)"
                  :key="i"
                  class="w-1.5 rounded-t-sm"
                  :class="bar.hasErrors ? 'bg-t-red/60' : 'bg-t-teal/40'"
                  :style="{ height: (bar.height * 100) + '%' }"
                ></div>
              </div>
            </td>

            <!-- Last seen -->
            <td class="px-2 pr-3 text-right">
              <span v-if="host.last_seen_at" class="text-xs" :class="lastSeenColorClass(host.last_seen_at)">{{ formatRelativeTime(host.last_seen_at) }}</span>
              <span v-else class="text-t-fg-dark text-xs">&mdash;</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
