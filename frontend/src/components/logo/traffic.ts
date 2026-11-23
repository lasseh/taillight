// The login mark's Traffic scene, ported from netops-dashboard
// (src/views/lab/taillightScenes.ts at 3f43390): cars leave a tip for another
// and take a shortest path, crossing the gaps over their links; every cell a
// car passes warms a little and cools over 10 s. A press sends a car from the
// cell. The random source is passed in, so tests can seed it.
import { bridges, carLight, graphOf, route, type Grid } from './flow'

export interface TrafficKnobs {
  // Cells a second.
  speed: number
  // Cells a car's light fades over behind it.
  tail: number
  // Cars on the road at once, at most.
  cars: number
}

// The reference's starting sliders, as Lasse tried them.
export const trafficKnobs: TrafficKnobs = { speed: 4, tail: 5, cars: 4 }

export interface Traffic {
  // How lit each cell is, 0 to 1, after the last tick.
  lit: Float32Array
  tick: (dt: number, knobs: TrafficKnobs) => void
  press: (cell: number) => void
}

export function trafficOf(
  grid: Grid,
  tips: readonly [number, number][],
  random = Math.random,
): Traffic {
  const graph = graphOf(grid, bridges(grid))
  const ends = tips.map(([x, y]) => grid.cellAt(x, y))
  const pick = () => ends[Math.floor(random() * ends.length)]!
  const lit = new Float32Array(grid.cells.length)
  const load = new Float32Array(grid.cells.length)
  const cars: { path: number[]; pos: number; pace: number }[] = []
  let nextSpawn = 0
  const put = (i: number, v: number) => (lit[i] = Math.min(1, Math.max(lit[i]!, v)))
  const send = (from: number, to: number) => {
    if (from !== to)
      cars.push({ path: route(graph, from, to, random), pos: -1, pace: 0.85 + random() * 0.3 })
  }
  return {
    lit,
    tick(dt, { speed, tail, cars: wanted }) {
      lit.fill(0)
      nextSpawn -= dt
      if (nextSpawn <= 0 && cars.length < wanted) {
        send(pick(), pick())
        nextSpawn = (0.6 + random() * 1.6) * (4 / speed)
      }
      for (let i = 0; i < load.length; i++) load[i]! *= Math.exp(-dt / 10)
      for (let k = cars.length - 1; k >= 0; k--) {
        const car = cars[k]!
        const was = Math.floor(car.pos)
        car.pos += speed * car.pace * dt
        for (
          let s = Math.max(0, was + 1);
          s <= Math.min(Math.floor(car.pos), car.path.length - 1);
          s++
        )
          load[car.path[s]!] = Math.min(1, load[car.path[s]!]! + 0.06)
        // Gone once its tail has passed the end of its road.
        if (car.pos - tail > car.path.length) {
          cars.splice(k, 1)
          continue
        }
        for (
          let s = Math.max(0, Math.floor(car.pos - tail));
          s <= Math.min(car.path.length - 1, Math.ceil(car.pos) + 1);
          s++
        )
          put(car.path[s]!, carLight(car.pos - s, tail))
      }
      for (let i = 0; i < load.length; i++) put(i, load[i]! * 0.3)
    },
    press: (cell) => send(cell, pick()),
  }
}
