<script setup lang="ts">
// Netlog filter bar — thin config binding over the shared LogFilterBar.
import { useNetlogFilterStore } from '@/stores/netlog-filters'
import { useNetlogMetaStore } from '@/stores/netlog-meta'
import LogFilterBar from '@/components/LogFilterBar.vue'

const filterStore = useNetlogFilterStore()
const meta = useNetlogMetaStore()
</script>

<template>
  <LogFilterBar
    :filter-store="filterStore"
    :meta="meta"
    device-route="netlog-device-detail"
    export-path="/api/v1/netlog/export"
    search-label="Search netlog messages"
  />
</template>
