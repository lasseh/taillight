<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { onClickOutside } from '@vueuse/core'
import type { FilterOption } from '@/types/syslog'

const props = withDefaults(
  defineProps<{
    label: string
    options: FilterOption[]
    searchable?: boolean
  }>(),
  { searchable: false },
)

const model = defineModel<string>({ required: true })

const open = ref(false)
const searchText = ref('')
const highlightIndex = ref(-1)
const searchInput = ref<HTMLInputElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

onClickOutside(dropdownRef, () => {
  open.value = false
})

watch(open, (isOpen) => {
  if (isOpen && props.searchable) {
    searchText.value = ''
    highlightIndex.value = -1
    nextTick(() => searchInput.value?.focus())
  }
})

watch(searchText, () => {
  highlightIndex.value = -1
})

const filteredOptions = computed(() => {
  if (!props.searchable || !searchText.value) return props.options
  const q = searchText.value.toLowerCase()
  return props.options.filter((o) => o.value.toLowerCase().includes(q))
})

const selectedOption = computed(() => {
  if (!model.value) return null
  return props.options.find((o) => o.value === model.value) ?? null
})

const selectedLabel = computed(() => {
  if (!model.value) return 'all'
  return selectedOption.value ? selectedOption.value.label : model.value
})

const longestLabel = computed(() => {
  let longest = 'all'
  for (const o of props.options) {
    if (o.label.length > longest.length) longest = o.label
  }
  return longest
})

function select(value: string) {
  model.value = value
  searchText.value = ''
  highlightIndex.value = -1
  open.value = false
}

function scrollHighlightedIntoView() {
  nextTick(() => {
    const el = listRef.value?.querySelector('[data-highlighted]')
    el?.scrollIntoView({ block: 'nearest' })
  })
}

function onSearchKeydown(e: KeyboardEvent) {
  const total = filteredOptions.value.length
  // -1 = "all" row, 0..total-1 = filtered options
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    highlightIndex.value = Math.min(highlightIndex.value + 1, total - 1)
    scrollHighlightedIntoView()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    highlightIndex.value = Math.max(highlightIndex.value - 1, -1)
    scrollHighlightedIntoView()
  } else if (e.key === 'Enter') {
    e.preventDefault()
    if (highlightIndex.value === -1) {
      // "all" row highlighted or no navigation yet — select "all" if empty, first match otherwise
      const first = filteredOptions.value[0]
      if (!searchText.value.trim()) {
        select('')
      } else if (first) {
        select(first.value)
      }
    } else {
      const opt = filteredOptions.value[highlightIndex.value]
      if (opt) select(opt.value)
    }
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    open.value = false
  }
}
</script>

<template>
  <div ref="dropdownRef" class="relative" @keydown="onKeydown">
    <label class="flex items-center gap-1">
      <span class="text-t-fg-dark text-xs">{{ label }}</span>
      <button
        type="button"
        :aria-label="`Filter by ${label}`"
        :aria-expanded="open"
        class="bg-t-bg-dark border-t-border text-t-fg cursor-pointer border px-2 py-0.5 text-left text-xs transition-colors"
        :class="open ? 'border-t-terminal' : 'hover:border-t-terminal'"
        @click="open = !open"
      >
        <span class="invisible block h-0">{{ longestLabel }}</span>
        <span :class="selectedOption?.colorClass">{{ selectedLabel }}</span>
      </button>
    </label>

    <Transition name="menu">
      <div
        v-if="open"
        role="listbox"
        :aria-label="`${label} options`"
        class="bg-t-bg-dark border-t-border absolute left-0 top-full z-50 mt-1.5 w-max min-w-full rounded border shadow-lg"
      >
        <div v-if="searchable" class="border-t-border border-b px-2 py-1">
          <input
            ref="searchInput"
            v-model="searchText"
            type="text"
            placeholder="search…"
            class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter w-full border px-1.5 py-0.5 text-xs outline-none focus:border-t-blue"
            @keydown="onSearchKeydown"
          />
        </div>
        <div ref="listRef" class="max-h-64 overflow-y-auto py-1">
          <button
            type="button"
            role="option"
            :aria-selected="!model"
            :data-highlighted="highlightIndex === -1 && searchable ? '' : undefined"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              highlightIndex === -1 && searchable
                ? 'bg-t-bg-hover text-t-fg'
                : !model
                  ? 'bg-t-bg-highlight text-t-fg'
                  : 'text-t-fg hover:bg-t-bg-hover'
            "
            @click="select('')"
          >
            <span>all</span>
            <span v-if="!model" class="text-t-green ml-auto">*</span>
          </button>
          <button
            v-for="(opt, idx) in filteredOptions"
            :key="opt.value"
            type="button"
            role="option"
            :aria-selected="model === opt.value"
            :data-highlighted="highlightIndex === idx ? '' : undefined"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              highlightIndex === idx
                ? 'bg-t-bg-hover text-t-fg'
                : model === opt.value
                  ? 'bg-t-bg-highlight text-t-fg'
                  : 'text-t-fg hover:bg-t-bg-hover'
            "
            @click="select(opt.value)"
          >
            <span :class="opt.colorClass">{{ opt.label }}</span>
            <span v-if="model === opt.value" class="text-t-green ml-auto">*</span>
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
</style>
