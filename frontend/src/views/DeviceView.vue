<script setup lang="ts">
// Srvlog device view — thin config binding over the shared DeviceLogView.
import { api } from '@/lib/api'
import { useDeviceLogs } from '@/composables/useDeviceLogs'
import SrvlogRow from '@/components/SrvlogRow.vue'
import DeviceLogView from './DeviceLogView.vue'

defineProps<{ hostname: string }>()
</script>

<template>
  <DeviceLogView
    :hostname="hostname"
    :use-logs="useDeviceLogs"
    :fetch-summary="api.getSrvlogDeviceSummary"
    :row="SrvlogRow"
    list-route="srvlog"
    list-label="go to srvlog"
    detail-route="srvlog-detail"
    highlight-prefix="srvlog"
  />
</template>
