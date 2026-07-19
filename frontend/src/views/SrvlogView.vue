<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { SrvlogEvent } from '@/types/srvlog'
import { api, ApiError } from '@/lib/api'
import { severityColorClass } from '@/lib/constants'
import { highlight } from '@/lib/highlighter'
import { formatDateTime } from '@/lib/format'
import { selectedRowsText } from '@/lib/copy'
import ErrorDisplay from '@/components/ErrorDisplay.vue'

const props = defineProps<{
  id: string
}>()

const router = useRouter()
const event = ref<SrvlogEvent | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

const sevClass = computed(() =>
  event.value ? (severityColorClass[event.value.severity] ?? 'text-t-fg') : 'text-t-fg',
)

const highlightedMsg = computed(() => (event.value ? highlight(event.value.message) : ''))

const highlightedRaw = computed(() =>
  event.value?.raw_message ? highlight(event.value.raw_message) : '',
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

watch(
  () => props.id,
  async (id) => {
    const version = ++fetchVersion
    event.value = null
    loading.value = true
    error.value = null
    errorStatus.value = null

    const numId = Number(id)
    if (!Number.isInteger(numId) || numId <= 0) {
      errorStatus.value = 404
      error.value = `srvlog #${id} does not exist`
      loading.value = false
      return
    }
    try {
      const res = await api.getSrvlog(numId)
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
  },
  { immediate: true },
)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-4">
      <button class="text-t-teal mb-4 text-xs hover:underline" @click="router.back()">
        &larr; back
      </button>

      <div v-if="loading" class="text-t-fg-dark text-xs">loading...</div>
      <ErrorDisplay
        v-else-if="error && errorStatus === 404"
        :code="404"
        title="srvlog not found"
        :message="`srvlog #${props.id} does not exist`"
        :show-back="false"
        list-route="srvlog"
        list-label="go to srvlog"
      />
      <ErrorDisplay
        v-else-if="error && errorStatus"
        :code="errorStatus"
        title="failed to load srvlog"
        :message="error"
        :show-back="false"
        list-route="srvlog"
        list-label="go to srvlog"
      />
      <ErrorDisplay
        v-else-if="error"
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="false"
        list-route="srvlog"
        list-label="go to srvlog"
      />

      <div v-else-if="event" class="mx-auto max-w-7xl space-y-4" @copy="onCopy">
        <!-- Header: severity + message -->
        <div class="bg-t-bg-dark rounded p-4">
          <div
            class="mb-2"
            :data-copytext="`severity: ${event.severity_label} (${event.severity})`"
          >
            <span class="text-xs font-semibold uppercase" :class="sevClass">
              {{ event.severity_label }}
            </span>
          </div>
          <p
            class="text-t-fg break-all font-mono text-sm leading-relaxed"
            :data-copytext="`message: ${event.message}`"
            v-html="highlightedMsg"
          />
        </div>

        <!-- Metadata grid -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3
            class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide"
          >
            Details
          </h3>
          <div class="divide-t-border divide-y text-sm">
            <div
              class="flex gap-2 px-4 py-1.5"
              :data-copytext="`received: ${formatDateTime(event.received_at)}`"
            >
              <span class="text-t-fg-dark w-24 shrink-0 text-right">received</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.received_at) }}</span>
            </div>
            <div
              class="flex gap-2 px-4 py-1.5"
              :data-copytext="`reported: ${formatDateTime(event.reported_at)}`"
            >
              <span class="text-t-fg-dark w-24 shrink-0 text-right">reported</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.reported_at) }}</span>
            </div>
            <div
              class="flex gap-2 px-4 py-1.5"
              :data-copytext="`hostname: ${event.hostname || '–'}`"
            >
              <span class="text-t-fg-dark w-24 shrink-0 text-right">hostname</span>
              <RouterLink
                :to="{ name: 'srvlog-device-detail', params: { hostname: event.hostname } }"
                class="text-t-teal font-mono hover:underline"
              >
                {{ event.hostname || '–' }} <span class="text-t-fg-dark text-xs">&rarr;</span>
              </RouterLink>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`ip: ${event.fromhost_ip || '–'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">ip</span>
              <span class="text-t-blue font-mono">{{ event.fromhost_ip || '–' }}</span>
            </div>
            <div
              class="flex gap-2 px-4 py-1.5"
              :data-copytext="`program: ${event.programname || '–'}`"
            >
              <span class="text-t-fg-dark w-24 shrink-0 text-right">program</span>
              <span class="text-t-purple font-mono">{{ event.programname || '–' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`msgid: ${event.msgid || '–'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">msgid</span>
              <span class="text-t-fg font-mono">{{ event.msgid || '–' }}</span>
            </div>
            <div
              class="flex gap-2 px-4 py-1.5"
              :data-copytext="`severity: ${event.severity_label} (${event.severity})`"
            >
              <span class="text-t-fg-dark w-24 shrink-0 text-right">severity</span>
              <span class="font-mono" :class="sevClass"
                >{{ event.severity_label }} ({{ event.severity }})</span
              >
            </div>
            <div
              class="flex gap-2 px-4 py-1.5"
              :data-copytext="`facility: ${event.facility_label} (${event.facility})`"
            >
              <span class="text-t-fg-dark w-24 shrink-0 text-right">facility</span>
              <span class="text-t-orange font-mono"
                >{{ event.facility_label }} ({{ event.facility }})</span
              >
            </div>
          </div>
        </div>

        <!-- Structured data -->
        <div v-if="event.structured_data" class="bg-t-bg-dark border-t-border rounded border">
          <h3
            class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide"
          >
            Structured Data
          </h3>
          <pre
            class="text-t-fg overflow-x-auto p-4 font-mono text-xs leading-relaxed"
            :data-copytext="`structured data: ${event.structured_data}`"
            >{{ event.structured_data }}</pre
          >
        </div>

        <!-- Raw message -->
        <div v-if="event.raw_message" class="bg-t-bg-dark border-t-border rounded border">
          <h3
            class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide"
          >
            Raw Message
          </h3>
          <pre
            class="text-t-fg overflow-x-auto p-4 font-mono text-xs leading-relaxed"
            :data-copytext="`raw message: ${event.raw_message}`"
            v-html="highlightedRaw"
          ></pre>
        </div>
      </div>
    </div>
  </div>
</template>
