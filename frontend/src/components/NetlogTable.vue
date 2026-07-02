<script setup lang="ts">
// Netlog live table — thin config binding over the shared LogTable.
import { useNetlogEventStore } from '@/stores/netlog-events'
import NetlogRow from '@/components/NetlogRow.vue'
import LogTable from '@/components/LogTable.vue'

const events = useNetlogEventStore()
</script>

<template>
  <LogTable :store="events" route-name="netlog" :row="NetlogRow" />
</template>
