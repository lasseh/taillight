<script setup lang="ts">
import { ref, computed, nextTick, toRef, inject, watch } from 'vue'
import type { Ref } from 'vue'
import type { AppLogEvent } from '@/types/applog'
import { levelColorClass, levelBgClass } from '@/lib/applog-constants'
import { formatTime, formatAttrs } from '@/lib/format'
import AppLogDetail from '@/components/AppLogDetail.vue'
import { useAppLogFilterStore } from '@/stores/applog-filters'

const filterStore = useAppLogFilterStore()

const props = defineProps<{
  event: AppLogEvent
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
    <!-- Mobile: two-line layout -->
    <div
      :data-copytext="copyText"
      role="button"
      tabindex="0"
      :aria-expanded="expanded"
      :aria-label="`${event.level} event from ${event.service}: ${event.msg.slice(0, 80)}`"
      class="hover:bg-t-bg-hover cursor-pointer px-2 py-0.5 md:hidden"
      :class="bgClass"
      @click="toggle"
      @keydown.enter="toggle"
      @keydown.space.prevent="toggle"
    >
      <div class="flex items-baseline gap-1.5 leading-snug">
        <span class="w-[8ch] shrink-0 uppercase" :class="lvlClass">{{ event.level }}</span>
        <span class="text-t-fg min-w-0 flex-1 truncate">{{ event.msg }}<template v-if="hasAttrs">&nbsp;<span class="text-t-orange">-</span> <span class="text-t-fg-dark">{{ formatAttrs(event.attrs!) }}</span></template></span>
      </div>
      <div class="text-t-fg-gutter flex items-baseline gap-1 truncate pl-[8ch] text-[10px] leading-snug">
        <span>{{ formatTime(event.timestamp) }}</span>
        <span class="text-t-fg-gutter/50">&middot;</span>
        <span class="text-t-teal/70">{{ event.host }}</span>
        <span class="text-t-fg-gutter/50">&middot;</span>
        <span class="text-t-purple/70">{{ event.service }}</span>
        <template v-if="event.component">
          <span class="text-t-fg-gutter/50">&middot;</span>
          <span class="text-t-yellow/70">{{ event.component }}</span>
        </template>
      </div>
    </div>
    <!-- Desktop: single-line layout -->
    <div
      :data-copytext="copyText"
      role="button"
      tabindex="0"
      :aria-expanded="expanded"
      :aria-label="`${event.level} event from ${event.service}: ${event.msg.slice(0, 80)}`"
      class="hover:bg-t-bg-hover hidden cursor-pointer items-baseline gap-3 px-4 py-px leading-snug md:flex"
      :class="bgClass"
      @click="toggle"
      @keydown.enter="toggle"
      @keydown.space.prevent="toggle"
    >
      <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.timestamp) }}</span>
      <span class="w-[8ch] shrink-0 uppercase" :class="lvlClass">{{ event.level }}</span>
      <button
        class="text-t-teal shrink-0 truncate text-left hover:underline"
        :style="{ width: 'var(--col-host, 20ch)' }"
        @click.stop="filterStore.filters.host = event.host"
      >
        {{ event.host }}
      </button>
      <span class="text-t-purple shrink-0 truncate" :style="{ width: 'var(--col-svc, 14ch)' }">{{ event.service }}</span>
      <span class="text-t-yellow shrink-0 truncate" :style="{ width: 'var(--col-comp, 14ch)' }">{{ event.component }}</span>
      <span class="text-t-fg min-w-0 flex-1 truncate">{{ event.msg }}<template v-if="hasAttrs">&nbsp;<span class="text-t-orange">-</span> <span class="text-t-fg-dark">{{ formatAttrs(event.attrs!) }}</span></template></span>
    </div>
    <AppLogDetail v-if="expanded" :event="event" />
  </div>
</template>
