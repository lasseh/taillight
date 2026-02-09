<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { onClickOutside } from '@vueuse/core'
import { wildcardMatch } from '@/lib/wildcard'
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
const searchInput = ref<HTMLInputElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)

onClickOutside(dropdownRef, () => {
  open.value = false
})

watch(open, (isOpen) => {
  if (isOpen && props.searchable) {
    searchText.value = model.value?.includes('*') ? model.value : ''
    nextTick(() => searchInput.value?.focus())
  }
})

const filteredOptions = computed(() => {
  if (!props.searchable || !searchText.value) return props.options
  return props.options.filter((o) => wildcardMatch(o.value, searchText.value))
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
  open.value = false
}

function onSearchKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    const text = searchText.value.trim()
    if (!text || text === '*') {
      select('')
    } else {
      model.value = text
      open.value = false
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
            placeholder="glob pattern…"
            class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter w-full border px-1.5 py-0.5 text-xs outline-none focus:border-t-blue"
            @keydown="onSearchKeydown"
          />
        </div>
        <div class="overflow-y-auto py-1" style="max-height: 20rem">
          <button
            type="button"
            role="option"
            :aria-selected="!model"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              !model
                ? 'bg-t-bg-highlight text-t-fg'
                : 'text-t-fg hover:bg-t-bg-hover'
            "
            @click="select('')"
          >
            <span>all</span>
            <span v-if="!model" class="text-t-green ml-auto">*</span>
          </button>
          <button
            v-for="opt in filteredOptions"
            :key="opt.value"
            type="button"
            role="option"
            :aria-selected="model === opt.value"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              model === opt.value
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
