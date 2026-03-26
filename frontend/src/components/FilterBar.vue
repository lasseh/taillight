<script setup lang="ts">
import { ref, computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useSrvlogFilterStore } from '@/stores/srvlog-filters'
import { useMetaStore } from '@/stores/meta'
import { facilityLabels, severityOptions } from '@/lib/constants'
import FilterSelect from '@/components/FilterSelect.vue'
import DateRangePicker from '@/components/DateRangePicker.vue'

const filterStore = useSrvlogFilterStore()
const meta = useMetaStore()

const mobileOpen = ref(false)
const activeFilterCount = computed(() => Object.keys(filterStore.activeFilters).length)

const hostOptions = computed(() =>
  meta.hosts.map((h) => ({ value: h, label: h, colorClass: 'text-t-teal' })),
)

const programOptions = computed(() =>
  meta.programs.map((p) => ({ value: p, label: p, colorClass: 'text-t-purple' })),
)

const facilityOptions = computed(() =>
  meta.facilities.map((f) => ({
    value: String(f),
    label: facilityLabels[f] ?? String(f),
  })),
)

const searchInput = computed({
  get: () => filterStore.filters.search,
  set: useDebounceFn((val: string) => {
    filterStore.filters.search = val
  }, 300),
})
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
        v-if="filterStore.filters.hostname && !filterStore.filters.hostname.includes('*')"
        :to="{ name: 'device-detail', params: { hostname: filterStore.filters.hostname } }"
        class="text-t-teal text-xs hover:underline"
        @click.stop
      >
        {{ filterStore.filters.hostname }} &rarr;
      </RouterLink>
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
      <FilterSelect v-model="filterStore.filters.severity_max" label="severity" :options="severityOptions" />
      <FilterSelect v-model="filterStore.filters.hostname" label="host" :options="hostOptions" searchable />
      <FilterSelect v-model="filterStore.filters.programname" label="program" :options="programOptions" />
      <FilterSelect v-model="filterStore.filters.facility" label="facility" :options="facilityOptions" />
      <label class="flex items-center gap-1">
        <span class="text-t-fg-dark text-xs">search</span>
        <input
          v-model="searchInput"
          type="text"
          placeholder="message…"
          aria-label="Search srvlog messages"
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
    <FilterSelect v-model="filterStore.filters.severity_max" label="severity" :options="severityOptions" />
    <FilterSelect v-model="filterStore.filters.hostname" label="host" :options="hostOptions" searchable />
    <FilterSelect v-model="filterStore.filters.programname" label="program" :options="programOptions" />
    <FilterSelect v-model="filterStore.filters.facility" label="facility" :options="facilityOptions" />
    <label class="flex items-center gap-1">
      <span class="text-t-fg-dark text-xs">search</span>
      <input
        v-model="searchInput"
        type="text"
        placeholder="message…"
        aria-label="Search srvlog messages"
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

    <span v-if="filterStore.filters.hostname && !filterStore.filters.hostname.includes('*')" class="text-t-fg-dark text-xs">-</span>
    <RouterLink
      v-if="filterStore.filters.hostname && !filterStore.filters.hostname.includes('*')"
      :to="{ name: 'device-detail', params: { hostname: filterStore.filters.hostname } }"
      class="text-t-teal text-xs hover:underline"
    >
      {{ filterStore.filters.hostname }} details &rarr;
    </RouterLink>
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
