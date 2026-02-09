<script setup lang="ts">
import { ref, computed } from 'vue'
import { onClickOutside } from '@vueuse/core'
import type { FilterOption } from '@/types/syslog'

const props = defineProps<{
  label: string
  options: FilterOption[]
}>()

const model = defineModel<string>({ required: true })

const open = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

onClickOutside(dropdownRef, () => {
  open.value = false
})

const selectedLabel = computed(() => {
  if (!model.value) return 'all'
  const match = props.options.find((o) => o.value === model.value)
  return match ? match.label : model.value
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
  open.value = false
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
        class="bg-t-bg-dark border-t-border text-t-fg cursor-pointer border px-2 py-0.5 text-xs transition-colors"
        :class="open ? 'border-t-terminal' : 'hover:border-t-terminal'"
        @click="open = !open"
      >
        <span class="invisible h-0 block">{{ longestLabel }}</span>
        <span>{{ selectedLabel }}</span>
      </button>
    </label>

    <Transition name="menu">
      <div
        v-if="open"
        role="listbox"
        :aria-label="`${label} options`"
        class="bg-t-bg-dark border-t-border absolute left-0 top-full z-50 mt-1.5 min-w-36 rounded border shadow-lg"
      >
        <div class="max-h-64 overflow-y-auto py-1">
          <button
            type="button"
            role="option"
            :aria-selected="!model"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              !model
                ? 'bg-t-bg-highlight text-t-fg'
                : 'text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg'
            "
            @click="select('')"
          >
            <span>all</span>
            <span v-if="!model" class="text-t-green ml-auto">*</span>
          </button>
          <button
            v-for="opt in options"
            :key="opt.value"
            type="button"
            role="option"
            :aria-selected="model === opt.value"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs transition-colors"
            :class="
              model === opt.value
                ? 'bg-t-bg-highlight text-t-fg'
                : 'text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg'
            "
            @click="select(opt.value)"
          >
            <span>{{ opt.label }}</span>
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
