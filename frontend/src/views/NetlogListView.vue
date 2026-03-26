<script setup lang="ts">
import { onActivated } from 'vue'
import { useNetlogEventStore } from '@/stores/netlog-events'
import NetlogTable from '@/components/NetlogTable.vue'

defineOptions({ name: 'NetlogListView' })

const events = useNetlogEventStore()

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
  <NetlogTable />
</template>
