<script setup lang="ts">
import { ref, computed, nextTick, toRef, inject, watch } from 'vue'
import type { Ref } from 'vue'
import type { SyslogEvent } from '@/types/syslog'
import { severityColorClass, severityBgClass, severityBgClassByLabel } from '@/lib/constants'
import { highlightMessage } from '@/lib/highlighter'
import { formatTime } from '@/lib/format'
import SyslogDetail from '@/components/SyslogDetail.vue'
import { useSyslogFilterStore } from '@/stores/syslog-filters'

const filterStore = useSyslogFilterStore()

const props = defineProps<{
  event: SyslogEvent
}>()

const expanded = ref(false)
const rowEl = ref<HTMLElement | null>(null)
const collapseSignal = inject<Ref<number>>('collapseSignal')
if (collapseSignal) {
  watch(collapseSignal, () => { expanded.value = false })
}

function toggle() {
  expanded.value = !expanded.value
  if (expanded.value) {
    nextTick(() => {
      rowEl.value?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    })
  }
}

const event = toRef(props, 'event')
const sevClass = computed(() => severityColorClass[event.value.severity] ?? 'text-t-fg')
const sevBgClass = computed(() => severityBgClass[event.value.severity] ?? '')
const sevBarClass = computed(() => severityBgClassByLabel[event.value.severity_label] ?? 'bg-sev-info')

const highlightedMessage = computed(() =>
  highlightMessage(event.value.id, event.value.message),
)

const copyText = computed(() => {
  const e = event.value
  return `${formatTime(e.received_at)} ${e.severity_label.toUpperCase()} ${e.hostname} ${e.programname}: ${e.message}`
})
</script>

<template>
  <div ref="rowEl" class="group">
    <!-- Mobile: two-line layout with severity color bar -->
    <div
      :data-copytext="copyText"
      role="button"
      tabindex="0"
      :aria-expanded="expanded"
      :aria-label="`${event.severity_label} event from ${event.hostname}: ${event.message.slice(0, 80)}`"
      class="hover:bg-t-bg-hover flex cursor-pointer gap-2 py-1 pr-2 md:hidden"
      :class="sevBgClass"
      @click="toggle"
      @keydown.enter="toggle"
      @keydown.space.prevent="toggle"
    >
      <div class="w-[3px] shrink-0 rounded-r" :class="sevBarClass" />
      <div class="min-w-0 flex-1">
        <div class="truncate leading-snug" v-html="highlightedMessage" />
        <div class="text-t-fg-gutter mt-px flex items-center gap-1.5 text-[10px] leading-tight">
          <span class="uppercase" :class="sevClass">{{ event.severity_label }}</span>
          <span class="text-t-fg-gutter/40">&middot;</span>
          <span>{{ formatTime(event.received_at) }}</span>
          <span class="text-t-fg-gutter/40">&middot;</span>
          <span class="text-t-teal/60 truncate">{{ event.hostname }}</span>
          <span class="text-t-fg-gutter/40">&middot;</span>
          <span class="text-t-purple/60 truncate">{{ event.programname }}</span>
        </div>
      </div>
    </div>
    <!-- Desktop: single-line layout -->
    <div
      :data-copytext="copyText"
      role="button"
      tabindex="0"
      :aria-expanded="expanded"
      :aria-label="`${event.severity_label} event from ${event.hostname}: ${event.message.slice(0, 80)}`"
      class="hover:bg-t-bg-hover hidden cursor-pointer items-baseline gap-3 px-4 py-px leading-snug md:flex"
      :class="sevBgClass"
      @click="toggle"
      @keydown.enter="toggle"
      @keydown.space.prevent="toggle"
    >
      <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.received_at) }}</span>
      <span class="w-[8ch] shrink-0 uppercase" :class="sevClass">{{ event.severity_label }}</span>
      <button
        class="text-t-teal shrink-0 truncate text-left hover:underline"
        :style="{ width: 'var(--col-host, 20ch)' }"
        @click.stop="filterStore.filters.hostname = event.hostname"
      >
        {{ event.hostname }}
      </button>
      <span class="text-t-purple shrink-0 truncate" :style="{ width: 'var(--col-prog, 14ch)' }">{{ event.programname }}</span>
      <span class="min-w-0 flex-1 truncate" v-html="highlightedMessage" />
    </div>
    <SyslogDetail v-if="expanded" :event="event" />
  </div>
</template>
