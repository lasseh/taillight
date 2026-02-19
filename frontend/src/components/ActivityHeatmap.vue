<script setup lang="ts">
import { computed, ref } from 'vue'

const props = withDefaults(defineProps<{
  /** Map of "YYYY-MM-DD HH:mm" to count (30-min buckets, e.g. "2026-02-19 14:00") */
  data: Record<string, number>
  /** CSS color variable name for the heatmap accent */
  colorVar?: string
  /** Label shown in tooltip */
  label?: string
}>(), {
  colorVar: '--color-t-teal',
  label: 'events',
})

// ── Rolling heatmap: 7 columns (days) × 48 rows (30-min slots) = 336 cells ──
// Time flows top→bottom within each column, then left→right to the next day.
// Last cell (col 6, row 47) = current 30-min slot ("now")

const DAY_COLS = 7
const TIME_ROWS = 48
const TOTAL_SLOTS = DAY_COLS * TIME_ROWS // 336
const SLOT_MS = 30 * 60 * 1000
const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

interface Cell {
  key: string        // "YYYY-MM-DD HH:mm"
  count: number
  level: number      // 0-4
  row: number        // 0-47 (time slot within day)
  col: number        // 0-6  (day)
  tipText: string
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

/** Floor a Date to the previous 30-min boundary. */
function floor30(d: Date): Date {
  const out = new Date(d)
  out.setMinutes(Math.floor(out.getMinutes() / 30) * 30, 0, 0)
  return out
}

const grid = computed(() => {
  const now = floor30(new Date())
  const startTime = new Date(now.getTime() - (TOTAL_SLOTS - 1) * SLOT_MS)

  const cells: Cell[] = []
  const counts: number[] = []

  for (let i = 0; i < TOTAL_SLOTS; i++) {
    const t = new Date(startTime.getTime() + i * SLOT_MS)
    const key = `${t.getFullYear()}-${pad2(t.getMonth() + 1)}-${pad2(t.getDate())} ${pad2(t.getHours())}:${pad2(t.getMinutes())}`
    const count = props.data[key] ?? 0
    counts.push(count)

    const col = Math.floor(i / TIME_ROWS)
    const row = i % TIME_ROWS

    // Tooltip: "Thu 2/13, 15:30"
    const isToday = t.getDate() === now.getDate() && t.getMonth() === now.getMonth() && t.getFullYear() === now.getFullYear()
    const dayLabel = isToday ? 'Today' : DAYS[t.getDay()]
    const tipText = `${dayLabel} ${t.getMonth() + 1}/${t.getDate()}, ${pad2(t.getHours())}:${pad2(t.getMinutes())}`

    cells.push({ key, count, level: 0, row, col, tipText })
  }

  // Percentile-based levels (GitHub style)
  const nonZero = counts.filter(c => c > 0).sort((a, b) => a - b)
  if (nonZero.length > 0) {
    const t = [
      1,
      nonZero[Math.floor(nonZero.length * 0.25)] ?? 1,
      nonZero[Math.floor(nonZero.length * 0.50)] ?? 1,
      nonZero[Math.floor(nonZero.length * 0.75)] ?? 1,
    ]
    for (const cell of cells) {
      if (cell.count === 0) cell.level = 0
      else if (cell.count < t[1]) cell.level = 1
      else if (cell.count < t[2]) cell.level = 2
      else if (cell.count < t[3]) cell.level = 3
      else cell.level = 4
    }
  }

  return cells
})

// ── Time labels (left, Y-axis): hour every 3 hours across 48 rows ──

const timeLabels = computed(() => {
  const now = floor30(new Date())
  const startTime = new Date(now.getTime() - (TOTAL_SLOTS - 1) * SLOT_MS)
  const startH = startTime.getHours()
  const startM = startTime.getMinutes()

  const labels: { label: string; row: number }[] = []
  for (let row = 0; row < TIME_ROWS; row++) {
    const totalMin = (startH * 60 + startM) + row * 30
    const h = Math.floor(totalMin / 60) % 24
    const m = totalMin % 60
    if (h % 3 === 0 && m === 0) {
      labels.push({ label: pad2(h), row })
    }
  }
  return labels
})

// ── Day labels (top, X-axis): day name + day-of-month for each column ──

const dayLabels = computed(() => {
  const now = floor30(new Date())
  const startTime = new Date(now.getTime() - (TOTAL_SLOTS - 1) * SLOT_MS)

  return Array.from({ length: DAY_COLS }, (_, col) => {
    const t = new Date(startTime.getTime() + col * TIME_ROWS * SLOT_MS)
    return `${DAYS[t.getDay()]} ${t.getDate()}`
  })
})

const totalCount = computed(() => grid.value.reduce((sum, c) => sum + c.count, 0))

// ── Tooltip ──

const tooltip = ref<{ x: number; y: number; cell: Cell } | null>(null)

function showTooltip(event: MouseEvent, cell: Cell) {
  const rect = (event.target as HTMLElement).getBoundingClientRect()
  tooltip.value = { x: rect.left + rect.width / 2, y: rect.top - 8, cell }
}

function hideTooltip() {
  tooltip.value = null
}
</script>

<template>
  <div class="heatmap-wrap">
    <!-- Day labels (top, X-axis) -->
    <div class="heatmap-day-labels">
      <span
        v-for="(lbl, i) in dayLabels"
        :key="i"
        class="text-t-fg-dark text-[10px]"
      >{{ lbl }}</span>
    </div>

    <div class="flex gap-[2px]">
      <!-- Time labels (left sidebar, Y-axis) -->
      <div class="heatmap-time-labels">
        <span
          v-for="h in timeLabels"
          :key="h.row"
          class="text-t-fg-dark text-[10px]"
          :style="{ gridRowStart: h.row + 1 }"
        >{{ h.label }}</span>
      </div>

      <!-- Grid -->
      <div class="heatmap-grid">
        <div
          v-for="(cell, i) in grid"
          :key="i"
          class="heatmap-cell"
          :data-level="cell.level"
          :style="{
            gridColumn: cell.col + 1,
            gridRow: cell.row + 1,
            '--heatmap-color': `var(${colorVar})`,
          }"
          @mouseenter="showTooltip($event, cell)"
          @mouseleave="hideTooltip"
        ></div>
      </div>
    </div>

    <!-- Legend -->
    <div class="mt-2 flex items-center justify-between">
      <span class="text-t-fg-dark text-[10px]">{{ totalCount.toLocaleString() }} {{ label }} in 7 days</span>
      <div class="flex items-center gap-1">
        <span class="text-t-fg-dark text-[10px]">Less</span>
        <div class="heatmap-cell legend" data-level="0" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="1" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="2" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="3" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="4" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <span class="text-t-fg-dark text-[10px]">More</span>
      </div>
    </div>

    <!-- Tooltip -->
    <Teleport to="body">
      <div
        v-if="tooltip"
        class="heatmap-tooltip"
        :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }"
      >
        <strong>{{ tooltip.cell.count.toLocaleString() }} {{ label }}</strong>
        <span>{{ tooltip.cell.tipText }}</span>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.heatmap-wrap {
  overflow-x: auto;
  overflow-y: hidden;
}

.heatmap-day-labels {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  margin-left: 30px; /* align with grid after time labels */
  margin-bottom: 2px;
  height: 14px;
}

.heatmap-day-labels span {
  text-align: center;
}

.heatmap-time-labels {
  display: grid;
  grid-template-rows: repeat(48, 1fr);
  gap: 2px;
  width: 26px;
  flex-shrink: 0;
}

.heatmap-time-labels span {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding-right: 4px;
  height: 11px;
}

.heatmap-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  grid-template-rows: repeat(48, 1fr);
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.heatmap-cell {
  aspect-ratio: 1;
  min-height: 0;
  border-radius: 2px;
  transition: outline 0.1s;
}

.heatmap-cell:hover {
  outline: 1px solid var(--color-t-fg-dark);
  outline-offset: -1px;
}

.heatmap-cell.legend {
  width: 11px;
  height: 11px;
  cursor: default;
  flex-shrink: 0;
}

.heatmap-cell.legend:hover {
  outline: none;
}

/* Intensity levels */
.heatmap-cell[data-level="0"] {
  background-color: var(--color-t-bg-highlight);
}

.heatmap-cell[data-level="1"] {
  background-color: color-mix(in srgb, var(--heatmap-color) 25%, var(--color-t-bg-highlight));
}

.heatmap-cell[data-level="2"] {
  background-color: color-mix(in srgb, var(--heatmap-color) 50%, var(--color-t-bg-highlight));
}

.heatmap-cell[data-level="3"] {
  background-color: color-mix(in srgb, var(--heatmap-color) 75%, var(--color-t-bg-highlight));
}

.heatmap-cell[data-level="4"] {
  background-color: var(--heatmap-color);
}
</style>

<style>
/* Global tooltip style (not scoped, since it's teleported) */
.heatmap-tooltip {
  position: fixed;
  transform: translate(-50%, -100%);
  background: var(--color-t-bg-dark);
  border: 1px solid var(--color-t-border);
  border-radius: 4px;
  padding: 4px 8px;
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-t-fg);
  pointer-events: none;
  z-index: 9999;
  white-space: nowrap;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

.heatmap-tooltip span {
  color: var(--color-t-fg-dark);
  font-size: 10px;
}
</style>
