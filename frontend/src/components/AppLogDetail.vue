<script setup lang="ts">
import type { AppLogEvent } from '@/types/applog'
import { levelBorderClass, levelColorClass } from '@/lib/applog-constants'
import { highlightAttrs } from '@/lib/format'

const props = defineProps<{
  event: AppLogEvent
}>()

const fields: { label: string; key: keyof AppLogEvent; color?: string }[] = [
  { label: 'received', key: 'received_at' },
  { label: 'timestamp', key: 'timestamp' },
  { label: 'level', key: 'level' },
  { label: 'host', key: 'host', color: 'text-t-teal' },
  { label: 'service', key: 'service', color: 'text-t-purple' },
  { label: 'component', key: 'component', color: 'text-t-yellow' },
  { label: 'source', key: 'source', color: 'text-t-blue' },
]

const borderClass = levelBorderClass[props.event.level] ?? 'border-t-border'
const lvlClass = levelColorClass[props.event.level] ?? 'text-t-fg'

function fieldColor(field: (typeof fields)[number]): string {
  if (field.key === 'level') return lvlClass
  return field.color ?? 'text-t-fg'
}

</script>

<template>
  <div
    class="bg-t-bg-dark relative border mx-2 my-1 rounded py-1.5 pl-4 pr-4"
    :class="borderClass"
  >
    <!-- permalink -->
    <RouterLink
      :to="{ name: 'applog-detail', params: { id: event.id } }"
      class="absolute right-3 top-1.5 text-xs font-normal leading-none text-t-purple transition-all hover:font-extrabold hover:brightness-125"
      title="permalink"
      @click.stop
    >
      Details
    </RouterLink>

    <!-- fields -->
    <div
      v-for="field in fields"
      :key="field.key"
      class="flex gap-2 py-0.5 text-sm"
    >
      <span class="text-t-fg-dark w-24 shrink-0 text-right">{{ field.label }}</span>
      <span class="min-w-0 break-all" :class="fieldColor(field)">{{ event[field.key] ?? '–' }}</span>
    </div>

    <!-- message -->
    <div class="flex gap-2 py-0.5 text-sm">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">message</span>
      <span class="text-t-fg min-w-0 break-all font-mono text-xs">{{ event.msg }}</span>
    </div>

    <!-- attrs -->
    <div v-if="event.attrs && Object.keys(event.attrs).length > 0" class="flex gap-2 py-0.5 text-sm">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">attrs</span>
      <pre class="language-json text-t-fg min-w-0 break-all font-mono text-xs" v-html="highlightAttrs(event.attrs)"></pre>
    </div>
  </div>
</template>
