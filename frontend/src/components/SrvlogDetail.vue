<script setup lang="ts">
// Srvlog detail panel — thin config binding over the shared LogDetail.
import type { SrvlogEvent } from '@/types/srvlog'
import { useSrvlogFilterStore } from '@/stores/srvlog-filters'
import LogDetail from '@/components/LogDetail.vue'

defineProps<{ event: SrvlogEvent }>()

const filterStore = useSrvlogFilterStore()
</script>

<template>
  <LogDetail
    :event="event"
    :filter-store="filterStore"
    detail-route="srvlog-detail"
    device-route="srvlog-device-detail"
  />
</template>
