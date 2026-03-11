<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { api, ApiError } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { AnalysisReportSummary, AnalysisReport } from '@/types/analysis'

const auth = useAuthStore()

const reports = ref<AnalysisReportSummary[]>([])
const selectedId = ref(0)
const currentReport = ref<AnalysisReport | null>(null)

const listLoading = ref(false)
const reportLoading = ref(false)
const triggerLoading = ref(false)

const listError = ref('')
const reportError = ref('')
const triggerError = ref('')

const selectedSummary = computed(() =>
  reports.value.find((r) => r.id === selectedId.value),
)

const renderedMarkdown = computed(() => {
  if (!currentReport.value) return ''
  const html = marked.parse(currentReport.value.report) as string
  return DOMPurify.sanitize(html)
})

async function fetchReportList() {
  listLoading.value = true
  listError.value = ''
  try {
    const res = await api.listAnalysisReports()
    reports.value = res.data
    const first = reports.value[0]
    if (first && !reports.value.some((r) => r.id === selectedId.value)) {
      selectedId.value = first.id
    }
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      reports.value = []
    } else {
      listError.value = e instanceof Error ? e.message : 'Failed to load reports'
    }
  } finally {
    listLoading.value = false
  }
}

async function fetchReport(id: number) {
  reportLoading.value = true
  reportError.value = ''
  currentReport.value = null
  try {
    const res = await api.getAnalysisReport(id)
    currentReport.value = res.data
  } catch (e) {
    reportError.value = e instanceof Error ? e.message : 'Failed to load report'
  } finally {
    reportLoading.value = false
  }
}

async function triggerAnalysis() {
  triggerLoading.value = true
  triggerError.value = ''
  try {
    const signal = AbortSignal.timeout(16 * 60 * 1000)
    const res = await api.triggerAnalysis(signal)
    await fetchReportList()
    selectedId.value = res.data.report_id
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.code === 'not_configured') {
        triggerError.value = 'Analysis is not configured on this server'
      } else if (e.code === 'analysis_failed') {
        triggerError.value = `Analysis failed: ${e.message}`
      } else {
        triggerError.value = e.message
      }
    } else if (e instanceof DOMException && e.name === 'TimeoutError') {
      triggerError.value = 'Analysis timed out — the report may still be generating'
    } else {
      triggerError.value = e instanceof Error ? e.message : 'Failed to trigger analysis'
    }
  } finally {
    triggerLoading.value = false
  }
}

watch(selectedId, (id) => {
  if (id > 0) fetchReport(id)
})

onMounted(() => {
  fetchReportList()
})

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatDuration(ms: number): string {
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

function formatPeriod(start: string, end: string): string {
  const s = new Date(start)
  const e = new Date(end)
  const fmt = (d: Date) =>
    d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
  return `${fmt(s)} — ${fmt(e)}`
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-7xl space-y-5">

        <!-- Loading state -->
        <div v-if="listLoading" class="text-t-fg-dark flex items-center gap-2 py-20 text-sm justify-center">
          <svg class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" />
          </svg>
          <span>Loading reports...</span>
        </div>

        <!-- List error -->
        <div v-else-if="listError" class="rounded border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-400">
          {{ listError }}
        </div>

        <!-- Content -->
        <template v-else>
          <!-- Page header -->
          <div class="flex items-center justify-between gap-4">
            <div>
              <h2 class="text-t-fg text-base font-semibold">Analysis</h2>
              <p class="text-t-fg-dark mt-1 text-sm">AI-generated syslog analysis reports</p>
            </div>

            <div class="flex items-center gap-3">
              <!-- Trigger button -->
              <button
                v-if="auth.user?.username !== 'anonymous'"
                :disabled="triggerLoading"
                class="bg-t-orange/15 text-t-orange hover:bg-t-orange/25 disabled:opacity-50 rounded px-3 py-1.5 text-xs font-medium transition-colors"
                @click="triggerAnalysis"
              >
                <span v-if="triggerLoading" class="flex items-center gap-1.5">
                  <svg class="h-3 w-3 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" />
                  </svg>
                  Analyzing...
                </span>
                <span v-else>Trigger Analysis</span>
              </button>

              <!-- Report selector -->
              <select
                v-if="reports.length > 0"
                v-model="selectedId"
                class="bg-t-bg-dark border-t-border text-t-fg rounded border px-3 py-1.5 text-xs"
              >
                <option
                  v-for="r in reports"
                  :key="r.id"
                  :value="r.id"
                >
                  {{ formatPeriod(r.period_start, r.period_end) }} — {{ r.model }}
                </option>
              </select>
            </div>
          </div>

          <!-- Trigger error banner -->
          <div v-if="triggerError" class="rounded border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {{ triggerError }}
          </div>

          <!-- Metadata bar (from selectedSummary — instant on selection change) -->
          <div
            v-if="selectedSummary"
            class="bg-t-bg-dark border-t-border flex flex-wrap items-center gap-x-6 gap-y-2 rounded border px-4 py-3"
          >
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Status</span>
              <span
                class="rounded px-1.5 py-0.5 text-xs font-medium"
                :class="
                  selectedSummary.status === 'completed'
                    ? 'bg-t-green/15 text-t-green'
                    : selectedSummary.status === 'running'
                      ? 'bg-t-yellow/15 text-t-yellow'
                      : 'bg-t-red/15 text-t-red'
                "
              >
                {{ selectedSummary.status }}
              </span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Model</span>
              <span class="text-t-fg text-xs font-medium">{{ selectedSummary.model }}</span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Period</span>
              <span class="text-t-fg text-xs font-medium">{{ formatPeriod(selectedSummary.period_start, selectedSummary.period_end) }}</span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Tokens</span>
              <span class="text-t-fg text-xs font-medium">{{ selectedSummary.prompt_tokens.toLocaleString() }} + {{ selectedSummary.completion_tokens.toLocaleString() }}</span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Duration</span>
              <span class="text-t-fg text-xs font-medium">{{ formatDuration(selectedSummary.duration_ms) }}</span>
            </div>
            <div class="flex items-center gap-1.5">
              <span class="text-t-fg-dark text-xs">Generated</span>
              <span class="text-t-fg text-xs font-medium">{{ formatDate(selectedSummary.generated_at) }}</span>
            </div>
          </div>

          <!-- Report loading -->
          <div v-if="reportLoading" class="text-t-fg-dark flex items-center gap-2 py-10 text-sm justify-center">
            <svg class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" />
            </svg>
            <span>Loading report...</span>
          </div>

          <!-- Report error -->
          <div v-else-if="reportError" class="rounded border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {{ reportError }}
          </div>

          <!-- Report body -->
          <div
            v-else-if="currentReport"
            class="prose border-t-border rounded border px-6 py-5"
            v-html="renderedMarkdown"
          />

          <!-- Empty state -->
          <div
            v-if="!listLoading && reports.length === 0"
            class="text-t-fg-dark flex flex-col items-center gap-3 py-20 text-sm"
          >
            <svg class="h-8 w-8 opacity-40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
              <polyline points="14 2 14 8 20 8" />
              <line x1="16" y1="13" x2="8" y2="13" />
              <line x1="16" y1="17" x2="8" y2="17" />
              <polyline points="10 9 9 9 8 9" />
            </svg>
            <span class="text-t-fg font-medium">No analysis reports yet</span>
            <p class="max-w-sm text-center text-xs leading-relaxed">
              To enable AI-powered syslog analysis, configure the
              <code class="text-t-teal bg-t-bg-highlight rounded px-1 py-0.5 text-[0.6875rem] border border-t-border">analysis</code>
              section in your <code class="text-t-teal bg-t-bg-highlight rounded px-1 py-0.5 text-[0.6875rem] border border-t-border">config.yml</code>
              with an Ollama endpoint and model, then restart Taillight.
            </p>
          </div>

        </template>

      </div>
    </div>
  </div>
</template>

<style scoped>
/* Theme-aware prose styling for rendered markdown */
.prose {
  color: var(--color-t-fg);
  line-height: 1.75;
}

/* ── Headings ── */
.prose :deep(h1) {
  color: var(--color-t-fg);
  font-size: 1.25rem;
  font-weight: 700;
  margin-top: 0;
  margin-bottom: 1rem;
  padding-bottom: 0.625rem;
  border-bottom: 2px solid var(--color-t-orange);
}

.prose :deep(h2) {
  color: var(--color-t-orange);
  font-size: 1rem;
  font-weight: 600;
  margin-top: 2rem;
  margin-bottom: 0.625rem;
  padding-bottom: 0.375rem;
  border-bottom: 1px solid var(--color-t-border);
}

.prose :deep(h3) {
  color: var(--color-t-teal);
  font-size: 0.875rem;
  font-weight: 600;
  margin-top: 1.5rem;
  margin-bottom: 0.375rem;
}

/* ── Body text ── */
.prose :deep(p) {
  margin-top: 0.5rem;
  margin-bottom: 0.5rem;
  font-size: 0.8125rem;
}

.prose :deep(ul),
.prose :deep(ol) {
  margin-top: 0.375rem;
  margin-bottom: 0.375rem;
  padding-left: 1.5rem;
  font-size: 0.8125rem;
}

.prose :deep(li) {
  margin-top: 0.1875rem;
  margin-bottom: 0.1875rem;
}

.prose :deep(li::marker) {
  color: var(--color-t-fg-dark);
}

/* ── Inline emphasis ── */
.prose :deep(strong) {
  color: var(--color-t-fg);
  font-weight: 600;
}

.prose :deep(em) {
  color: var(--color-t-fg-dark);
  font-style: italic;
}

/* ── Inline code ── */
.prose :deep(code) {
  color: var(--color-t-teal);
  background: var(--color-t-bg-highlight);
  padding: 0.125rem 0.375rem;
  border-radius: 0.25rem;
  font-size: 0.75rem;
  border: 1px solid var(--color-t-border);
}

/* ── Code blocks ── */
.prose :deep(pre) {
  background: var(--color-t-bg-dark);
  border: 1px solid var(--color-t-border);
  border-radius: 0.375rem;
  padding: 0.875rem 1rem;
  overflow-x: auto;
  margin-top: 0.75rem;
  margin-bottom: 0.75rem;
}

.prose :deep(pre code) {
  background: none;
  padding: 0;
  border: none;
  color: var(--color-t-fg);
}

/* ── Horizontal rules ── */
.prose :deep(hr) {
  border: none;
  border-top: 1px solid var(--color-t-border);
  margin: 1.5rem 0;
}

/* ── Tables ── */
.prose :deep(table) {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.75rem;
  margin-top: 0.75rem;
  margin-bottom: 0.75rem;
  border: 1px solid var(--color-t-border);
  border-radius: 0.375rem;
  overflow: hidden;
}

.prose :deep(th) {
  color: var(--color-t-orange);
  font-weight: 600;
  text-align: left;
  padding: 0.5rem 0.75rem;
  background: var(--color-t-bg-dark);
  border-bottom: 1px solid var(--color-t-border);
}

.prose :deep(td) {
  padding: 0.375rem 0.75rem;
  border-bottom: 1px solid var(--color-t-border);
}

.prose :deep(tr:last-child td) {
  border-bottom: none;
}

.prose :deep(tr:hover td) {
  background: var(--color-t-bg-highlight);
}

/* ── Links ── */
.prose :deep(a) {
  color: var(--color-t-blue);
  text-decoration: underline;
  text-underline-offset: 2px;
}

.prose :deep(a:hover) {
  color: var(--color-t-teal);
}

/* ── Blockquotes ── */
.prose :deep(blockquote) {
  border-left: 3px solid var(--color-t-orange);
  padding-left: 1rem;
  color: var(--color-t-fg-dark);
  margin: 0.75rem 0;
  font-size: 0.8125rem;
}
</style>
