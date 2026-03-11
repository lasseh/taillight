<script setup lang="ts">
import { onActivated } from 'vue'
import { useAppLogEventStore } from '@/stores/applog-events'
import AppLogTable from '@/components/AppLogTable.vue'

defineOptions({ name: 'AppLogListView' })

const events = useAppLogEventStore()

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
  <AppLogTable />
</template>
