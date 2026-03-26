<script setup lang="ts">
import { onActivated } from 'vue'
import { useSrvlogEventStore } from '@/stores/srvlog-events'
import SrvlogTable from '@/components/SrvlogTable.vue'

defineOptions({ name: 'SrvlogListView' })

const events = useSrvlogEventStore()

onActivated(() => {
  if (events.events.length === 0) {
    events.enter()
  }
  // SSE stays connected across route changes — no pause/resume needed.
  // The Pinia store is a singleton so events keep accumulating even
  // when this view is deactivated by KeepAlive.
})
</script>

<template>
  <SrvlogTable />
</template>
