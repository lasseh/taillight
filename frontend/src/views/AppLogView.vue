<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import type { AppLogEvent } from '@/types/applog'
import { api, ApiError } from '@/lib/api'
import { levelColorClass, levelBorderClass } from '@/lib/applog-constants'
import { formatDateTime, highlightAttrs } from '@/lib/format'
import ErrorDisplay from '@/components/ErrorDisplay.vue'

const props = defineProps<{
  id: string
}>()

const router = useRouter()
const event = ref<AppLogEvent | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

const borderClass = computed(() =>
  event.value ? (levelBorderClass[event.value.level] ?? 'border-t-border') : 'border-t-border',
)

const lvlClass = computed(() =>
  event.value ? (levelColorClass[event.value.level] ?? 'text-t-fg') : 'text-t-fg',
)

onMounted(async () => {
  const numId = Number(props.id)
  if (!Number.isInteger(numId) || numId <= 0) {
    errorStatus.value = 404
    error.value = `applog #${props.id} does not exist`
    loading.value = false
    return
  }
  try {
    const res = await api.getAppLog(numId)
    event.value = res.data
  } catch (e) {
    if (e instanceof ApiError && e.code !== 'unknown') {
      errorStatus.value = e.status
      error.value = e.message
    } else {
      error.value = e instanceof Error ? e.message : 'failed to load event'
    }
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-4">
      <button
        class="text-t-fg-dark hover:text-t-fg mb-4 text-xs transition-colors"
        @click="router.back()"
      >
        &larr; back
      </button>

      <div v-if="loading" class="text-t-fg-dark text-xs">loading...</div>
      <ErrorDisplay
        v-else-if="error && errorStatus === 404"
        :code="404"
        title="applog not found"
        :message="`applog #${props.id} does not exist`"
        :show-back="false"
        list-route="applog"
        list-label="go to applogs"
      />
      <ErrorDisplay
        v-else-if="error && errorStatus"
        :code="errorStatus"
        title="failed to load applog"
        :message="error"
        :show-back="false"
        list-route="applog"
        list-label="go to applogs"
      />
      <ErrorDisplay
        v-else-if="error"
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="false"
        list-route="applog"
        list-label="go to applogs"
      />

      <div v-else-if="event" class="mx-auto max-w-7xl space-y-4">
        <!-- Header: level + message -->
        <div
          class="bg-t-bg-dark rounded border-l-2 p-4"
          :class="borderClass"
        >
          <div class="mb-2 flex items-center gap-2">
            <span class="text-xs font-semibold uppercase" :class="lvlClass">
              {{ event.level }}
            </span>
            <span class="text-t-fg-dark text-xs">#{{ event.id }}</span>
          </div>
          <p class="text-t-fg break-all font-mono text-sm leading-relaxed">{{ event.msg }}</p>
        </div>

        <!-- Metadata grid -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Details
          </h3>
          <dl class="grid grid-cols-[auto_1fr] text-sm">
            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">received</dt>
            <dd class="text-t-fg border-t-border border-b px-4 py-1.5 font-mono">{{ formatDateTime(event.received_at) }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">timestamp</dt>
            <dd class="text-t-fg border-t-border border-b px-4 py-1.5 font-mono">{{ formatDateTime(event.timestamp) }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">level</dt>
            <dd class="border-t-border border-b px-4 py-1.5 font-mono" :class="lvlClass">{{ event.level }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">host</dt>
            <dd class="text-t-teal border-t-border border-b px-4 py-1.5 font-mono">{{ event.host || '–' }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">service</dt>
            <dd class="text-t-purple border-t-border border-b px-4 py-1.5 font-mono">{{ event.service || '–' }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">component</dt>
            <dd class="text-t-yellow border-t-border border-b px-4 py-1.5 font-mono">{{ event.component || '–' }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">source</dt>
            <dd class="text-t-blue border-t-border border-b px-4 py-1.5 font-mono">{{ event.source || '–' }}</dd>
          </dl>
        </div>

        <!-- Attrs -->
        <div v-if="event.attrs && Object.keys(event.attrs).length > 0" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Attributes
          </h3>
          <pre class="language-json text-t-fg overflow-x-auto p-4 font-mono text-xs leading-relaxed" v-html="highlightAttrs(event.attrs)"></pre>
        </div>
      </div>
    </div>
  </div>
</template>
