<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { api, ApiError } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import { usePolling } from '@/composables/usePolling'
import {
  briefingTitle,
  feedBadgeClass,
  formatDuration,
  formatPeriodRange,
  formatPeriodUTC,
  promptModeBadgeClass,
  statusBadgeClass,
} from '@/lib/analysis-format'
import type { AnalysisReport, AnalysisReportResponse } from '@/types/analysis'

const props = defineProps<{ slug: string }>()

const router = useRouter()
const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.is_admin === true)

const report = ref<AnalysisReport | null>(null)
const loading = ref(true)
const loadError = ref('')

const confirmDelete = ref(false)
const deleting = ref(false)
const deleteError = ref('')

// Pin the allowed tag/attr set to what the report template actually emits, so a
// future marked extension or model output can't widen the attack surface.
const MARKDOWN_SANITIZE = {
  ALLOWED_TAGS: [
    'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
    'p', 'br', 'hr',
    'ul', 'ol', 'li',
    'strong', 'em', 'del', 's', 'code', 'pre',
    'blockquote', 'a',
    'table', 'thead', 'tbody', 'tr', 'th', 'td',
  ],
  ALLOWED_ATTR: ['href', 'title', 'align'],
}

const renderedMarkdown = computed(() => {
  if (!report.value?.report) return ''
  const html = marked.parse(report.value.report) as string
  return DOMPurify.sanitize(html, MARKDOWN_SANITIZE)
})

const polling = usePolling<AnalysisReportResponse>(
  () => api.getAnalysisReport(props.slug),
  (res) => res.data.status === 'pending' || res.data.status === 'running',
  3000,
)

watch(
  () => polling.data.value,
  (res) => {
    if (res) report.value = res.data
  },
)

// polling.start() does the initial fetch; we then read polling.error to surface
// any failure, so we don't double-fetch on mount.
async function refresh() {
  try {
    await polling.start()
    const e = polling.error.value
    if (e instanceof ApiError && e.status === 404) {
      loadError.value = 'report not found'
    } else if (e) {
      loadError.value = e instanceof Error ? e.message : 'failed to load report'
    } else {
      loadError.value = ''
    }
  } finally {
    loading.value = false
  }
}

async function deleteReport() {
  deleting.value = true
  deleteError.value = ''
  try {
    await api.deleteAnalysisReport(props.slug)
    router.push({ name: 'analysis' })
  } catch (e) {
    deleteError.value = e instanceof ApiError ? e.message : 'failed to delete report'
  } finally {
    deleting.value = false
    confirmDelete.value = false
  }
}

function exportPDF() {
  window.print()
}

// Classic ops-brief header:
//   Daily Operations Briefing — 2026-05-20 → 2026-05-21
//   Period: 2026-05-20 19:19 UTC – 2026-05-21 19:19 UTC
// Mode label comes from briefingTitle (daily/weekly/incident); the date
// span and full UTC window are rendered from period_start/period_end so
// the heading reflects the syslog window, not the report's creation time.
function reportTitle(r: AnalysisReport): string {
  return `${briefingTitle(r.prompt_mode)} — ${formatPeriodRange(r.period_start, r.period_end)}`
}

function reportPeriodLine(r: AnalysisReport): string {
  return `Period: ${formatPeriodUTC(r.period_start, r.period_end)}`
}

// Detail page wants — for missing values rather than the empty string the
// shared helper returns (used by the list table).
function durationOrDash(r: AnalysisReport): string {
  return formatDuration(r) || '—'
}

onMounted(refresh)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-7xl space-y-5">

        <div v-if="loading" class="text-t-fg-dark py-20 text-center text-sm">loading...</div>

        <div v-else-if="loadError" class="space-y-3">
          <router-link :to="{ name: 'analysis' }" class="text-t-fg-dark hover:text-t-fg text-sm">← back to analysis</router-link>
          <div class="rounded border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {{ loadError }}
          </div>
        </div>

        <template v-else-if="report">
          <div class="print-hide flex items-center justify-between gap-4">
            <div class="flex items-center gap-3">
              <router-link :to="{ name: 'analysis' }" class="text-t-fg-dark hover:text-t-fg text-sm">← back</router-link>
            </div>
            <div class="flex items-center gap-3">
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-3 py-1.5 text-xs transition-all"
                @click="exportPDF"
              >
                Export PDF
              </button>
              <template v-if="isAdmin">
                <button
                  v-if="!confirmDelete"
                  class="text-t-red/70 hover:text-t-red text-xs transition-colors"
                  @click="confirmDelete = true"
                >
                  delete
                </button>
                <span v-else class="flex items-center gap-2">
                  <button
                    class="text-t-red hover:brightness-125 text-xs font-semibold"
                    :disabled="deleting"
                    @click="deleteReport"
                  >
                    {{ deleting ? 'deleting...' : 'yes' }}
                  </button>
                  <button class="text-t-fg-dark hover:text-t-fg text-xs" @click="confirmDelete = false">no</button>
                </span>
              </template>
            </div>
          </div>

          <div class="space-y-1">
            <h1 class="text-t-fg text-xl font-semibold">{{ reportTitle(report) }}</h1>
            <div class="text-t-fg-dark text-sm">{{ reportPeriodLine(report) }}</div>
            <div class="text-t-fg-dark font-mono text-xs">{{ report.slug }}</div>
          </div>

          <!-- Period moved into the heading above; the chip row keeps Mode
               for the at-a-glance colour cue. Status, source, model, and
               duration stay because each answers a question the heading
               doesn't. -->
          <div
            class="bg-t-bg-dark border-t-border flex flex-wrap items-center gap-x-6 gap-y-2 rounded border px-4 py-3"
          >
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Status</span>
              <span class="rounded px-1.5 py-0.5 text-xs font-medium" :class="statusBadgeClass(report.status)">
                {{ report.status }}
              </span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Source</span>
              <span class="rounded px-1.5 py-0.5 text-xs font-medium" :class="feedBadgeClass(report.feed)">
                {{ report.feed }}
              </span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Mode</span>
              <span
                class="rounded px-1.5 py-0.5 text-xs font-medium"
                :class="promptModeBadgeClass(report.prompt_mode)"
              >
                {{ report.prompt_mode }}
              </span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Model</span>
              <span class="text-t-fg text-xs font-medium">{{ report.model || '—' }}</span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Duration</span>
              <span class="text-t-fg text-xs font-medium">{{ durationOrDash(report) }}</span>
            </div>
          </div>

          <div v-if="deleteError" class="rounded border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {{ deleteError }}
          </div>

          <div
            v-if="report.status === 'pending' || report.status === 'running'"
            class="text-t-fg-dark flex items-center gap-3 py-10 text-sm justify-center"
          >
            <svg class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" />
            </svg>
            <span>report is being generated — this page will update automatically</span>
          </div>

          <div
            v-else-if="report.status === 'failed'"
            class="rounded border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <div class="font-semibold mb-1">analysis failed</div>
            <div class="font-mono text-xs">{{ report.error || 'no error message' }}</div>
          </div>

          <div
            v-else-if="report.status === 'completed' && report.report"
            class="prose border-t-border rounded border px-6 py-5"
            v-html="renderedMarkdown"
          />
        </template>

      </div>
    </div>
  </div>
</template>

<style scoped>
.prose {
  color: var(--color-t-fg);
  line-height: 1.75;
}
.prose :deep(h1) { color: var(--color-t-fg); font-size: 1.25rem; font-weight: 700; margin-top: 0; margin-bottom: 1rem; padding-bottom: 0.625rem; border-bottom: 2px solid var(--color-t-orange); }
.prose :deep(h2) { color: var(--color-t-orange); font-size: 1rem; font-weight: 600; margin-top: 2rem; margin-bottom: 0.625rem; padding-bottom: 0.375rem; border-bottom: 1px solid var(--color-t-border); }
.prose :deep(h3) { color: var(--color-t-teal); font-size: 0.875rem; font-weight: 600; margin-top: 1.5rem; margin-bottom: 0.375rem; }
.prose :deep(p) { margin-top: 0.5rem; margin-bottom: 0.5rem; font-size: 0.8125rem; }
.prose :deep(ul), .prose :deep(ol) { margin-top: 0.375rem; margin-bottom: 0.375rem; padding-left: 1.5rem; font-size: 0.8125rem; }
.prose :deep(li) { margin-top: 0.1875rem; margin-bottom: 0.1875rem; }
.prose :deep(li::marker) { color: var(--color-t-fg-dark); }
.prose :deep(strong) { color: var(--color-t-fg); font-weight: 600; }
.prose :deep(em) { color: var(--color-t-fg-dark); font-style: italic; }
.prose :deep(code) { color: var(--color-t-teal); background: var(--color-t-bg-highlight); padding: 0.125rem 0.375rem; border-radius: 0.25rem; font-size: 0.75rem; border: 1px solid var(--color-t-border); }
.prose :deep(pre) { background: var(--color-t-bg-dark); border: 1px solid var(--color-t-border); border-radius: 0.375rem; padding: 0.875rem 1rem; overflow-x: auto; margin-top: 0.75rem; margin-bottom: 0.75rem; }
.prose :deep(pre code) { background: none; padding: 0; border: none; color: var(--color-t-fg); }
.prose :deep(hr) { border: none; border-top: 1px solid var(--color-t-border); margin: 1.5rem 0; }
.prose :deep(table) { width: 100%; border-collapse: collapse; font-size: 0.75rem; margin-top: 0.75rem; margin-bottom: 0.75rem; border: 1px solid var(--color-t-border); border-radius: 0.375rem; overflow: hidden; }
.prose :deep(th) { color: var(--color-t-orange); font-weight: 600; text-align: left; padding: 0.5rem 0.75rem; background: var(--color-t-bg-dark); border-bottom: 1px solid var(--color-t-border); }
.prose :deep(td) { padding: 0.375rem 0.75rem; border-bottom: 1px solid var(--color-t-border); }
.prose :deep(tr:last-child td) { border-bottom: none; }
.prose :deep(tr:hover td) { background: var(--color-t-bg-highlight); }
.prose :deep(a) { color: var(--color-t-blue); text-decoration: underline; text-underline-offset: 2px; }
.prose :deep(a:hover) { color: var(--color-t-teal); }
.prose :deep(blockquote) { border-left: 3px solid var(--color-t-orange); padding-left: 1rem; color: var(--color-t-fg-dark); margin: 0.75rem 0; font-size: 0.8125rem; }
</style>

<style>
/* Global print styles — when the user prints from the report detail page,
 * hide the app shell (header, banners, status footer) and let the report
 * body span the full page. Vue mounts at <div id="app">, App.vue renders
 * one wrapping <div>, and that div holds <header>, <main>, and a status
 * <div>. We hide every sibling of <main> at that level. .print-hide inside
 * the view itself hides per-page controls. */
@media print {
  body {
    background: white !important;
    color: black !important;
  }
  body > div > div > *:not(main) {
    display: none !important;
  }
  .print-hide {
    display: none !important;
  }
}
</style>
