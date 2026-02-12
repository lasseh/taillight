<script setup lang="ts">
import { ref, computed, nextTick, toRef } from 'vue'
import type { AppLogEvent } from '@/types/applog'
import { levelColorClass, levelBgClass } from '@/lib/applog-constants'
import { formatTime, formatAttrs, truncate } from '@/lib/format'
import AppLogDetail from '@/components/AppLogDetail.vue'

const props = defineProps<{
  event: AppLogEvent
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
const lvlClass = computed(() => levelColorClass[event.value.level] ?? 'text-t-fg')
const bgClass = computed(() => levelBgClass[event.value.level] ?? '')

const hasAttrs = computed(() => event.value.attrs && Object.keys(event.value.attrs).length > 0)

const copyText = computed(() => {
  const e = event.value
  const parts = [formatTime(e.timestamp), e.level.toUpperCase(), e.host, e.service]
  if (e.component) parts.push(e.component)
  let line = parts.join(' ') + ': ' + e.msg
  if (hasAttrs.value) line += ' ' + formatAttrs(e.attrs!)
  return line
})
</script>

<template>
  <div ref="rowEl" class="group">
    <div
      :data-copytext="copyText"
      role="button"
      tabindex="0"
      :aria-expanded="expanded"
      :aria-label="`${event.level} event from ${event.service}: ${event.msg.slice(0, 80)}`"
      class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug"
      :class="bgClass"
      @click="toggle"
      @keydown.enter="toggle"
      @keydown.space.prevent="toggle"
    >
      <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.timestamp) }}</span>
      <span class="w-[8ch] shrink-0 uppercase" :class="lvlClass">{{ event.level }}</span>
      <span class="text-t-teal w-[20ch] shrink-0 truncate">{{ event.host }}</span>
      <span class="text-t-purple w-[14ch] shrink-0 truncate">{{ event.service }}</span>
      <span class="text-t-yellow w-[14ch] shrink-0 truncate">{{ event.component }}</span>
      <span class="text-t-fg min-w-0 flex-1 truncate">{{ truncate(event.msg, 200) }}<template v-if="hasAttrs">&nbsp;<span class="text-t-orange">-</span> <span class="text-t-fg-dark">{{ formatAttrs(event.attrs!) }}</span></template></span>
    </div>
    <AppLogDetail v-if="expanded" :event="event" />
  </div>
</template>
