<script setup lang="ts">
// The //T// mark over the login card: square cells in the [Taillight]
// heading's gradient, emergency to alert, with cars of light driving on them
// (the Traffic scene, traffic.ts). Ported from netops-dashboard's
// TaillightFlow.vue at 3f43390. A lit cell is its own ink mixed toward the car
// colour by how lit it is, so a car shows over both ends of the gradient.
// Under reduced motion the mark stands still in its ink.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { gridOf, mixRGB, rgbOf } from './flow'
import { slashT, tips } from './mark'
import { trafficKnobs, trafficOf } from './traffic'

// 5 px a cell; a cell fills 0.8 of its square, so the gaps stay open.
const CELL = 5
const SHARE = 0.8
// How far a fully lit cell mixes toward the car colour.
const STRENGTH = 0.6
// The car colour, as in the reference. On the light themes it still reads: a
// pale streak over their darker ink; a dark car there was barely seen.
const CAR = rgbOf('#ffd6f0')
const STEPS = 64

const grid = gridOf(slashT)
const { cells, across, down } = grid
const traffic = trafficOf(grid, tips)
const { themeId } = useTheme()
// Device pixels a cell, so the cells' edges land on whole pixels.
const scale = Math.round(CELL * (window.devicePixelRatio || 1))

const canvas = ref<HTMLCanvasElement>()
let ctx: CanvasRenderingContext2D
// Each column's ink mixed toward the car colour in STEPS steps, made once a
// theme, so a frame only looks its colours up.
let shades: string[][] = []

// The canvas turns any CSS colour into #rrggbb.
function hexOf(colour: string): string {
  ctx.fillStyle = colour
  return ctx.fillStyle as string
}

function readInk() {
  const css = getComputedStyle(document.documentElement)
  const left = rgbOf(hexOf(css.getPropertyValue('--color-sev-emerg')))
  const right = rgbOf(hexOf(css.getPropertyValue('--color-sev-alert')))
  shades = Array.from({ length: across }, (_, x) => {
    const own = mixRGB(left, right, (x + SHARE / 2) / across)
    return Array.from(
      { length: STEPS + 1 },
      (_, k) =>
        `rgb(${mixRGB(own, CAR, k / STEPS)
          .map(Math.round)
          .join(',')})`,
    )
  })
}

function draw() {
  ctx.clearRect(0, 0, across, down)
  for (let i = 0; i < cells.length; i++) {
    const { x, y } = cells[i]!
    ctx.fillStyle = shades[x]![Math.round(STRENGTH * traffic.lit[i]! * STEPS)]!
    ctx.fillRect(x, y, SHARE, SHARE)
  }
}

let raf = 0
let last = 0
function frame(now: number) {
  traffic.tick(last ? Math.min(0.05, (now - last) / 1000) : 0, trafficKnobs)
  last = now
  draw()
  raf = requestAnimationFrame(frame)
}

onMounted(() => {
  ctx = canvas.value!.getContext('2d')!
  ctx.setTransform(scale, 0, 0, scale, 0, 0)
  readInk()
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) draw()
  else raf = requestAnimationFrame(frame)
})
onBeforeUnmount(() => cancelAnimationFrame(raf))

// setTheme has already set data-theme when this runs, so the new ink is read.
watch(themeId, () => {
  readInk()
  draw()
})

function press(e: PointerEvent) {
  const r = canvas.value!.getBoundingClientRect()
  const c = grid.cellAt(
    Math.floor(((e.clientX - r.left) / r.width) * across),
    Math.floor(((e.clientY - r.top) / r.height) * down),
  )
  if (c >= 0) traffic.press(c)
}
</script>

<template>
  <canvas
    ref="canvas"
    class="logo-mark"
    :width="across * scale"
    :height="down * scale"
    :style="{ width: `${across * CELL}px` }"
    aria-hidden="true"
    @pointerdown="press"
  />
</template>

<style scoped>
.logo-mark {
  display: block;
  max-width: 100%;
  height: auto;
}
</style>
