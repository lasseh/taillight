<script setup lang="ts">
// Netlog detail panel — thin config binding over the shared LogDetail.
import type { NetlogEvent } from '@/types/netlog'
import { useNetlogFilterStore } from '@/stores/netlog-filters'
import LogDetail from '@/components/LogDetail.vue'

defineProps<{ event: NetlogEvent }>()

const filterStore = useNetlogFilterStore()
</script>

<template>
  <LogDetail
    :event="event"
    :filter-store="filterStore"
    detail-route="netlog-detail"
    device-route="netlog-device-detail"
  />
</template>
