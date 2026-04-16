<script setup lang="ts">
import type { SeverityCount } from '@/types/stats'
import { severityColorClassByLabel, severityBgClassByLabel } from '@/lib/constants'
import { formatNumber } from '@/lib/format'

defineProps<{
  items: SeverityCount[]
  title?: string
  collapsible?: boolean
}>()

const collapsed = defineModel<boolean>('collapsed', { default: false })
</script>

<template>
  <div class="bg-t-bg-dark border-t-border rounded border p-4">
    <div class="mb-3 flex items-center justify-between">
      <h3 class="text-t-teal text-xs font-semibold uppercase tracking-wide">{{ title ?? 'Severity Distribution' }}</h3>
      <button
        v-if="collapsible"
        class="text-t-fg-dark hover:text-t-fg transition-colors"
        :aria-label="collapsed ? 'Expand severity breakdown' : 'Collapse severity breakdown'"
        :aria-expanded="!collapsed"
        @click="collapsed = !collapsed"
      >
        <svg class="h-4 w-4 transition-transform" :class="{ '-rotate-90': collapsed }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9" /></svg>
      </button>
    </div>
    <div v-if="!collapsed" class="space-y-2">
      <div
        v-for="item in items"
        :key="item.severity"
        class="group flex items-center gap-2"
      >
        <span class="w-16 shrink-0 text-xs uppercase" :class="severityColorClassByLabel[item.label] ?? 'text-t-fg'">{{ item.label }}</span>
        <div class="bg-t-bg-highlight h-2 flex-1 overflow-hidden rounded">
          <div
            class="h-full rounded transition-all group-hover:opacity-80"
            :class="severityBgClassByLabel[item.label] ?? 'bg-t-fg'"
            :style="{ width: `${Math.min(item.pct * 1.3, 100)}%`, opacity: 0.7 }"
          ></div>
        </div>
        <span class="text-t-fg-dark w-8 text-right text-xs">{{ item.pct.toFixed(0) }}%</span>
        <span class="text-t-fg w-10 text-right text-xs">{{ formatNumber(item.count) }}</span>
      </div>
    </div>
  </div>
</template>
