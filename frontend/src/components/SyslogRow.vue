<script setup lang="ts">
import { ref, computed, nextTick, toRef } from 'vue'
import type { SyslogEvent } from '@/types/syslog'
import { severityColorClass, severityBgClass } from '@/lib/constants'
import { highlightMessage } from '@/lib/highlighter'
import { formatTime, truncate } from '@/lib/format'
import SyslogDetail from '@/components/SyslogDetail.vue'
import { useSyslogFilterStore } from '@/stores/syslog-filters'

const filterStore = useSyslogFilterStore()

const props = defineProps<{
  event: SyslogEvent
}>()

const expanded = ref(false)
const rowEl = ref<HTMLElement | null>(null)

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

const highlightedMessage = computed(() =>
  highlightMessage(event.value.id, truncate(event.value.message, 200)),
)

const copyText = computed(() => {
  const e = event.value
  return `${formatTime(e.received_at)} ${e.severity_label.toUpperCase()} ${e.hostname} ${e.programname}: ${e.message}`
})
</script>

<template>
  <div ref="rowEl" class="group">
    <div
      :data-copytext="copyText"
      role="button"
      tabindex="0"
      :aria-expanded="expanded"
      :aria-label="`${event.severity_label} event from ${event.hostname}: ${event.message.slice(0, 80)}`"
      class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug"
      :class="sevBgClass"
      @click="toggle"
      @keydown.enter="toggle"
      @keydown.space.prevent="toggle"
    >
      <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.received_at) }}</span>
      <span class="w-[8ch] shrink-0 uppercase" :class="sevClass">{{ event.severity_label }}</span>
      <button
        class="text-t-teal w-[20ch] shrink-0 truncate text-left hover:underline"
        @click.stop="filterStore.filters.hostname = event.hostname"
      >
        {{ event.hostname }}
      </button>
      <span class="text-t-purple w-[14ch] shrink-0 truncate">{{ event.programname }}</span>
      <span class="min-w-0 flex-1 truncate" v-html="highlightedMessage" />
    </div>
    <SyslogDetail v-if="expanded" :event="event" />
  </div>
</template>
