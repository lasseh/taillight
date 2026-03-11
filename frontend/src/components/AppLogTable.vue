<script setup lang="ts">
import { computed } from 'vue'
import { useAppLogEventStore } from '@/stores/applog-events'
import EventTable from '@/components/EventTable.vue'
import AppLogRow from '@/components/AppLogRow.vue'

const events = useAppLogEventStore()

const colWidths = computed(() => {
  let maxHost = 0
  let maxSvc = 0
  let maxComp = 0
  for (const e of events.events) {
    if (e.host.length > maxHost) maxHost = e.host.length
    if (e.service.length > maxSvc) maxSvc = e.service.length
    if (e.component && e.component.length > maxComp) maxComp = e.component.length
  }
  return {
    '--col-host': `${Math.min(20, Math.max(8, maxHost + 1))}ch`,
    '--col-svc': `${Math.min(16, Math.max(6, maxSvc + 1))}ch`,
    '--col-comp': `${Math.min(16, Math.max(6, maxComp + 1))}ch`,
  }
})
</script>

<template>
  <EventTable
    route-name="applog"
    :events="events.events"
    :loading="events.loading"
    :error="events.error"
    :has-more="events.hasMore"
    :load-history="events.loadHistory"
    :style="colWidths"
  >
    <template #default="{ item }">
      <AppLogRow :event="item" />
    </template>
  </EventTable>
</template>
