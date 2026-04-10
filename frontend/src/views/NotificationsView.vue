<script setup lang="ts">
import { ref } from 'vue'
import NotificationChannels from '@/components/NotificationChannels.vue'
import NotificationRules from '@/components/NotificationRules.vue'
import NotificationSummaries from '@/components/NotificationSummaries.vue'
import NotificationLog from '@/components/NotificationLog.vue'

const tabs = ['Channels', 'Rules', 'Summaries', 'Log'] as const
type Tab = (typeof tabs)[number]
const activeTab = ref<Tab>('Channels')
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-5xl space-y-5">

        <!-- Page header -->
        <div>
          <h2 class="text-t-fg text-base font-semibold">Notifications</h2>
          <p class="text-t-fg-dark mt-1 text-sm">configure alert rules and notification channels</p>
        </div>

        <!-- Tabs -->
        <div class="border-t-border flex gap-0 border-b">
          <button
            v-for="tab in tabs"
            :key="tab"
            class="relative px-4 py-2 text-xs font-medium transition-colors"
            :class="
              activeTab === tab
                ? 'text-t-yellow'
                : 'text-t-fg-dark hover:text-t-fg'
            "
            @click="activeTab = tab"
          >
            {{ tab }}
            <span
              v-if="activeTab === tab"
              class="bg-t-yellow absolute bottom-0 left-0 h-px w-full"
            />
          </button>
        </div>

        <!-- Tab content -->
        <NotificationChannels v-if="activeTab === 'Channels'" />
        <NotificationRules v-else-if="activeTab === 'Rules'" />
        <NotificationSummaries v-else-if="activeTab === 'Summaries'" />
        <NotificationLog v-else-if="activeTab === 'Log'" />

      </div>
    </div>
  </div>
</template>
