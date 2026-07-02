<script setup lang="ts">
// Srvlog event row — thin config binding over the shared LogRow.
import type { SrvlogEvent } from '@/types/srvlog'
import { useSrvlogFilterStore } from '@/stores/srvlog-filters'
import SrvlogDetail from '@/components/SrvlogDetail.vue'
import LogRow from '@/components/LogRow.vue'

defineProps<{ event: SrvlogEvent; showProgram?: boolean }>()

const filterStore = useSrvlogFilterStore()
</script>

<template>
  <LogRow
    :event="event"
    :show-program="showProgram"
    :detail="SrvlogDetail"
    :filter-store="filterStore"
    highlight-prefix="srvlog"
  />
</template>
