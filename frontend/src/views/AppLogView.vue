<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { AppLogEvent } from '@/types/applog'
import { api, ApiError } from '@/lib/api'
import { levelColorClass, levelBorderClass } from '@/lib/applog-constants'
import { formatDateTime, highlightAttrs } from '@/lib/format'
import { selectedRowsText } from '@/lib/copy'
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

function onCopy(ev: ClipboardEvent) {
  const container = ev.currentTarget as Element | null
  if (!container) return
  const text = selectedRowsText(container, window.getSelection())
  if (text == null) return
  ev.preventDefault()
  ev.clipboardData?.setData('text/plain', text)
}

let fetchVersion = 0

watch(() => props.id, async (id) => {
  const version = ++fetchVersion
  event.value = null
  loading.value = true
  error.value = null
  errorStatus.value = null

  const numId = Number(id)
  if (!Number.isInteger(numId) || numId <= 0) {
    errorStatus.value = 404
    error.value = `applog #${id} does not exist`
    loading.value = false
    return
  }
  try {
    const res = await api.getAppLog(numId)
    if (version !== fetchVersion) return
    event.value = res.data
  } catch (e) {
    if (version !== fetchVersion) return
    if (e instanceof ApiError) {
      errorStatus.value = e.status
      error.value = e.message
    } else {
      error.value = e instanceof Error ? e.message : 'failed to load event'
    }
  } finally {
    if (version === fetchVersion) {
      loading.value = false
    }
  }
}, { immediate: true })
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

      <div v-else-if="event" class="mx-auto max-w-7xl space-y-4" @copy="onCopy">
        <!-- Message + Fields -->
        <div
          class="bg-t-bg-dark rounded border-l-2 p-4"
          :class="borderClass"
        >
          <div class="mb-2" :data-copytext="`level: ${event.level}`">
            <span class="text-xs font-semibold uppercase" :class="lvlClass">
              {{ event.level }}
            </span>
          </div>
          <p class="text-t-fg break-all font-mono text-sm leading-relaxed" :data-copytext="`message: ${event.msg}`">{{ event.msg }}</p>
          <div v-if="event.attrs && Object.keys(event.attrs).length > 0" class="border-t-border mt-3 border-t pt-3">
            <span class="text-t-fg-dark mb-1 block text-xs font-semibold uppercase tracking-wide">Fields</span>
            <pre
              class="language-json text-t-fg overflow-x-auto font-mono text-xs leading-relaxed"
              :data-copytext="`attrs: ${JSON.stringify(event.attrs, null, 2)}`"
              v-html="highlightAttrs(event.attrs)"
            ></pre>
          </div>
        </div>

        <!-- Metadata grid -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Details
          </h3>
          <div class="divide-t-border divide-y text-sm">
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`received: ${formatDateTime(event.received_at)}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">received</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.received_at) }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`timestamp: ${formatDateTime(event.timestamp)}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">timestamp</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.timestamp) }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`level: ${event.level}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">level</span>
              <span class="font-mono" :class="lvlClass">{{ event.level }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`host: ${event.host || '–'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">host</span>
              <RouterLink
                v-if="event.host"
                :to="{ name: 'applog-device-detail', params: { hostname: event.host } }"
                class="text-t-teal font-mono hover:underline"
              >
                {{ event.host }} <span class="text-t-fg-dark text-xs">&rarr;</span>
              </RouterLink>
              <span v-else class="text-t-teal font-mono">–</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`service: ${event.service || '–'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">service</span>
              <span class="text-t-purple font-mono">{{ event.service || '–' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`component: ${event.component || '–'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">component</span>
              <span class="text-t-yellow font-mono">{{ event.component || '–' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`source: ${event.source || '–'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">source</span>
              <span class="text-t-blue font-mono">{{ event.source || '–' }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
