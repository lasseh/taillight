// The cell graph the login mark's cars drive on, ported from netops-dashboard
// (src/views/lab/taillightFlow.ts at 3f43390): which cells touch, the parts
// that do not, a link across each gap, and shortest paths. No Vue, no canvas.

export interface Cell {
  x: number
  y: number
}

export interface Grid {
  across: number
  down: number
  cells: Cell[]
  // The cell at a grid position, or -1.
  cellAt: (x: number, y: number) => number
  // The drawn cells among the eight around a cell.
  touching: number[][]
  // Which part a cell is in: parts are groups of cells that do not touch.
  part: Int8Array
  parts: number
}

export function gridOf(rows: readonly string[]): Grid {
  const across = rows[0]!.length
  const down = rows.length
  const cells = rows.flatMap((row, y) => [...row].flatMap((c, x) => (c === '#' ? [{ x, y }] : [])))
  const at = new Int16Array(across * down).fill(-1)
  cells.forEach(({ x, y }, i) => (at[y * across + x] = i))
  const cellAt = (x: number, y: number) =>
    x >= 0 && x < across && y >= 0 && y < down ? at[y * across + x]! : -1
  const touching = cells.map(({ x, y }) => {
    const out: number[] = []
    for (let dy = -1; dy <= 1; dy++)
      for (let dx = -1; dx <= 1; dx++) {
        const j = dx || dy ? cellAt(x + dx, y + dy) : -1
        if (j >= 0) out.push(j)
      }
    return out
  })
  const part = new Int8Array(cells.length).fill(-1)
  let parts = 0
  for (let i = 0; i < cells.length; i++) {
    if (part[i]! >= 0) continue
    const stack = [i]
    part[i] = parts
    while (stack.length)
      for (const j of touching[stack.pop()!]!)
        if (part[j]! < 0) {
          part[j] = parts
          stack.push(j)
        }
    parts++
  }
  return { across, down, cells, cellAt, touching, part, parts }
}

// One link across the gap between each pair of parts that come within reach
// of each other: at their closest pair of cells, the pair nearest the middle.
export function bridges(grid: Grid, reach = 6): [number, number][] {
  const { cells, part, parts, across } = grid
  const out: [number, number][] = []
  for (let a = 0; a < parts; a++)
    for (let b = a + 1; b < parts; b++) {
      let best: { i: number; j: number; d: number; mid: number } | undefined
      for (let i = 0; i < cells.length; i++) {
        if (part[i] !== a) continue
        for (let j = 0; j < cells.length; j++) {
          if (part[j] !== b) continue
          const d = Math.hypot(cells[i]!.x - cells[j]!.x, cells[i]!.y - cells[j]!.y)
          const mid = Math.abs((cells[i]!.x + cells[j]!.x) / 2 + 0.5 - across / 2)
          if (d <= reach && (!best || d < best.d - 0.01 || (d <= best.d + 0.01 && mid < best.mid)))
            best = { i, j, d, mid }
        }
      }
      if (best) out.push([best.i, best.j])
    }
  return out
}

// The cells as a graph: the touching cells and the links.
export function graphOf(grid: Grid, links: [number, number][]): number[][] {
  const out = grid.touching.map((n) => [...n])
  for (const [i, j] of links) {
    out[i]!.push(j)
    out[j]!.push(i)
  }
  return out
}

// A shortest path from a to b, ties broken at random, so two cars between the
// same cells can take different roads. Just [a] when b cannot be reached.
export function route(graph: number[][], a: number, b: number, random = Math.random): number[] {
  const from = new Int16Array(graph.length).fill(-1)
  from[a] = a
  const queue = [a]
  for (let q = 0; q < queue.length && from[b]! < 0; q++) {
    const i = queue[q]!
    for (const j of [...graph[i]!].sort(() => random() - 0.5))
      if (from[j]! < 0) {
        from[j] = i
        queue.push(j)
      }
  }
  if (from[b]! < 0) return [a]
  const path = [b]
  while (path[path.length - 1] !== a) path.push(from[path[path.length - 1]!]!)
  return path.reverse()
}

// A car of light: how lit a cell is, d cells behind the head's centre
// (negative: ahead of it). It fades in over the cell ahead, so it glides
// rather than hops, and fades out over `tail` cells behind.
export const carLight = (d: number, tail: number) =>
  d < -1 ? 0 : d < 0 ? 1 + d : Math.max(0, 1 - d / tail)

export type RGB = [number, number, number]
export const rgbOf = (hex: string): RGB =>
  [1, 3, 5].map((k) => parseInt(hex.slice(k, k + 2), 16)) as RGB
export const mixRGB = (a: RGB, b: RGB, t: number): RGB => [
  a[0] + (b[0] - a[0]) * t,
  a[1] + (b[1] - a[1]) * t,
  a[2] + (b[2] - a[2]) * t,
]
