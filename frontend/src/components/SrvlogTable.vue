<script setup lang="ts">
// Srvlog live table — thin config binding over the shared LogTable.
import { useSrvlogEventStore } from '@/stores/srvlog-events'
import SrvlogRow from '@/components/SrvlogRow.vue'
import LogTable from '@/components/LogTable.vue'

const events = useSrvlogEventStore()
</script>

<template>
  <LogTable :store="events" route-name="srvlog" :row="SrvlogRow" />
</template>
