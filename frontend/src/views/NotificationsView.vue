<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import NotificationChannels from '@/components/NotificationChannels.vue'
import NotificationRules from '@/components/NotificationRules.vue'
import MuteRules from '@/components/MuteRules.vue'
import NotificationSummaries from '@/components/NotificationSummaries.vue'
import NotificationLog from '@/components/NotificationLog.vue'

const tabs = ['Channels', 'Rules', 'Mutes', 'Summaries', 'Log'] as const
type Tab = (typeof tabs)[number]

const route = useRoute()
const router = useRouter()

// The tab is ?tab= (the lowercased label), read live so a query-only
// navigation that reuses this view, like the top-nav link, switches it. The
// query is only rewritten on a tab click, never on mount, so a mute-similar
// prefill in the URL survives until MuteRules reads it.
const activeTab = computed<Tab>(
  () => tabs.find((t) => t.toLowerCase() === route.query.tab) ?? 'Channels',
)

function selectTab(tab: Tab) {
  router.replace({ query: tab === 'Channels' ? {} : { tab: tab.toLowerCase() } })
}
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
        <div class="border-t-border flex gap-0 overflow-x-auto border-b">
          <button
            v-for="tab in tabs"
            :key="tab"
            class="relative whitespace-nowrap px-4 py-2 text-xs font-medium transition-colors"
            :class="activeTab === tab ? 'text-t-yellow' : 'text-t-fg-dark hover:text-t-fg'"
            @click="selectTab(tab)"
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
        <MuteRules v-else-if="activeTab === 'Mutes'" />
        <NotificationSummaries v-else-if="activeTab === 'Summaries'" />
        <NotificationLog v-else-if="activeTab === 'Log'" />
      </div>
    </div>
  </div>
</template>
