<script setup lang="ts">
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import { featuresLoaded } from '@/lib/features'

const props = defineProps<{
  feature: string
}>()

const defaultRoute = 'netlog'

// When the flags never loaded we don't actually know the feature is off — the
// fetch can fail for a second while the API restarts, and the router is built
// once at boot. Claiming "not enabled" there sends people hunting a config
// problem that doesn't exist, so say what really happened and point at a reload.
const unknown = !featuresLoaded()
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-4">
      <ErrorDisplay
        :title="unknown ? 'configuration unavailable' : 'feature not enabled'"
        :message="
          unknown
            ? `Could not load this instance's configuration, so the ${props.feature} feature is unavailable. Reload the page to try again.`
            : `The ${props.feature} feature is not enabled on this instance.`
        "
        :list-route="defaultRoute"
        :list-label="`go to ${defaultRoute}`"
      />
    </div>
  </div>
</template>
