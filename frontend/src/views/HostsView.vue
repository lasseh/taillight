<script setup lang="ts">
import { onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useHostsStore } from '@/stores/hosts'
import { features } from '@/config'
import { formatNumber, formatRelativeTime, lastSeenColorClass } from '@/lib/format'
import LoadingIndicator from '@/components/LoadingIndicator.vue'
import type { HostEntry } from '@/types/host'

const router = useRouter()
const store = useHostsStore()

const rangePresets = [
  { label: '1h', value: '1h' },
  { label: '6h', value: '6h' },
  { label: '12h', value: '12h' },
  { label: '24h', value: '24h' },
  { label: '7d', value: '7d' },
  { label: '30d', value: '30d' },
]

// Max total across all hosts — used to scale bars relative to each other.
const maxTotal = computed(() => {
  let max = 0
  for (const h of store.filteredHosts) {
    if (h.total_count > max) max = h.total_count
  }
  return max || 1
})

function goToDevice(host: HostEntry) {
  if (host.feed === 'netlog' && features.netlog) {
    router.push({ name: 'netlog-device-detail', params: { hostname: host.hostname } })
  } else {
    router.push({ name: 'srvlog-device-detail', params: { hostname: host.hostname } })
  }
}

function barPct(count: number): number {
  return (count / maxTotal.value) * 100
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

    <!-- Host bars -->
    <div v-else class="flex-1 overflow-y-auto">
      <div class="space-y-px">
        <div
          v-for="host in store.filteredHosts"
          :key="host.hostname"
          class="group relative flex cursor-pointer items-center gap-2 py-1 transition-colors hover:bg-t-bg-dark"
          @click="goToDevice(host)"
        >
          <!-- Left: hostname + feed + last seen -->
          <div class="flex w-48 shrink-0 items-center gap-1.5 pl-2">
            <span class="text-t-fg min-w-0 flex-1 truncate text-xs font-medium">{{ host.hostname }}</span>
            <span v-if="host.feed === 'srvlog' || host.feed === 'both'" class="rounded bg-t-teal/20 px-0.5 text-[9px] text-t-teal">S</span>
            <span v-if="host.feed === 'netlog' || host.feed === 'both'" class="rounded bg-t-fuchsia/20 px-0.5 text-[9px] text-t-fuchsia">N</span>
          </div>

          <!-- Bar area: fills remaining width -->
          <div class="relative flex-1">
            <!-- Total bar (teal, background) -->
            <div
              class="h-4 rounded-sm bg-t-teal/20 transition-all"
              :style="{ width: barPct(host.total_count) + '%' }"
            >
              <!-- Error bar (red, overlaid on the left of the total bar) -->
              <div
                v-if="host.error_count > 0"
                class="absolute left-0 top-0 h-4 rounded-sm bg-t-red/40"
                :style="{ width: barPct(host.error_count) + '%' }"
              ></div>
            </div>

            <!-- Labels overlaid on the bar -->
            <div class="pointer-events-none absolute inset-0 flex items-center gap-3 px-2">
              <span class="text-t-fg text-[10px] font-medium">{{ formatNumber(host.total_count) }}</span>
              <span v-if="host.error_count" class="text-t-red text-[10px]">{{ formatNumber(host.error_count) }} err</span>
            </div>
          </div>

          <!-- Right: trend + last seen -->
          <div class="flex w-28 shrink-0 items-center justify-end gap-3 pr-2">
            <span class="text-[10px]" :class="trendColor(host.trend)">{{ trendArrow(host.trend) }}{{ Math.abs(host.trend).toFixed(0) }}%</span>
            <span v-if="host.last_seen_at" class="w-12 text-right text-[10px]" :class="lastSeenColorClass(host.last_seen_at)">{{ formatRelativeTime(host.last_seen_at) }}</span>
            <span v-else class="text-t-fg-dark w-12 text-right text-[10px]">&mdash;</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
