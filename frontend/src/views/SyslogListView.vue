<script setup lang="ts">
import { onActivated } from 'vue'
import { useSyslogEventStore } from '@/stores/syslog-events'
import SyslogTable from '@/components/SyslogTable.vue'

defineOptions({ name: 'SyslogListView' })

const events = useSyslogEventStore()

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
  <SyslogTable />
</template>
