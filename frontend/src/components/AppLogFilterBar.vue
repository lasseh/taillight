<script setup lang="ts">
import { computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useAppLogFilterStore } from '@/stores/applog-filters'
import { useAppLogMetaStore } from '@/stores/applog-meta'
import { levelOptions } from '@/lib/applog-constants'
import FilterSelect from '@/components/FilterSelect.vue'

const filterStore = useAppLogFilterStore()
const meta = useAppLogMetaStore()

const serviceOptions = computed(() =>
  meta.services.map((s) => ({ value: s, label: s })),
)

const componentOptions = computed(() =>
  meta.components.map((c) => ({ value: c, label: c })),
)

const hostOptions = computed(() =>
  meta.hosts.map((h) => ({ value: h, label: h })),
)

const searchInput = computed({
  get: () => filterStore.filters.search,
  set: useDebounceFn((val: string) => {
    filterStore.filters.search = val
  }, 300),
})
</script>

<template>
  <div class="border-t-border bg-t-bg-dark flex flex-wrap items-center gap-3 border-b px-4 py-1.5">
    <FilterSelect v-model="filterStore.filters.level" label="level" :options="levelOptions" />
    <FilterSelect v-model="filterStore.filters.host" label="host" :options="hostOptions" />
    <FilterSelect v-model="filterStore.filters.service" label="service" :options="serviceOptions" />
    <FilterSelect v-model="filterStore.filters.component" label="component" :options="componentOptions" />
    <label class="flex items-center gap-1">
      <span class="text-t-fg-dark text-xs">search</span>
      <input
        v-model="searchInput"
        type="text"
        placeholder="message…"
        aria-label="Search app log messages"
        class="bg-t-bg-dark border-t-border text-t-fg placeholder:text-t-fg-gutter hover:border-t-terminal focus:border-t-blue w-64 border px-2 py-0.5 text-xs outline-none"
      />
    </label>

    <button
      v-if="filterStore.hasActiveFilters"
      class="text-t-red text-xs"
      aria-label="Clear all filters"
      @click="filterStore.clearAll()"
    >
      clear
    </button>
  </div>
</template>
