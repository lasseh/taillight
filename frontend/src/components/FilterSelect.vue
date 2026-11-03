<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { useClickOutside } from '@/composables/useClickOutside'
import { useDebounceFn } from '@/composables/useDebounceFn'
import type { FilterOption } from '@/types/srvlog'
import { wildcardMatch } from '@/lib/wildcard'

const props = withDefaults(
  defineProps<{
    label: string
    options: FilterOption[]
    searchable?: boolean
    // Looks typed text up on the server. The options passed in may be a
    // capped list, so a value missing from them can still be found by name.
    fetchOptions?: (q: string) => Promise<FilterOption[]>
  }>(),
  { searchable: false },
)

const SEARCH_DEBOUNCE_MS = 300

const model = defineModel<string>({ required: true })

const open = ref(false)
const searchText = ref('')
const highlightIndex = ref(-1)
const searchInput = ref<HTMLInputElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

useClickOutside(dropdownRef, () => {
  open.value = false
})

watch(open, (isOpen) => {
  if (isOpen && props.searchable) {
    searchText.value = ''
    highlightIndex.value = -1
    nextTick(() => searchInput.value?.focus())
  }
})

// Server results and the text they answer; null while typed text is matched
// against the loaded options only.
const remote = ref<{ q: string; options: FilterOption[] } | null>(null)
let searchSeq = 0

// remoteQuery is the typed text to look up on the server: anything but a *
// pattern, which stays a local match plus the free-text pattern row.
function remoteQuery(): string {
  const q = searchText.value.trim()
  return props.fetchOptions && q && !q.includes('*') ? q : ''
}

// runRemoteSearch looks q up and returns the options, or null when the lookup
// failed or the text has moved on; local matching then stays in place.
async function runRemoteSearch(q: string): Promise<FilterOption[] | null> {
  if (!props.fetchOptions) return null
  const seq = ++searchSeq
  try {
    const options = await props.fetchOptions(q)
    if (seq !== searchSeq || remoteQuery() !== q) return null
    remote.value = { q, options }
    return options
  } catch {
    return null
  }
}

const searchRemote = useDebounceFn((q: string) => void runRemoteSearch(q), SEARCH_DEBOUNCE_MS)

watch(searchText, () => {
  highlightIndex.value = -1
  const q = remoteQuery()
  if (q) {
    searchRemote(q)
  } else {
    searchSeq++
    remote.value = null
  }
})

const hasWildcard = computed(() => searchText.value.includes('*'))

const filteredOptions = computed(() => {
  if (!props.searchable || !searchText.value) return props.options
  // Server results replace the loaded options once they arrive, still
  // narrowed by the text typed since, trimmed as the server query is.
  const pool = remote.value?.options ?? props.options
  const q = searchText.value.trim().toLowerCase()
  if (hasWildcard.value) {
    return pool.filter((o) => wildcardMatch(o.value, searchText.value))
  }
  return pool.filter((o) => o.value.toLowerCase().includes(q))
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

async function onSearchKeydown(e: KeyboardEvent) {
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
      const trimmed = searchText.value.trim()
      if (!trimmed) {
        select('')
      } else if (hasWildcard.value) {
        select(trimmed)
      } else {
        // Pick from the server's answer for this text, not from options
        // that may not hold the value yet.
        let first = filteredOptions.value[0]
        const q = remoteQuery()
        if (q && remote.value?.q !== q) first = (await runRemoteSearch(q))?.[0] ?? first
        if (first) select(first.value)
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
        class="bg-t-bg-dark border-t-border text-t-fg max-w-48 truncate cursor-pointer border px-2 py-0.5 text-left text-xs transition-colors"
        :class="open ? 'border-t-terminal' : 'hover:border-t-terminal'"
        @click="open = !open"
      >
        <span class="invisible block h-0 truncate">{{ longestLabel }}</span>
        <span :class="selectedOption?.colorClass">{{ selectedLabel }}</span>
      </button>
    </label>

    <Transition name="menu">
      <div
        v-if="open"
        role="listbox"
        :aria-label="`${label} options`"
        class="bg-t-bg-dark border-t-border absolute left-0 top-full z-50 mt-1.5 w-max min-w-full max-w-64 rounded border shadow-lg"
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
            v-if="hasWildcard && searchText.trim()"
            type="button"
            role="option"
            :aria-selected="false"
            :data-highlighted="highlightIndex === -1 && searchable ? '' : undefined"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              highlightIndex === -1 && searchable
                ? 'bg-t-bg-hover text-t-terminal'
                : 'text-t-terminal hover:bg-t-bg-hover'
            "
            @click="select(searchText.trim())"
          >
            <span class="truncate">{{ searchText.trim() }}</span>
            <span class="text-t-fg-dark ml-auto text-[10px]">pattern</span>
          </button>
          <button
            v-if="!hasWildcard || !searchText.trim()"
            type="button"
            role="option"
            :aria-selected="!model"
            :data-highlighted="highlightIndex === -1 && searchable && !hasWildcard ? '' : undefined"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              highlightIndex === -1 && searchable && !hasWildcard
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
            <span class="truncate" :class="opt.colorClass">{{ opt.label }}</span>
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
  transition:
    opacity 0.1s ease,
    transform 0.1s ease;
}

.menu-enter-from,
.menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
