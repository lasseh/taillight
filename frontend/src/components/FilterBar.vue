<script setup lang="ts">
// Srvlog filter bar — thin config binding over the shared LogFilterBar.
import { useSrvlogFilterStore } from '@/stores/srvlog-filters'
import { useMetaStore } from '@/stores/meta'
import LogFilterBar from '@/components/LogFilterBar.vue'

const filterStore = useSrvlogFilterStore()
const meta = useMetaStore()
</script>

<template>
  <LogFilterBar
    :filter-store="filterStore"
    :meta="meta"
    device-route="srvlog-device-detail"
    export-path="/api/v1/srvlog/export"
    search-label="Search srvlog messages"
  />
</template>
