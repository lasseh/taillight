<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import type { SyslogEvent, JuniperSyslogRef } from '@/types/syslog'
import { api, ApiError } from '@/lib/api'
import { severityColorClass, severityBorderClass } from '@/lib/constants'
import { highlight } from '@/lib/highlighter'
import { formatDateTime } from '@/lib/format'
import ErrorDisplay from '@/components/ErrorDisplay.vue'

const props = defineProps<{
  id: string
}>()

const router = useRouter()
const event = ref<SyslogEvent | null>(null)
const juniperRefs = ref<JuniperSyslogRef[]>([])
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

onMounted(async () => {
  const numId = Number(props.id)
  if (!Number.isInteger(numId) || numId <= 0) {
    errorStatus.value = 404
    error.value = `syslog #${props.id} does not exist`
    loading.value = false
    return
  }
  try {
    const res = await api.getSyslog(numId)
    event.value = res.data
    if (res.data.juniper_ref) {
      juniperRefs.value = res.data.juniper_ref
    }
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
        title="syslog not found"
        :message="`syslog #${props.id} does not exist`"
        :show-back="false"
        list-route="syslog"
        list-label="go to syslog"
      />
      <ErrorDisplay
        v-else-if="error && errorStatus"
        :code="errorStatus"
        title="failed to load syslog"
        :message="error"
        :show-back="false"
        list-route="syslog"
        list-label="go to syslog"
      />
      <ErrorDisplay
        v-else-if="error"
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="false"
        list-route="syslog"
        list-label="go to syslog"
      />

      <div v-else-if="event" class="mx-auto max-w-4xl space-y-4">
        <!-- Header: severity + message -->
        <div
          class="bg-t-bg-dark rounded border-l-2 p-4"
          :class="borderClass"
        >
          <div class="mb-2 flex items-center gap-2">
            <span class="text-xs font-semibold uppercase" :class="sevClass">
              {{ event.severity_label }}
            </span>
            <span class="text-t-fg-dark text-xs">#{{ event.id }}</span>
          </div>
          <p class="text-t-fg break-all font-mono text-sm leading-relaxed" v-html="highlightedMsg" />
        </div>

        <!-- Metadata grid -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Details
          </h3>
          <dl class="grid grid-cols-[auto_1fr] text-sm">
            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">received</dt>
            <dd class="text-t-fg border-t-border border-b px-4 py-1.5 font-mono">{{ formatDateTime(event.received_at) }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">reported</dt>
            <dd class="text-t-fg border-t-border border-b px-4 py-1.5 font-mono">{{ formatDateTime(event.reported_at) }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">hostname</dt>
            <dd class="text-t-teal border-t-border border-b px-4 py-1.5 font-mono">
              <RouterLink
                :to="{ name: 'device-detail', params: { hostname: event.hostname } }"
                class="hover:underline"
              >
                {{ event.hostname || '–' }}
              </RouterLink>
            </dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">ip</dt>
            <dd class="text-t-blue border-t-border border-b px-4 py-1.5 font-mono">{{ event.fromhost_ip || '–' }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">program</dt>
            <dd class="text-t-purple border-t-border border-b px-4 py-1.5 font-mono">{{ event.programname || '–' }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">tag</dt>
            <dd class="text-t-fg border-t-border border-b px-4 py-1.5 font-mono">{{ event.syslogtag || '–' }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">msgid</dt>
            <dd class="text-t-fg border-t-border border-b px-4 py-1.5 font-mono">{{ event.msgid || '–' }}</dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">severity</dt>
            <dd class="border-t-border border-b px-4 py-1.5 font-mono" :class="sevClass">
              {{ event.severity_label }} ({{ event.severity }})
            </dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">facility</dt>
            <dd class="text-t-orange border-t-border border-b px-4 py-1.5 font-mono">
              {{ event.facility_label }} ({{ event.facility }})
            </dd>

          </dl>
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
