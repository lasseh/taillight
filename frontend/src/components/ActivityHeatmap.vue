<script setup lang="ts">
import { computed, ref } from 'vue'

const props = withDefaults(defineProps<{
  /** Map of ISO date string (YYYY-MM-DD) to count */
  data: Record<string, number>
  /** CSS color variable name for the heatmap accent, e.g. '--color-t-teal' */
  colorVar?: string
  /** Label shown in tooltip, e.g. 'syslog events' */
  label?: string
}>(), {
  colorVar: '--color-t-teal',
  label: 'events',
})

// ── Build the grid: 53 columns x 7 rows, covering ~1 year ending today ──

interface Cell {
  date: string       // YYYY-MM-DD
  count: number
  level: number      // 0-4 intensity
  dayOfWeek: number  // 0=Sun, 6=Sat
  weekIndex: number
}

const DAY_MS = 86_400_000

const grid = computed(() => {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const todayDow = today.getDay() // 0=Sun

  // Start from the Sunday of the week 52 weeks ago
  const startOffset = todayDow + 52 * 7
  const startDate = new Date(today.getTime() - startOffset * DAY_MS)

  const totalDays = startOffset + todayDow + 1 // include today
  const cells: Cell[] = []
  const counts: number[] = []

  for (let i = 0; i <= startOffset + todayDow; i++) {
    const d = new Date(startDate.getTime() + i * DAY_MS)
    const iso = formatDate(d)
    const count = props.data[iso] ?? 0
    counts.push(count)
    cells.push({
      date: iso,
      count,
      level: 0, // computed below
      dayOfWeek: d.getDay(),
      weekIndex: Math.floor(i / 7),
    })
  }

  // Compute levels based on percentile thresholds (GitHub style)
  const nonZero = counts.filter(c => c > 0).sort((a, b) => a - b)
  let thresholds = [0, 0, 0, 0]
  if (nonZero.length > 0) {
    const p25 = nonZero[Math.floor(nonZero.length * 0.25)] ?? 1
    const p50 = nonZero[Math.floor(nonZero.length * 0.50)] ?? 1
    const p75 = nonZero[Math.floor(nonZero.length * 0.75)] ?? 1
    thresholds = [1, p25, p50, p75]
  }

  for (const cell of cells) {
    if (cell.count === 0) {
      cell.level = 0
    } else if (cell.count < thresholds[1]) {
      cell.level = 1
    } else if (cell.count < thresholds[2]) {
      cell.level = 2
    } else if (cell.count < thresholds[3]) {
      cell.level = 3
    } else {
      cell.level = 4
    }
  }

  return cells
})

// ── Month labels positioned at the first week that starts in that month ──

const monthLabels = computed(() => {
  const labels: { label: string; weekIndex: number }[] = []
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
  let lastMonth = -1

  for (const cell of grid.value) {
    if (cell.dayOfWeek !== 0) continue // only check Sundays (start of week column)
    const month = parseInt(cell.date.slice(5, 7), 10) - 1
    if (month !== lastMonth) {
      labels.push({ label: months[month], weekIndex: cell.weekIndex })
      lastMonth = month
    }
  }

  return labels
})

const totalWeeks = computed(() => {
  if (grid.value.length === 0) return 0
  return grid.value[grid.value.length - 1].weekIndex + 1
})

const totalCount = computed(() => grid.value.reduce((sum, c) => sum + c.count, 0))

// ── Tooltip ──

const tooltip = ref<{ x: number; y: number; date: string; count: number } | null>(null)

function showTooltip(event: MouseEvent, cell: Cell) {
  const rect = (event.target as HTMLElement).getBoundingClientRect()
  tooltip.value = {
    x: rect.left + rect.width / 2,
    y: rect.top - 8,
    date: cell.date,
    count: cell.count,
  }
}

function hideTooltip() {
  tooltip.value = null
}

function formatDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function formatTooltipDate(iso: string): string {
  const d = new Date(iso + 'T00:00:00')
  return d.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' })
}
</script>

<template>
  <div class="heatmap-wrap">
    <!-- Month labels -->
    <div class="heatmap-months" :style="{ gridTemplateColumns: `repeat(${totalWeeks}, 1fr)` }">
      <span
        v-for="m in monthLabels"
        :key="m.weekIndex"
        class="text-t-fg-dark text-[10px]"
        :style="{ gridColumnStart: m.weekIndex + 1 }"
      >{{ m.label }}</span>
    </div>

    <div class="flex gap-[2px]">
      <!-- Day-of-week labels -->
      <div class="heatmap-day-labels">
        <span></span>
        <span class="text-t-fg-dark text-[10px]">Mon</span>
        <span></span>
        <span class="text-t-fg-dark text-[10px]">Wed</span>
        <span></span>
        <span class="text-t-fg-dark text-[10px]">Fri</span>
        <span></span>
      </div>

      <!-- Grid -->
      <div class="heatmap-grid" :style="{ gridTemplateColumns: `repeat(${totalWeeks}, 1fr)` }">
        <div
          v-for="(cell, i) in grid"
          :key="i"
          class="heatmap-cell"
          :data-level="cell.level"
          :style="{
            gridColumn: cell.weekIndex + 1,
            gridRow: cell.dayOfWeek + 1,
            '--heatmap-color': `var(${colorVar})`,
          }"
          @mouseenter="showTooltip($event, cell)"
          @mouseleave="hideTooltip"
        ></div>
      </div>
    </div>

    <!-- Legend -->
    <div class="mt-2 flex items-center justify-between">
      <span class="text-t-fg-dark text-[10px]">{{ totalCount.toLocaleString() }} {{ label }} in the last year</span>
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

    <!-- Tooltip (teleported to body would be ideal, but inline is fine for now) -->
    <Teleport to="body">
      <div
        v-if="tooltip"
        class="heatmap-tooltip"
        :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }"
      >
        <strong>{{ tooltip.count.toLocaleString() }} {{ label }}</strong>
        <span>{{ formatTooltipDate(tooltip.date) }}</span>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.heatmap-wrap {
  overflow-x: auto;
  overflow-y: hidden;
}

.heatmap-months {
  display: grid;
  margin-left: 30px; /* align with grid after day labels */
  margin-bottom: 2px;
  height: 14px;
}

.heatmap-day-labels {
  display: grid;
  grid-template-rows: repeat(7, 1fr);
  gap: 2px;
  width: 26px;
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

/* Intensity levels using the theme color variable */
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
