<script setup lang="ts">
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import { useFeaturesStore } from '@/stores/features'

const { features } = useFeaturesStore()
const defaultRoute = features.netlog ? 'netlog' : features.srvlog ? 'srvlog' : features.applog ? 'applog' : 'home'
const defaultLabel = `go to ${defaultRoute}`
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-4">
      <ErrorDisplay
        :code="404"
        title="page not found"
        message="the page you're looking for doesn't exist"
        :list-route="defaultRoute"
        :list-label="defaultLabel"
      />
    </div>
  </div>
</template>
