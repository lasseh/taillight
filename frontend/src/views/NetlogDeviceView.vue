<script setup lang="ts">
// Netlog device view — thin config binding over the shared DeviceLogView.
import { api } from '@/lib/api'
import { useNetlogDeviceLogs } from '@/composables/useNetlogDeviceLogs'
import NetlogRow from '@/components/NetlogRow.vue'
import DeviceLogView from './DeviceLogView.vue'

defineProps<{ hostname: string }>()
</script>

<template>
  <DeviceLogView
    :hostname="hostname"
    :use-logs="useNetlogDeviceLogs"
    :fetch-summary="api.getNetlogDeviceSummary"
    :row="NetlogRow"
    list-route="netlog"
    list-label="go to netlog"
    detail-route="netlog-detail"
    highlight-prefix="netlog"
  />
</template>
