<script setup lang="ts">
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import { useFeaturesStore } from '@/stores/features'

const props = defineProps<{
  feature: string
}>()

const { features } = useFeaturesStore()
const defaultRoute = features.netlog ? 'netlog' : features.srvlog ? 'srvlog' : features.applog ? 'applog' : 'home'
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-4">
      <ErrorDisplay
        title="feature not enabled"
        :message="`The ${props.feature} feed is not enabled on this instance.`"
        :list-route="defaultRoute"
        :list-label="`go to ${defaultRoute}`"
      />
    </div>
  </div>
</template>
