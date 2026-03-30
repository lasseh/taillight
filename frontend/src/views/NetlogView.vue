<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { NetlogEvent, JuniperNetlogRef } from '@/types/netlog'
import { api, ApiError } from '@/lib/api'
import { severityColorClass, severityBorderClass } from '@/lib/constants'
import { highlight } from '@/lib/highlighter'
import { formatDateTime } from '@/lib/format'
import ErrorDisplay from '@/components/ErrorDisplay.vue'

const props = defineProps<{
  id: string
}>()

const router = useRouter()
const event = ref<NetlogEvent | null>(null)
const juniperRefs = ref<JuniperNetlogRef[]>([])
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

const borderClass = computed(() =>
  event.value ? (severityBorderClass[event.value.severity] ?? 'border-t-border') : 'border-t-border',
)

const sevClass = computed(() =>
  event.value ? (severityColorClass[event.value.severity] ?? 'text-t-fg') : 'text-t-fg',
)

const highlightedMsg = computed(() =>
  event.value ? highlight(event.value.message) : '',
)

const highlightedRaw = computed(() =>
  event.value?.raw_message ? highlight(event.value.raw_message) : '',
)

const copyText = computed(() => {
  if (!event.value) return ''
  const e = event.value
  const lines = [
    `severity: ${e.severity_label} (${e.severity})`,
    `message: ${e.message}`,
    `received: ${formatDateTime(e.received_at)}`,
    `reported: ${formatDateTime(e.reported_at)}`,
    `hostname: ${e.hostname || '-'}`,
    `ip: ${e.fromhost_ip || '-'}`,
    `program: ${e.programname || '-'}`,
    `tag: ${e.syslogtag || '-'}`,
    `msgid: ${e.msgid || '-'}`,
    `facility: ${e.facility_label} (${e.facility})`,
  ]
  if (e.structured_data) lines.push(`structured data: ${e.structured_data}`)
  if (e.raw_message) lines.push(`raw message: ${e.raw_message}`)
  return lines.join('\n')
})

function onCopy(ev: ClipboardEvent) {
  const sel = window.getSelection()?.toString() ?? ''
  if (!sel.includes('\n')) return // single field: use browser default
  ev.preventDefault()
  ev.clipboardData?.setData('text/plain', copyText.value)
}

let fetchVersion = 0

watch(() => props.id, async (id) => {
  const version = ++fetchVersion
  event.value = null
  juniperRefs.value = []
  loading.value = true
  error.value = null
  errorStatus.value = null

  const numId = Number(id)
  if (!Number.isInteger(numId) || numId <= 0) {
    errorStatus.value = 404
    error.value = `netlog #${id} does not exist`
    loading.value = false
    return
  }
  try {
    const res = await api.getNetlog(numId)
    if (version !== fetchVersion) return
    event.value = res.data.event
    if (res.data.juniper_ref) {
      juniperRefs.value = res.data.juniper_ref
    }
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
        title="netlog not found"
        :message="`netlog #${props.id} does not exist`"
        :show-back="false"
        list-route="netlog"
        list-label="go to netlog"
      />
      <ErrorDisplay
        v-else-if="error && errorStatus"
        :code="errorStatus"
        title="failed to load netlog"
        :message="error"
        :show-back="false"
        list-route="netlog"
        list-label="go to netlog"
      />
      <ErrorDisplay
        v-else-if="error"
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="false"
        list-route="netlog"
        list-label="go to netlog"
      />

      <div v-else-if="event" class="mx-auto max-w-7xl space-y-4" @copy="onCopy">
        <!-- Header: severity + message -->
        <div
          class="bg-t-bg-dark rounded border-l-2 p-4"
          :class="borderClass"
        >
          <div class="mb-2">
            <span class="text-xs font-semibold uppercase" :class="sevClass">
              {{ event.severity_label }}
            </span>
          </div>
          <p class="text-t-fg break-all font-mono text-sm leading-relaxed" v-html="highlightedMsg" />
        </div>

        <!-- Metadata grid -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Details
          </h3>
          <div class="divide-t-border divide-y text-sm">
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">received</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.received_at) }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">reported</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.reported_at) }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">hostname</span>
              <RouterLink
                :to="{ name: 'netlog-device-detail', params: { hostname: event.hostname } }"
                class="text-t-teal font-mono hover:underline"
              >
                {{ event.hostname || '-' }} <span class="text-t-fg-dark text-xs">&rarr;</span>
              </RouterLink>
            </div>
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">ip</span>
              <span class="text-t-blue font-mono">{{ event.fromhost_ip || '-' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">program</span>
              <span class="text-t-purple font-mono">{{ event.programname || '-' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">msgid</span>
              <span class="text-t-fg font-mono">{{ event.msgid || '-' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">severity</span>
              <span class="font-mono" :class="sevClass">{{ event.severity_label }} ({{ event.severity }})</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">facility</span>
              <span class="text-t-orange font-mono">{{ event.facility_label }} ({{ event.facility }})</span>
            </div>
          </div>
        </div>

        <!-- Structured data -->
        <div v-if="event.structured_data" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Structured Data
          </h3>
          <pre class="text-t-fg overflow-x-auto p-4 font-mono text-xs leading-relaxed">{{ event.structured_data }}</pre>
        </div>

        <!-- Juniper reference -->
        <div v-if="juniperRefs.length > 0" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Juniper Reference
          </h3>
          <div v-for="ref in juniperRefs" :key="ref.id" class="border-t-border border-b p-4 last:border-b-0">
            <div class="mb-2 flex items-center gap-2">
              <span class="text-t-fg font-mono text-sm font-semibold">{{ ref.name }}</span>
              <span class="bg-t-green/15 text-t-green rounded px-1.5 py-0.5 text-xs">{{ ref.os }}</span>
              <span v-if="ref.severity" class="text-t-fg-dark text-xs">{{ ref.severity }}</span>
              <span v-if="ref.type" class="text-t-fg-dark text-xs">({{ ref.type }})</span>
            </div>
            <dl class="space-y-2 text-sm">
              <div v-if="ref.description">
                <dt class="text-t-fg-dark text-xs font-semibold uppercase">Description</dt>
                <dd class="text-t-fg mt-0.5">{{ ref.description }}</dd>
              </div>
              <div v-if="ref.cause">
                <dt class="text-t-fg-dark text-xs font-semibold uppercase">Cause</dt>
                <dd class="text-t-fg mt-0.5">{{ ref.cause }}</dd>
              </div>
              <div v-if="ref.action">
                <dt class="text-t-fg-dark text-xs font-semibold uppercase">Action</dt>
                <dd class="text-t-fg mt-0.5">{{ ref.action }}</dd>
              </div>
              <div v-if="ref.message">
                <dt class="text-t-fg-dark text-xs font-semibold uppercase">Message</dt>
                <dd class="text-t-fg mt-0.5 font-mono text-xs">{{ ref.message }}</dd>
              </div>
            </dl>
          </div>
        </div>

        <!-- Raw message -->
        <div v-if="event.raw_message" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Raw Message
          </h3>
          <pre class="text-t-fg overflow-x-auto p-4 font-mono text-xs leading-relaxed" v-html="highlightedRaw"></pre>
        </div>
      </div>
    </div>
  </div>
</template>
