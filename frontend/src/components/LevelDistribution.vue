<script setup lang="ts">
import { computed } from 'vue'
import type { LevelCount } from '@/types/stats'
import { LEVEL_RANK, levelColorClass, levelBgColorClass } from '@/lib/applog-constants'
import { formatNumber } from '@/lib/format'

const props = defineProps<{
  items: LevelCount[]
  title?: string
}>()

const sorted = computed(() =>
  [...props.items].sort((a, b) => (LEVEL_RANK[a.level] ?? 99) - (LEVEL_RANK[b.level] ?? 99))
)
</script>

<template>
  <div class="bg-t-bg-dark border-t-border rounded border p-4">
    <h3 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">{{ title ?? 'Level Distribution' }}</h3>
    <div class="space-y-2">
      <div
        v-for="item in sorted"
        :key="item.level"
        class="group flex items-center gap-2"
      >
        <span class="w-16 shrink-0 text-xs uppercase" :class="levelColorClass[item.level] ?? 'text-t-fg'">{{ item.level }}</span>
        <div class="bg-t-bg-highlight h-2 flex-1 overflow-hidden rounded">
          <div
            class="h-full rounded transition-all group-hover:opacity-80"
            :class="levelBgColorClass[item.level] ?? 'bg-t-fg'"
            :style="{ width: `${Math.min(item.pct * 1.3, 100)}%`, opacity: 0.7 }"
          ></div>
        </div>
        <span class="text-t-fg-dark w-8 text-right text-xs">{{ item.pct.toFixed(0) }}%</span>
        <span class="text-t-fg w-10 text-right text-xs">{{ formatNumber(item.count) }}</span>
      </div>
    </div>
  </div>
</template>
