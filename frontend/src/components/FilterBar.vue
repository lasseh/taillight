<script setup lang="ts">
import { computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useSyslogFilterStore } from '@/stores/syslog-filters'
import { useMetaStore } from '@/stores/meta'
import { facilityLabels, severityOptions } from '@/lib/constants'
import FilterSelect from '@/components/FilterSelect.vue'

const filterStore = useSyslogFilterStore()
const meta = useMetaStore()

const hostOptions = computed(() =>
  meta.hosts.map((h) => ({ value: h, label: h })),
)

const programOptions = computed(() =>
  meta.programs.map((p) => ({ value: p, label: p })),
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
  <div class="border-t-border bg-t-bg-dark flex flex-wrap items-center gap-3 border-b px-4 py-1.5">
    <FilterSelect v-model="filterStore.filters.severity_max" label="severity" :options="severityOptions" />
    <FilterSelect v-model="filterStore.filters.hostname" label="host" :options="hostOptions" />
    <FilterSelect v-model="filterStore.filters.programname" label="program" :options="programOptions" />
    <FilterSelect v-model="filterStore.filters.facility" label="facility" :options="facilityOptions" />
    <label class="flex items-center gap-1">
      <span class="text-t-fg-dark text-xs">search</span>
      <input
        v-model="searchInput"
        type="text"
        placeholder="message…"
        class="bg-t-bg-dark border-t-border text-t-fg placeholder:text-t-fg-gutter hover:border-t-terminal focus:border-t-blue w-64 border px-2 py-0.5 text-xs outline-none"
      />
    </label>

    <button
      v-if="filterStore.hasActiveFilters"
      class="text-t-red text-xs"
      @click="filterStore.clearAll()"
    >
      clear
    </button>
  </div>
</template>
