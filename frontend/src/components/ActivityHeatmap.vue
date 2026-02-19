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

// ── Build the grid: 48 columns x 7 rows, covering 7 days in 30-min slots ──

const SLOTS_PER_DAY = 48 // 24h × 2 half-hour slots
const DAY_MS = 86_400_000
const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

interface DayInfo {
  iso: string
  label: string
  shortDate: string
}

interface Cell {
  key: string        // "YYYY-MM-DD HH:mm"
  count: number
  level: number      // 0-4
  dayIndex: number   // 0-6 (row)
  slotIndex: number  // 0-47 (column)
  tipText: string
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function fmtDate(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

function buildDays(): DayInfo[] {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  return Array.from({ length: 7 }, (_, i) => {
    const d = new Date(today.getTime() - (6 - i) * DAY_MS)
    return {
      iso: fmtDate(d),
      label: i === 6 ? 'Today' : i === 5 ? 'Yest' : DAYS[d.getDay()],
      shortDate: `${d.getMonth() + 1}/${d.getDate()}`,
    }
  })
}

// grid depends on props.data (reactive), so days are recomputed each time data changes.
const grid = computed(() => {
  const dayInfos = buildDays()
  const cells: Cell[] = []
  const counts: number[] = []

  for (let dayIdx = 0; dayIdx < 7; dayIdx++) {
    const day = dayInfos[dayIdx]
    for (let slot = 0; slot < SLOTS_PER_DAY; slot++) {
      const h = Math.floor(slot / 2)
      const m = (slot % 2) * 30
      const key = `${day.iso} ${pad2(h)}:${pad2(m)}`
      const count = props.data[key] ?? 0
      counts.push(count)
      cells.push({
        key,
        count,
        level: 0,
        dayIndex: dayIdx,
        slotIndex: slot,
        tipText: `${day.label} ${day.shortDate}, ${pad2(h)}:${pad2(m)}`,
      })
    }
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

// ── Hour labels positioned at even-hour boundaries ──

const hourLabels = computed(() => {
  const labels: { label: string; slotIndex: number }[] = []
  for (let h = 0; h < 24; h += 3) {
    labels.push({ label: `${pad2(h)}`, slotIndex: h * 2 })
  }
  return labels
})

const totalCount = computed(() => grid.value.reduce((sum, c) => sum + c.count, 0))

// Day labels derived alongside grid (recomputes when props.data changes).
const dayLabels = computed(() => {
  // Touch props.data so this recomputes when data refreshes (and the date may have changed).
  void props.data
  return buildDays()
})

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
    <!-- Hour labels (top, replacing month labels) -->
    <div class="heatmap-hours" :style="{ gridTemplateColumns: `repeat(${SLOTS_PER_DAY}, 1fr)` }">
      <span
        v-for="h in hourLabels"
        :key="h.slotIndex"
        class="text-t-fg-dark text-[10px]"
        :style="{ gridColumnStart: h.slotIndex + 1 }"
      >{{ h.label }}</span>
    </div>

    <div class="flex gap-[2px]">
      <!-- Day labels (left sidebar) -->
      <div class="heatmap-day-labels">
        <span v-for="(day, i) in dayLabels" :key="i" class="text-t-fg-dark text-[10px]">
          {{ day.label }}
        </span>
      </div>

      <!-- Grid -->
      <div class="heatmap-grid" :style="{ gridTemplateColumns: `repeat(${SLOTS_PER_DAY}, 1fr)` }">
        <div
          v-for="(cell, i) in grid"
          :key="i"
          class="heatmap-cell"
          :data-level="cell.level"
          :style="{
            gridColumn: cell.slotIndex + 1,
            gridRow: cell.dayIndex + 1,
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

.heatmap-hours {
  display: grid;
  margin-left: 38px; /* align with grid after day labels */
  margin-bottom: 2px;
  height: 14px;
}

.heatmap-day-labels {
  display: grid;
  grid-template-rows: repeat(7, 1fr);
  gap: 2px;
  width: 34px;
  flex-shrink: 0;
}

.heatmap-day-labels span {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding-right: 4px;
  height: 11px;
}

.heatmap-grid {
  display: grid;
  grid-template-rows: repeat(7, 1fr);
  grid-auto-flow: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.heatmap-cell {
  width: 11px;
  height: 11px;
  border-radius: 2px;
  transition: outline 0.1s;
}

.heatmap-cell:hover {
  outline: 1px solid var(--color-t-fg-dark);
  outline-offset: -1px;
}

.heatmap-cell.legend {
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
