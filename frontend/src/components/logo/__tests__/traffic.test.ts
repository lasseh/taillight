import { expect, test } from 'vitest'
import { gridOf } from '../flow'
import { slashT, tips } from '../mark'
import { trafficKnobs, trafficOf } from '../traffic'

// A seeded random source, so a run plays the same every time.
function seeded(seed = 1) {
  return () => {
    seed = (seed * 1664525 + 1013904223) >>> 0
    return seed / 2 ** 32
  }
}

test('traffic lights cells, never past full, within 10 s', () => {
  const traffic = trafficOf(gridOf(slashT), tips, seeded())
  let least = 0
  let most = 0
  for (let f = 0; f < 600; f++) {
    traffic.tick(1 / 60, trafficKnobs)
    least = Math.min(least, ...traffic.lit)
    most = Math.max(most, ...traffic.lit)
  }
  expect(least).toBe(0)
  expect(most).toBeGreaterThan(0.3)
  expect(most).toBeLessThanOrEqual(1)
})

test('a press sends a car from the pressed cell', () => {
  const grid = gridOf(slashT)
  const traffic = trafficOf(grid, tips, seeded())
  // No cars of its own, so only the press puts one on the road.
  const knobs = { ...trafficKnobs, cars: 0 }
  traffic.tick(1, knobs)
  expect(traffic.lit.every((v) => v === 0)).toBe(true)

  // A cell in the T's stem, which is no tip.
  const cell = grid.cellAt(30, 15)
  traffic.press(cell)
  traffic.tick(0.25, knobs)
  expect(traffic.lit[cell]).toBeGreaterThan(0.5)
})
