<script setup lang="ts">
import { ref, computed } from 'vue'
import { onClickOutside } from '@vueuse/core'

const props = defineProps<{
  from: string
  to: string
}>()

const emit = defineEmits<{
  'update:from': [value: string]
  'update:to': [value: string]
}>()

const open = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

onClickOutside(dropdownRef, () => {
  open.value = false
})

const hasRange = computed(() => Boolean(props.from || props.to))

const presets = [
  { label: '15m', ms: 15 * 60 * 1000 },
  { label: '1h', ms: 60 * 60 * 1000 },
  { label: '6h', ms: 6 * 60 * 60 * 1000 },
  { label: '24h', ms: 24 * 60 * 60 * 1000 },
  { label: '7d', ms: 7 * 24 * 60 * 60 * 1000 },
  { label: '30d', ms: 30 * 24 * 60 * 60 * 1000 },
] as const

function applyPreset(ms: number) {
  const now = new Date()
  emit('update:from', new Date(now.getTime() - ms).toISOString())
  emit('update:to', now.toISOString())
  open.value = false
}

// datetime-local inputs use "YYYY-MM-DDTHH:mm" in local time
function isoToLocal(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  d.setMinutes(Math.round(d.getMinutes() / 5) * 5, 0, 0)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function localToIso(local: string): string {
  if (!local) return ''
  const d = new Date(local)
  d.setMinutes(Math.round(d.getMinutes() / 5) * 5, 0, 0)
  return d.toISOString()
}

function onFromInput(e: Event) {
  const val = (e.target as HTMLInputElement).value
  emit('update:from', val ? localToIso(val) : '')
}

function onToInput(e: Event) {
  const val = (e.target as HTMLInputElement).value
  emit('update:to', val ? localToIso(val) : '')
}

function clear() {
  emit('update:from', '')
  emit('update:to', '')
  open.value = false
}

function formatRange(from: string, to: string): string {
  const fmt = (iso: string) => {
    const d = new Date(iso)
    const month = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    const hours = String(d.getHours()).padStart(2, '0')
    const mins = String(d.getMinutes()).padStart(2, '0')
    return `${month}/${day} ${hours}:${mins}`
  }
  if (from && to) return `${fmt(from)} – ${fmt(to)}`
  if (from) return `from ${fmt(from)}`
  if (to) return `until ${fmt(to)}`
  return 'all time'
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') open.value = false
}
</script>

<template>
  <div ref="dropdownRef" class="relative" @keydown="onKeydown">
    <label class="flex items-center gap-1">
      <span class="text-t-fg-dark text-xs">time</span>
      <button
        type="button"
        aria-label="Filter by time range"
        :aria-expanded="open"
        class="bg-t-bg-dark border-t-border cursor-pointer border px-2 py-0.5 text-left text-xs transition-colors"
        :class="
          hasRange
            ? 'border-t-blue text-t-blue'
            : open
              ? 'text-t-fg border-t-terminal'
              : 'text-t-fg hover:border-t-terminal'
        "
        @click="open = !open"
      >
        {{ formatRange(from, to) }}
      </button>
    </label>

    <Transition name="menu">
      <div
        v-if="open"
        class="bg-t-bg-dark border-t-border absolute left-0 top-full z-50 mt-1.5 w-64 rounded border shadow-lg"
      >
        <!-- Presets -->
        <div class="border-t-border flex flex-wrap gap-1.5 border-b px-3 py-2">
          <button
            v-for="p in presets"
            :key="p.label"
            type="button"
            class="bg-t-bg-highlight text-t-fg hover:bg-t-bg-hover hover:text-t-blue rounded px-2 py-0.5 text-xs transition-colors"
            @click="applyPreset(p.ms)"
          >
            {{ p.label }}
          </button>
        </div>

        <!-- Custom range -->
        <div class="space-y-2 px-3 py-2">
          <label class="flex items-center gap-2">
            <span class="text-t-fg-dark w-8 text-xs">from</span>
            <input
              type="datetime-local"
              step="300"
              :value="isoToLocal(from)"
              :max="isoToLocal(to) || undefined"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue flex-1 rounded border px-1.5 py-0.5 text-xs outline-none"
              @input="onFromInput"
            />
          </label>
          <label class="flex items-center gap-2">
            <span class="text-t-fg-dark w-8 text-xs">to</span>
            <input
              type="datetime-local"
              step="300"
              :value="isoToLocal(to)"
              :min="isoToLocal(from) || undefined"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue flex-1 rounded border px-1.5 py-0.5 text-xs outline-none"
              @input="onToInput"
            />
          </label>
        </div>

        <!-- Clear -->
        <div v-if="hasRange" class="border-t-border border-t px-3 py-2">
          <button
            type="button"
            class="text-t-red text-xs hover:underline"
            @click="clear"
          >
            clear time range
          </button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.menu-enter-active,
.menu-leave-active {
  transition: opacity 0.1s ease, transform 0.1s ease;
}

.menu-enter-from,
.menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

/* Style native datetime-local inputs to blend with theme */
input[type='datetime-local']::-webkit-calendar-picker-indicator {
  filter: invert(0.6);
  cursor: pointer;
}
</style>
