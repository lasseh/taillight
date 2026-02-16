<script setup lang="ts">
import { ref, computed, watch } from 'vue'
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

const hours = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0'))
const minutes = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, '0'))

// Draft state — only applied on "apply" click
const draftFromDate = ref('')
const draftFromHour = ref('00')
const draftFromMinute = ref('00')
const draftToDate = ref('')
const draftToHour = ref('00')
const draftToMinute = ref('00')

function parseParts(iso: string): { date: string; hour: string; minute: string } {
  if (!iso) return { date: '', hour: '00', minute: '00' }
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  const date = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  return { date, hour: pad(d.getHours()), minute: pad(d.getMinutes()) }
}

// Sync draft from props when dropdown opens
watch(open, (isOpen) => {
  if (isOpen) {
    const f = parseParts(props.from)
    draftFromDate.value = f.date
    draftFromHour.value = f.hour
    draftFromMinute.value = f.minute
    const t = parseParts(props.to)
    draftToDate.value = t.date
    draftToHour.value = t.hour
    draftToMinute.value = t.minute
  }
})

function buildIso(date: string, hour: string, minute: string): string {
  if (!date) return ''
  return new Date(`${date}T${hour}:${minute}:00`).toISOString()
}

function todayStr(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function apply() {
  const today = todayStr()
  let fromIso = buildIso(draftFromDate.value || today, draftFromHour.value, draftFromMinute.value)
  let toIso = buildIso(draftToDate.value || today, draftToHour.value, draftToMinute.value)
  if (fromIso > toIso) [fromIso, toIso] = [toIso, fromIso]
  emit('update:from', fromIso)
  emit('update:to', toIso)
  open.value = false
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
        <!-- Custom range -->
        <div class="space-y-2 px-3 py-2">
          <div class="flex items-center gap-2">
            <span class="text-t-fg-dark w-8 text-xs">from</span>
            <input
              type="date"
              v-model="draftFromDate"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1.5 py-0.5 text-xs outline-none"
            />
            <select
              v-model="draftFromHour"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
            >
              <option v-for="h in hours" :key="h" :value="h">{{ h }}</option>
            </select>
            <span class="text-t-fg-dark text-xs">:</span>
            <select
              v-model="draftFromMinute"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
            >
              <option v-for="m in minutes" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-t-fg-dark w-8 text-xs">to</span>
            <input
              type="date"
              v-model="draftToDate"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1.5 py-0.5 text-xs outline-none"
            />
            <select
              v-model="draftToHour"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
            >
              <option v-for="h in hours" :key="h" :value="h">{{ h }}</option>
            </select>
            <span class="text-t-fg-dark text-xs">:</span>
            <select
              v-model="draftToMinute"
              class="bg-t-bg border-t-border text-t-fg focus:border-t-blue rounded border px-1 py-0.5 text-xs outline-none"
            >
              <option v-for="m in minutes" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
        </div>

        <!-- Actions -->
        <div class="border-t-border flex items-center gap-3 border-t px-3 py-2 justify-end">
          <button
            v-if="hasRange"
            type="button"
            class="text-t-red text-xs hover:underline"
            @click="clear"
          >
            clear
          </button>
          <button
            type="button"
            class="text-t-blue text-xs hover:underline"
            @click="apply"
          >
            apply
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
