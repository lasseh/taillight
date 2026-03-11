<script setup lang="ts">
import { RouterLink } from 'vue-router'

withDefaults(defineProps<{
  code?: number
  title: string
  message?: string
  showBack?: boolean
  listRoute?: string
  listLabel?: string
}>(), {
  showBack: true,
})
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-4">
    <!-- Header: error badge + title (mirrors severity + message panel) -->
    <div class="bg-t-bg-dark border-t-red rounded border-l-2 p-4">
      <div class="mb-2 flex items-center gap-2">
        <span class="text-t-red text-xs font-semibold uppercase">
          {{ code ?? 'error' }}
        </span>
      </div>
      <p class="text-t-fg break-all font-mono text-sm leading-relaxed">{{ title }}</p>
    </div>

    <!-- Metadata grid (mirrors Details panel) -->
    <div v-if="code || message" class="bg-t-bg-dark border-t-border rounded border">
      <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
        Details
      </h3>
      <dl class="grid grid-cols-[auto_1fr] text-sm">
        <template v-if="code">
          <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">status</dt>
          <dd class="text-t-red border-t-border border-b px-4 py-1.5 font-mono">{{ code }}</dd>
        </template>

        <template v-if="message">
          <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">message</dt>
          <dd class="text-t-fg border-t-border border-b px-4 py-1.5 font-mono">{{ message }}</dd>
        </template>
      </dl>
    </div>

    <!-- Navigation link below panels (mirrors back button style) -->
    <div v-if="listRoute" class="flex items-center gap-4 text-xs">
      <button
        v-if="showBack"
        class="text-t-fg-dark hover:text-t-fg transition-colors"
        @click="$router.back()"
      >
        &larr; go back
      </button>
      <RouterLink
        :to="{ name: listRoute }"
        class="text-t-fg-dark hover:text-t-fg transition-colors"
      >
        {{ listLabel ?? 'go to list' }} &rarr;
      </RouterLink>
    </div>
  </div>
</template>
