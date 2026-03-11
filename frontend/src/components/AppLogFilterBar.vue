<script setup lang="ts">
import { computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useAppLogFilterStore } from '@/stores/applog-filters'
import { useAppLogMetaStore } from '@/stores/applog-meta'
import { levelOptions } from '@/lib/applog-constants'
import FilterSelect from '@/components/FilterSelect.vue'
import DateRangePicker from '@/components/DateRangePicker.vue'

const filterStore = useAppLogFilterStore()
const meta = useAppLogMetaStore()

const serviceOptions = computed(() =>
  meta.services.map((s) => ({ value: s, label: s, colorClass: 'text-t-purple' })),
)

const componentOptions = computed(() =>
  meta.components.map((c) => ({ value: c, label: c, colorClass: 'text-t-yellow' })),
)

const hostOptions = computed(() =>
  meta.hosts.map((h) => ({ value: h, label: h, colorClass: 'text-t-teal' })),
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
    <DateRangePicker
      :from="filterStore.filters.from"
      :to="filterStore.filters.to"
      @update:from="filterStore.filters.from = $event"
      @update:to="filterStore.filters.to = $event"
    />
    <FilterSelect v-model="filterStore.filters.level" label="level" :options="levelOptions" />
    <FilterSelect v-model="filterStore.filters.host" label="host" :options="hostOptions" searchable />
    <FilterSelect v-model="filterStore.filters.service" label="service" :options="serviceOptions" />
    <FilterSelect v-model="filterStore.filters.component" label="component" :options="componentOptions" />
    <label class="flex items-center gap-1">
      <span class="text-t-fg-dark text-xs">search</span>
      <input
        v-model="searchInput"
        type="text"
        placeholder="message…"
        aria-label="Search applog messages"
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

    <span v-if="filterStore.filters.host && !filterStore.filters.host.includes('*')" class="text-t-fg-dark text-xs">-</span>
    <RouterLink
      v-if="filterStore.filters.host && !filterStore.filters.host.includes('*')"
      :to="{ name: 'applog-device-detail', params: { hostname: filterStore.filters.host } }"
      class="text-t-teal text-xs hover:underline"
    >
      {{ filterStore.filters.host }} details &rarr;
    </RouterLink>
  </div>
</template>
