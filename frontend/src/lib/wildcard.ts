/**
 * Match a value against a glob pattern where `*` matches any sequence of characters.
 * Case-insensitive, full-string anchored.
 *
 * A single left-to-right scan for each literal segment, like the server's
 * matchWildcard (api/internal/model/srvlog.go). A regex here would backtrack
 * on patterns with many stars, and filters come from the URL.
 */
export function wildcardMatch(value: string, pattern: string): boolean {
  if (pattern === '*') return true
  if (!pattern.includes('*')) return value.toLowerCase() === pattern.toLowerCase()

  const lower = value.toLowerCase()
  const parts = pattern.toLowerCase().split('*')
  let pos = 0
  for (const [i, part] of parts.entries()) {
    if (part === '') continue
    const idx = lower.indexOf(part, pos)
    if (idx < 0) return false
    // First segment must anchor at start.
    if (i === 0 && idx !== 0) return false
    pos = idx + part.length
  }
  // Last segment must anchor at end.
  const last = parts[parts.length - 1]!
  return last === '' || lower.endsWith(last)
}
