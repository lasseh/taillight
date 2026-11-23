import { expect, test } from 'vitest'
import { bridges, carLight, graphOf, gridOf, route } from '../flow'
import { slashT, tips } from '../mark'

test('//T// is five parts, linked in a chain: slash, slash, T, slash, slash', () => {
  const grid = gridOf(slashT)
  expect(grid.parts).toBe(5)
  // Each part's leftmost column, so the links can be named by it.
  const left = Array.from({ length: grid.parts }, (_, p) =>
    Math.min(...grid.cells.filter((_, i) => grid.part[i] === p).map((c) => c.x)),
  )
  const links = bridges(grid).map(([i, j]) =>
    [left[grid.part[i]!], left[grid.part[j]!]].sort((a, b) => a! - b!),
  )
  expect(links.sort((a, b) => a[0]! - b[0]!)).toEqual([
    [0, 7],
    [7, 21],
    [21, 38],
    [38, 45],
  ])
})

test('a car from any tip reaches any other, one cell or one link a step', () => {
  const grid = gridOf(slashT)
  const graph = graphOf(grid, bridges(grid))
  const at = tips.map(([x, y]) => grid.cellAt(x, y))
  expect(at.every((i) => i >= 0)).toBe(true)
  for (const a of at)
    for (const b of at) {
      const path = route(graph, a, b)
      expect(path[0]).toBe(a)
      expect(path[path.length - 1]).toBe(b)
      for (let s = 1; s < path.length; s++) expect(graph[path[s - 1]!]).toContain(path[s])
    }
})

test('a car fades in over the cell ahead and out over its tail', () => {
  expect(carLight(-1.5, 5)).toBe(0)
  expect(carLight(-0.5, 5)).toBe(0.5)
  expect(carLight(0, 5)).toBe(1)
  expect(carLight(2.5, 5)).toBe(0.5)
  expect(carLight(6, 5)).toBe(0)
})
