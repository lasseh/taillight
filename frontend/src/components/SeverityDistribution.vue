<script setup lang="ts">
import type { SeverityCount } from '@/types/stats'
import { severityColorClassByLabel, severityBgClassByLabel } from '@/lib/constants'
import { formatNumber } from '@/lib/format'

defineProps<{
  items: SeverityCount[]
  title?: string
}>()
</script>

<template>
  <div class="bg-t-bg-dark border-t-border rounded border p-4">
    <h3 class="text-t-teal mb-3 text-xs font-semibold uppercase tracking-wide">{{ title ?? 'Severity Distribution' }}</h3>
    <div class="space-y-2">
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
