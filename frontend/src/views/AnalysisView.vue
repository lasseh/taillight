<script setup lang="ts">
import { ref } from 'vue'
import AnalysisReports from '@/components/AnalysisReports.vue'
import AnalysisSchedules from '@/components/AnalysisSchedules.vue'

const tabs = ['Reports', 'Schedules'] as const
type Tab = (typeof tabs)[number]
const activeTab = ref<Tab>('Reports')
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-7xl space-y-5">
        <div>
          <h2 class="text-t-fg text-base font-semibold">Analysis</h2>
          <p class="text-t-fg-dark mt-1 text-sm">
            AI-generated reports on log activity — trigger on demand or run on a recurring schedule
          </p>
        </div>

        <div class="border-t-border flex gap-0 border-b">
          <button
            v-for="tab in tabs"
            :key="tab"
            class="relative px-4 py-2 text-xs font-medium transition-colors"
            :class="activeTab === tab ? 'text-t-orange' : 'text-t-fg-dark hover:text-t-fg'"
            @click="activeTab = tab"
          >
            {{ tab }}
            <span
              v-if="activeTab === tab"
              class="bg-t-orange absolute bottom-0 left-0 h-px w-full"
            />
          </button>
        </div>

        <AnalysisReports v-if="activeTab === 'Reports'" />
        <AnalysisSchedules v-else-if="activeTab === 'Schedules'" />
      </div>
    </div>
  </div>
</template>
