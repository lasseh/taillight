import { expect, test } from 'vitest'
import { gridOf } from '../flow'
import { slashT } from '../mark'

test('//T// is 58 cells across, 30 down', () => {
  expect(slashT).toHaveLength(30)
  for (const row of slashT) expect(row).toMatch(/^[#.]{58}$/)
})

test("//T//'s slashes turned half round are themselves", () => {
  const grid = gridOf(slashT)
  const t = grid.part[grid.cellAt(21, 4)]
  const slashes = grid.cells.filter((_, i) => grid.part[i] !== t)
  const at = new Set(slashes.map((c) => `${c.x} ${c.y}`))
  expect(slashes.filter((c) => !at.has(`${grid.across - 1 - c.x} ${grid.down - 1 - c.y}`))).toEqual(
    [],
  )
})

test("//T//'s T is 22 wide with a 6-wide stem, 4 rows inside the slashes", () => {
  const grid = gridOf(slashT)
  const t = grid.cells.filter((_, i) => grid.part[i] === grid.part[grid.cellAt(21, 4)])
  const ys = t.map((c) => c.y)
  expect([Math.min(...ys), Math.max(...ys)]).toEqual([4, 25])
  const width = (y: number) => t.filter((c) => c.y === y).length
  expect([4, 5, 6, 7].map(width)).toEqual([22, 22, 22, 22])
  for (let y = 8; y <= 25; y++) expect(width(y)).toBe(6)
})
