<script setup lang="ts">
import { useSyslogEventStore } from '@/stores/syslog-events'
import EventTable from '@/components/EventTable.vue'
import SyslogRow from '@/components/SyslogRow.vue'

const events = useSyslogEventStore()
</script>

<template>
  <EventTable
    route-name="syslog"
    :events="events.events"
    :loading="events.loading"
    :error="events.error"
    :has-more="events.hasMore"
    :load-history="events.loadHistory"
  >
    <template #default="{ item }">
      <SyslogRow :event="item" />
    </template>
  </EventTable>
</template>
