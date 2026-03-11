<script setup lang="ts">
import { computed } from 'vue'
import { useSyslogEventStore } from '@/stores/syslog-events'
import EventTable from '@/components/EventTable.vue'
import SyslogRow from '@/components/SyslogRow.vue'

const events = useSyslogEventStore()

const colWidths = computed(() => {
  let maxHost = 0
  let maxProg = 0
  for (const e of events.events) {
    if (e.hostname.length > maxHost) maxHost = e.hostname.length
    if (e.programname.length > maxProg) maxProg = e.programname.length
  }
  return {
    '--col-host': `${Math.min(20, Math.max(8, maxHost + 1))}ch`,
    '--col-prog': `${Math.min(16, Math.max(6, maxProg + 1))}ch`,
  }
})
</script>

<template>
  <EventTable
    route-name="syslog"
    :events="events.events"
    :loading="events.loading"
    :error="events.error"
    :has-more="events.hasMore"
    :load-history="events.loadHistory"
    :style="colWidths"
  >
    <template #default="{ item }">
      <SyslogRow :event="item" />
    </template>
  </EventTable>
</template>
