<script setup lang="ts">
import ErrorDisplay from '@/components/ErrorDisplay.vue'
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-4">
      <ErrorDisplay
        :code="404"
        title="page not found"
        message="the page you're looking for doesn't exist"
        list-route="syslog"
        list-label="go to syslog"
      />
    </div>
  </div>
</template>
