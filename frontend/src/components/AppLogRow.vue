<script setup lang="ts">
import { ref, computed, nextTick, toRef, inject, watch } from 'vue'
import type { Ref } from 'vue'
import type { AppLogEvent } from '@/types/applog'
import { levelColorClass, levelBgClass, levelBgColorClass } from '@/lib/applog-constants'
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
const lvlBarClass = computed(() => levelBgColorClass[event.value.level] ?? 'bg-sev-notice')

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
    <!-- Mobile: two-line layout with level color bar -->
    <div
      :data-copytext="copyText"
      role="button"
      tabindex="0"
      :aria-expanded="expanded"
      :aria-label="`${event.level} event from ${event.service}: ${event.msg.slice(0, 80)}`"
      class="hover:bg-t-bg-hover flex cursor-pointer gap-2 py-1 pr-2 md:hidden"
      :class="bgClass"
      @click="toggle"
      @keydown.enter="toggle"
      @keydown.space.prevent="toggle"
    >
      <div class="w-[3px] shrink-0 rounded-r" :class="lvlBarClass" />
      <div class="min-w-0 flex-1">
        <div class="text-t-teal/60 truncate text-[10px] leading-tight">{{ event.host }}</div>
        <div class="text-t-fg truncate text-xs leading-snug">{{ event.msg }}<template v-if="hasAttrs">&nbsp;<span class="text-t-orange">-</span> <span class="text-t-fg-dark">{{ formatAttrs(event.attrs!) }}</span></template></div>
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
