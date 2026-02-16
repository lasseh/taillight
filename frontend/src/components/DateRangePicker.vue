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

const hours = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0'))
const minutes = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, '0'))

function applyPreset(ms: number) {
  const now = new Date()
  emit('update:from', new Date(now.getTime() - ms).toISOString())
  emit('update:to', now.toISOString())
  open.value = false
}

function parseParts(iso: string): { date: string; hour: string; minute: string } {
  if (!iso) return { date: '', hour: '00', minute: '00' }
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  const date = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  return { date, hour: pad(d.getHours()), minute: pad(d.getMinutes()) }
}

function buildIso(date: string, hour: string, minute: string): string {
  if (!date) return ''
  return new Date(`${date}T${hour}:${minute}:00`).toISOString()
}

const fromParts = computed(() => parseParts(props.from))
const toParts = computed(() => parseParts(props.to))

function updateFrom(field: 'date' | 'hour' | 'minute', value: string) {
  const parts = { ...fromParts.value, [field]: value }
  emit('update:from', buildIso(parts.date, parts.hour, parts.minute))
}

function updateTo(field: 'date' | 'hour' | 'minute', value: string) {
  const parts = { ...toParts.value, [field]: value }
  emit('update:to', buildIso(parts.date, parts.hour, parts.minute))
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
        class="bg-t-bg-dark border-t-border absolute left-0 top-full z-50 mt-1.5 w-max rounded border shadow-lg"
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
          <div class="flex items-center gap-2">
            <span class="text-t-fg-dark w-8 text-xs">from</span>
            <input
              type="date"
              :value="fromParts.date"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1.5 py-0.5 text-xs outline-none"
              @input="updateFrom('date', ($event.target as HTMLInputElement).value)"
            />
            <select
              :value="fromParts.hour"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
              @change="updateFrom('hour', ($event.target as HTMLSelectElement).value)"
            >
              <option v-for="h in hours" :key="h" :value="h">{{ h }}</option>
            </select>
            <span class="text-t-fg-dark text-xs">:</span>
            <select
              :value="fromParts.minute"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
              @change="updateFrom('minute', ($event.target as HTMLSelectElement).value)"
            >
              <option v-for="m in minutes" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-t-fg-dark w-8 text-xs">to</span>
            <input
              type="date"
              :value="toParts.date"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1.5 py-0.5 text-xs outline-none"
              @input="updateTo('date', ($event.target as HTMLInputElement).value)"
            />
            <select
              :value="toParts.hour"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
              @change="updateTo('hour', ($event.target as HTMLSelectElement).value)"
            >
              <option v-for="h in hours" :key="h" :value="h">{{ h }}</option>
            </select>
            <span class="text-t-fg-dark text-xs">:</span>
            <select
              :value="toParts.minute"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
              @change="updateTo('minute', ($event.target as HTMLSelectElement).value)"
            >
              <option v-for="m in minutes" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
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

/* Style native date input picker icon to blend with theme */
input[type='date']::-webkit-calendar-picker-indicator {
  filter: invert(0.6);
  cursor: pointer;
}
</style>
