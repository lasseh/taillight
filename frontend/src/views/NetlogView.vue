<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { NetlogEvent, JuniperNetlogRef } from '@/types/netlog'
import type { NetboxLookup } from '@/types/netbox'
import { api, ApiError } from '@/lib/api'
import { severityColorClass, severityBorderClass } from '@/lib/constants'
import { highlight } from '@/lib/highlighter'
import { formatDateTime } from '@/lib/format'
import { selectedRowsText } from '@/lib/copy'
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import NetboxEnrichmentPanel from '@/components/NetboxEnrichmentPanel.vue'

const props = defineProps<{
  id: string
}>()

const router = useRouter()
const event = ref<NetlogEvent | null>(null)
const juniperRefs = ref<JuniperNetlogRef[]>([])
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

// Netbox enrichment is fetched lazily after the event loads. When the
// endpoint is disabled (404) or Netbox is unconfigured (503), the panel is
// hidden silently. Per-entity errors surface inside the panel.
const netboxLookups = ref<NetboxLookup[]>([])
const netboxLoading = ref(false)
const netboxAvailable = ref(true)

// Structured data + raw message are noisy and rarely needed at a glance —
// keep them collapsed behind a single toggle at the bottom of the page.
const showMore = ref(false)
const hasMore = computed(() =>
  Boolean(event.value?.structured_data || event.value?.raw_message),
)

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
  juniperRefs.value = []
  netboxLookups.value = []
  netboxLoading.value = false
  netboxAvailable.value = true
  showMore.value = false
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
    fetchNetbox(numId, version)
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

async function fetchNetbox(id: number, version: number) {
  netboxLoading.value = true
  try {
    const res = await api.getNetlogNetbox(id)
    if (version !== fetchVersion) return
    netboxLookups.value = res.data.lookups ?? []
  } catch (e) {
    if (version !== fetchVersion) return
    // 404 (route not registered) and 503 (disabled at runtime) both mean
    // "no netbox here" — hide the panel silently.
    if (e instanceof ApiError && (e.status === 404 || e.status === 503)) {
      netboxAvailable.value = false
      return
    }
    // Other errors: hide the panel rather than surface a noisy banner — the
    // detail page itself is still useful without enrichment.
    netboxAvailable.value = false
  } finally {
    if (version === fetchVersion) {
      netboxLoading.value = false
    }
  }
}
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
          <div class="mb-2" :data-copytext="`severity: ${event.severity_label} (${event.severity})`">
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
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Details
          </h3>
          <div class="divide-t-border divide-y text-sm">
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`received: ${formatDateTime(event.received_at)}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">received</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.received_at) }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`reported: ${formatDateTime(event.reported_at)}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">reported</span>
              <span class="text-t-fg font-mono">{{ formatDateTime(event.reported_at) }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`hostname: ${event.hostname || '-'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">hostname</span>
              <RouterLink
                :to="{ name: 'netlog-device-detail', params: { hostname: event.hostname } }"
                class="text-t-teal font-mono hover:underline"
              >
                {{ event.hostname || '-' }} <span class="text-t-fg-dark text-xs">&rarr;</span>
              </RouterLink>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`ip: ${event.fromhost_ip || '-'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">ip</span>
              <span class="text-t-blue font-mono">{{ event.fromhost_ip || '-' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`program: ${event.programname || '-'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">program</span>
              <span class="text-t-purple font-mono">{{ event.programname || '-' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`msgid: ${event.msgid || '-'}`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">msgid</span>
              <span class="text-t-fg font-mono">{{ event.msgid || '-' }}</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`severity: ${event.severity_label} (${event.severity})`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">severity</span>
              <span class="font-mono" :class="sevClass">{{ event.severity_label }} ({{ event.severity }})</span>
            </div>
            <div class="flex gap-2 px-4 py-1.5" :data-copytext="`facility: ${event.facility_label} (${event.facility})`">
              <span class="text-t-fg-dark w-24 shrink-0 text-right">facility</span>
              <span class="text-t-orange font-mono">{{ event.facility_label }} ({{ event.facility }})</span>
            </div>
          </div>
        </div>

        <!-- Netbox enrichment -->
        <NetboxEnrichmentPanel
          v-if="netboxAvailable && (netboxLoading || netboxLookups.length > 0)"
          :loading="netboxLoading"
          :lookups="netboxLookups"
        />

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

        <!-- Show more (raw message + structured data, collapsed by default) -->
        <template v-if="hasMore">
          <button
            type="button"
            class="text-t-fg-dark hover:text-t-fg flex items-center gap-1 text-xs transition-colors"
            :aria-expanded="showMore"
            @click="showMore = !showMore"
          >
            <span>{{ showMore ? '−' : '+' }}</span>
            <span>{{ showMore ? 'hide raw' : 'show raw' }}</span>
          </button>

          <div v-if="showMore && event.structured_data" class="bg-t-bg-dark border-t-border rounded border">
            <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
              Structured Data
            </h3>
            <pre
              class="text-t-fg overflow-x-auto p-4 font-mono text-xs leading-relaxed"
              :data-copytext="`structured data: ${event.structured_data}`"
            >{{ event.structured_data }}</pre>
          </div>

          <div v-if="showMore && event.raw_message" class="bg-t-bg-dark border-t-border rounded border">
            <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
              Raw Message
            </h3>
            <pre
              class="text-t-fg overflow-x-auto p-4 font-mono text-xs leading-relaxed"
              :data-copytext="`raw message: ${event.raw_message}`"
              v-html="highlightedRaw"
            ></pre>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
