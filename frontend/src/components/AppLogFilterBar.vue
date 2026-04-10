<script setup lang="ts">
import { ref, computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useAppLogFilterStore } from '@/stores/applog-filters'
import { useAppLogMetaStore } from '@/stores/applog-meta'
import { levelOptions } from '@/lib/applog-constants'
import FilterSelect from '@/components/FilterSelect.vue'
import DateRangePicker from '@/components/DateRangePicker.vue'
import { config } from '@/lib/config'

const filterStore = useAppLogFilterStore()
const meta = useAppLogMetaStore()

const mobileOpen = ref(false)
const activeFilterCount = computed(() => Object.keys(filterStore.activeFilters).length)

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

const canExport = computed(() => !!filterStore.filters.from && !!filterStore.filters.to)

function exportCSV() {
  const params = new URLSearchParams(filterStore.activeFilters)
  window.open(`${config.apiUrl}/api/v1/applog/export?${params}`, '_blank')
}
</script>

<template>
  <!-- Mobile trigger bar -->
  <div
    class="border-t-border bg-t-bg-dark flex items-center border-b px-4 py-1.5 md:hidden"
    @click="mobileOpen = !mobileOpen"
  >
    <div class="flex flex-1 items-center gap-2">
      <svg class="text-t-fg-dark h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 3H2l8 9.46V19l4 2v-8.54L22 3z"/></svg>
      <span class="text-t-fg-dark text-xs">filters</span>
      <span v-if="activeFilterCount" class="bg-t-blue/20 text-t-blue rounded-full px-1.5 text-xs">{{ activeFilterCount }}</span>
    </div>
    <div class="flex items-center gap-3">
      <button
        v-if="filterStore.hasActiveFilters"
        class="text-t-red text-xs"
        aria-label="Clear all filters"
        @click.stop="filterStore.clearAll()"
      >
        clear
      </button>
      <RouterLink
        v-if="filterStore.filters.host && !filterStore.filters.host.includes('*')"
        :to="{ name: 'applog-device-detail', params: { hostname: filterStore.filters.host } }"
        class="text-t-teal text-xs hover:underline"
        @click.stop
      >
        {{ filterStore.filters.host }} &rarr;
      </RouterLink>
      <button
        v-if="canExport"
        class="text-t-blue text-xs hover:underline"
        aria-label="Export filtered logs as CSV"
        @click.stop="exportCSV"
      >
        export csv
      </button>
    </div>
  </div>

  <!-- Mobile filter panel -->
  <Transition name="filter-panel">
    <div v-if="mobileOpen" class="border-t-border bg-t-bg-dark flex flex-col gap-3 border-b px-4 py-2 md:hidden">
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
          class="bg-t-bg-dark border-t-border text-t-fg placeholder:text-t-fg-gutter hover:border-t-terminal focus:border-t-blue w-full border px-2 py-0.5 text-xs outline-none"
        />
      </label>
      <button
        class="bg-t-blue/15 text-t-blue w-full rounded py-1.5 text-xs active:bg-t-blue/25"
        @click="mobileOpen = false"
      >
        apply
      </button>
    </div>
  </Transition>

  <!-- Desktop filter bar -->
  <div class="border-t-border bg-t-bg-dark hidden flex-wrap items-center gap-3 border-b px-4 py-1.5 md:flex">
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

    <button
      v-if="canExport"
      class="text-t-blue ml-auto text-xs hover:underline"
      aria-label="Export filtered logs as CSV"
      @click="exportCSV"
    >
      export csv
    </button>
  </div>
</template>

<style scoped>
.filter-panel-enter-active,
.filter-panel-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
  transform-origin: top;
}
.filter-panel-enter-from,
.filter-panel-leave-to {
  opacity: 0;
  transform: scaleY(0.95);
}
</style>
