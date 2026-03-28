<script setup lang="ts">
import { onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useHostsStore } from '@/stores/hosts'
import { features } from '@/config'
import { formatNumber, formatRelativeTime, lastSeenColorClass } from '@/lib/format'
import { severityBgClassByLabel } from '@/lib/constants'
import LoadingIndicator from '@/components/LoadingIndicator.vue'
import type { HostEntry, HourlyBucket } from '@/types/host'
import type { SeverityCount } from '@/types/stats'

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

const sortOptions = [
  { label: 'Errors', value: 'errors' as const },
  { label: 'Total', value: 'total' as const },
  { label: 'Hostname', value: 'hostname' as const },
  { label: 'Last Seen', value: 'last_seen' as const },
  { label: 'Trend', value: 'trend' as const },
]

// Group hosts by the selected groupBy field.
const groupedHosts = computed(() => {
  const hosts = store.filteredHosts
  if (store.groupBy === 'none') return [{ key: '', hosts }]

  const map = new Map<string, HostEntry[]>()
  for (const h of hosts) {
    const key = store.groupBy === 'feed' ? h.feed : h.status
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(h)
  }
  return [...map.entries()].map(([key, hosts]) => ({ key, hosts }))
})

function goToDevice(host: HostEntry) {
  if (host.feed === 'netlog' && features.netlog) {
    router.push({ name: 'netlog-device-detail', params: { hostname: host.hostname } })
  } else {
    router.push({ name: 'srvlog-device-detail', params: { hostname: host.hostname } })
  }
}

function statusColor(status: string): string {
  switch (status) {
    case 'critical': return 'text-t-red'
    case 'warning': return 'text-t-yellow'
    default: return 'text-t-green'
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

// Build a sparkline string from hourly buckets using Unicode block characters.
function sparkline(buckets: HourlyBucket[]): string {
  if (!buckets.length) return ''
  const blocks = ['\u2581', '\u2582', '\u2583', '\u2584', '\u2585', '\u2586', '\u2587', '\u2588']
  const max = Math.max(...buckets.map((b) => b.count))
  if (max === 0) return '\u2581'.repeat(buckets.length)
  return buckets
    .map((b) => {
      const idx = Math.min(Math.floor((b.count / max) * 7), 7)
      return blocks[idx]
    })
    .join('')
}

// Build a compact severity bar as percentages for the inline bar.
function severityBarSegments(breakdown: SeverityCount[]): { label: string; pct: number }[] {
  return breakdown.filter((s) => s.pct > 0).map((s) => ({ label: s.label, pct: s.pct }))
}

function onKeydown(e: KeyboardEvent) {
  if (e.code === 'Escape') store.collapseAll()
}

onMounted(() => {
  store.startRefresh()
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  store.stopRefresh()
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="mx-auto max-w-7xl px-4 py-4">
    <!-- Header -->
    <div class="mb-4 flex items-center justify-between">
      <h1 class="text-t-fg text-sm font-semibold uppercase tracking-wide">Hosts</h1>
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

    <!-- Status cards -->
    <div class="mb-4 grid grid-cols-2 gap-2 md:grid-cols-4">
      <button
        v-for="s in (['all', 'healthy', 'warning', 'critical'] as const)"
        :key="s"
        class="bg-t-bg-dark border-t-border rounded border px-3 py-2 text-left transition-colors"
        :class="store.statusFilter === s ? 'border-t-fg' : 'hover:border-t-fg-dark'"
        @click="store.setStatusFilter(s === store.statusFilter ? 'all' : s)"
      >
        <div class="text-t-fg text-sm font-medium">
          {{ s === 'all' ? store.statusCounts.total : store.statusCounts[s] }}
        </div>
        <div class="text-t-fg-dark text-xs capitalize">{{ s === 'all' ? 'total' : s }}</div>
      </button>
    </div>

    <!-- Toolbar -->
    <div class="mb-4 flex flex-wrap items-center gap-2">
      <input
        v-model="store.search"
        type="text"
        placeholder="Filter hostnames..."
        class="bg-t-bg-dark border-t-border text-t-fg placeholder-t-fg-dark rounded border px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-t-purple"
      />
      <select
        v-model="store.sortBy"
        class="bg-t-bg-dark border-t-border text-t-fg rounded border px-2 py-1 text-xs focus:outline-none"
      >
        <option v-for="o in sortOptions" :key="o.value" :value="o.value">Sort: {{ o.label }}</option>
      </select>
      <select
        v-model="store.groupBy"
        class="bg-t-bg-dark border-t-border text-t-fg rounded border px-2 py-1 text-xs focus:outline-none"
      >
        <option value="none">Group: None</option>
        <option value="feed">Group: Feed</option>
        <option value="status">Group: Status</option>
      </select>
      <select
        v-model="store.feedFilter"
        class="bg-t-bg-dark border-t-border text-t-fg rounded border px-2 py-1 text-xs focus:outline-none"
      >
        <option value="all">Feed: All</option>
        <option value="srvlog">Feed: Srvlog</option>
        <option value="netlog">Feed: Netlog</option>
        <option value="both">Feed: Both</option>
      </select>
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

    <!-- Empty state -->
    <div
      v-else-if="!store.filteredHosts.length && store.hosts.length"
      class="bg-t-bg-dark border-t-border rounded border px-6 py-10 text-center text-sm"
    >
      <span class="text-t-fg-dark">No hosts match your filters.</span>
      <button
        class="text-t-purple ml-2 text-sm hover:underline"
        @click="store.search = ''; store.statusFilter = 'all'; store.feedFilter = 'all'"
      >Clear filters</button>
    </div>

    <div
      v-else-if="!store.hosts.length && !store.loading"
      class="bg-t-bg-dark border-t-border rounded border px-6 py-10 text-center text-sm text-t-fg-dark"
    >
      No hosts have sent events in the selected time range.
    </div>

    <!-- Host list -->
    <div v-else class="space-y-1">
      <template v-for="group in groupedHosts" :key="group.key">
        <!-- Group header -->
        <div v-if="group.key" class="text-t-fg-dark mt-3 mb-1 text-xs font-semibold uppercase tracking-wide">
          {{ group.key }}
        </div>

        <div
          v-for="host in group.hosts"
          :key="host.hostname"
          class="bg-t-bg-dark border-t-border rounded border transition-colors hover:border-t-fg-dark"
        >
          <!-- Collapsed row -->
          <button
            class="flex w-full items-center gap-3 px-3 py-2 text-left"
            @click="store.toggle(host.hostname)"
          >
            <!-- Expand arrow -->
            <span class="text-t-fg-dark w-3 shrink-0 text-xs">{{ store.expanded.has(host.hostname) ? '\u25BC' : '\u25B6' }}</span>

            <!-- Hostname -->
            <span class="text-t-fg min-w-0 flex-1 truncate text-xs font-medium">{{ host.hostname }}</span>

            <!-- Feed badges -->
            <span class="flex shrink-0 gap-1">
              <span v-if="host.feed === 'srvlog' || host.feed === 'both'" class="rounded bg-t-teal/20 px-1 text-[10px] text-t-teal">S</span>
              <span v-if="host.feed === 'netlog' || host.feed === 'both'" class="rounded bg-t-fuchsia/20 px-1 text-[10px] text-t-fuchsia">N</span>
            </span>

            <!-- Status -->
            <span class="w-14 shrink-0 text-right text-[10px] font-semibold uppercase" :class="statusColor(host.status)">{{ host.status }}</span>

            <!-- Trend -->
            <span class="w-12 shrink-0 text-right text-xs" :class="trendColor(host.trend)">
              {{ trendArrow(host.trend) }} {{ Math.abs(host.trend).toFixed(0) }}%
            </span>
          </button>

          <!-- Second line (always visible) -->
          <div class="flex items-center gap-3 px-3 pb-2 pl-9">
            <span class="text-t-fg-dark text-xs">
              <span v-if="host.error_count" class="text-t-red">{{ formatNumber(host.error_count) }} errors</span>
              <span v-else class="text-t-green">0 errors</span>
              <span> / {{ formatNumber(host.total_count) }} total</span>
            </span>
            <span class="text-t-fg-dark flex-1 text-right text-xs">
              <span v-if="host.last_seen_at" :class="lastSeenColorClass(host.last_seen_at)">{{ formatRelativeTime(host.last_seen_at) }}</span>
              <span v-else>—</span>
            </span>
            <!-- Severity bar -->
            <div class="h-2 w-20 shrink-0 overflow-hidden rounded bg-t-bg-highlight">
              <div class="flex h-full">
                <div
                  v-for="seg in severityBarSegments(host.severity_breakdown)"
                  :key="seg.label"
                  class="h-full"
                  :class="severityBgClassByLabel[seg.label] ?? 'bg-t-fg'"
                  :style="{ width: seg.pct + '%', opacity: 0.7 }"
                ></div>
              </div>
            </div>
          </div>

          <!-- Expanded detail -->
          <div v-if="store.expanded.has(host.hostname)" class="border-t-border border-t px-3 py-3 pl-9">
            <div class="grid gap-4 md:grid-cols-2">
              <!-- Left: sparklines -->
              <div>
                <div class="text-t-fg-dark mb-1 text-[10px] uppercase tracking-wide">Activity (24h)</div>
                <div class="text-t-teal font-mono text-sm leading-tight tracking-tighter">{{ sparkline(host.hourly_buckets) }}</div>

                <!-- Severity breakdown -->
                <div class="mt-3">
                  <div class="text-t-fg-dark mb-1 text-[10px] uppercase tracking-wide">Severity</div>
                  <div class="space-y-1">
                    <div
                      v-for="s in host.severity_breakdown"
                      :key="s.severity"
                      class="flex items-center gap-2"
                    >
                      <span class="text-t-fg-dark w-14 text-[10px] uppercase">{{ s.label }}</span>
                      <div class="bg-t-bg-highlight h-1.5 flex-1 overflow-hidden rounded">
                        <div
                          class="h-full rounded"
                          :class="severityBgClassByLabel[s.label] ?? 'bg-t-fg'"
                          :style="{ width: Math.min(s.pct * 1.3, 100) + '%', opacity: 0.7 }"
                        ></div>
                      </div>
                      <span class="text-t-fg-dark w-8 text-right text-[10px]">{{ formatNumber(s.count) }}</span>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Right: top errors + links -->
              <div>
                <div class="text-t-fg-dark mb-1 text-[10px] uppercase tracking-wide">Top Errors</div>
                <div v-if="host.top_errors.length" class="space-y-1">
                  <div
                    v-for="err in host.top_errors"
                    :key="err.name"
                    class="flex items-center gap-2"
                  >
                    <span class="text-t-fg min-w-0 flex-1 truncate font-mono text-[10px]">{{ err.name }}</span>
                    <span class="text-t-fg-dark shrink-0 text-[10px]">{{ formatNumber(err.count) }}</span>
                  </div>
                </div>
                <div v-else class="text-t-fg-dark text-[10px]">No error patterns in the last 24h</div>

                <!-- Device links -->
                <div class="mt-3 flex gap-2">
                  <button
                    class="text-t-teal rounded border border-t-teal/30 px-2 py-1 text-[10px] transition-colors hover:bg-t-teal/10"
                    @click.stop="goToDevice(host)"
                  >View Device &rarr;</button>
                  <button
                    v-if="(host.feed === 'both' || host.feed === 'srvlog') && features.srvlog"
                    class="text-t-teal rounded border border-t-teal/30 px-2 py-1 text-[10px] transition-colors hover:bg-t-teal/10"
                    @click.stop="router.push({ name: 'srvlog-device-detail', params: { hostname: host.hostname } })"
                  >Srvlog</button>
                  <button
                    v-if="(host.feed === 'both' || host.feed === 'netlog') && features.netlog"
                    class="text-t-fuchsia rounded border border-t-fuchsia/30 px-2 py-1 text-[10px] transition-colors hover:bg-t-fuchsia/10"
                    @click.stop="router.push({ name: 'netlog-device-detail', params: { hostname: host.hostname } })"
                  >Netlog</button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- Footer -->
    <div class="text-t-fg-dark mt-3 flex items-center justify-between text-xs">
      <span>{{ store.filteredHosts.length }} host{{ store.filteredHosts.length === 1 ? '' : 's' }}</span>
      <span v-if="store.loading" class="animate-pulse">refreshing...</span>
    </div>
  </div>
</template>
