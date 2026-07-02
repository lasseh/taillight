<script setup lang="ts">
// Netlog event row — thin config binding over the shared LogRow.
import type { NetlogEvent } from '@/types/netlog'
import { useNetlogFilterStore } from '@/stores/netlog-filters'
import NetlogDetail from '@/components/NetlogDetail.vue'
import LogRow from '@/components/LogRow.vue'

defineProps<{ event: NetlogEvent; showProgram?: boolean }>()

const filterStore = useNetlogFilterStore()
</script>

<template>
  <LogRow
    :event="event"
    :show-program="showProgram"
    :detail="NetlogDetail"
    :filter-store="filterStore"
    highlight-prefix="netlog"
  />
</template>
