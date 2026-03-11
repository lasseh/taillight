/**
 * Match a value against a glob pattern where `*` matches any sequence of characters.
 * Case-insensitive, full-string anchored.
 */
export function wildcardMatch(value: string, pattern: string): boolean {
  if (pattern === '*') return true
  if (!pattern.includes('*')) return value.toLowerCase() === pattern.toLowerCase()

  const re = new RegExp(
    '^' +
      pattern
        .toLowerCase()
        .split('*')
        .map((s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
        .join('.*') +
      '$',
  )
  return re.test(value.toLowerCase())
}
