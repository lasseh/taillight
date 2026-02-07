<script setup lang="ts">
import type { AppLogEvent } from '@/types/applog'
import { useAppLogEventStore } from '@/stores/applog-events'
import EventTable from '@/components/EventTable.vue'
import AppLogRow from '@/components/AppLogRow.vue'

const events = useAppLogEventStore()
</script>

<template>
  <EventTable
    route-name="applog"
    :events="events.events"
    :loading="events.loading"
    :error="events.error"
    :has-more="events.hasMore"
    :load-history="events.loadHistory"
  >
    <template #default="{ item }">
      <AppLogRow :event="(item as AppLogEvent)" />
    </template>
  </EventTable>
</template>
