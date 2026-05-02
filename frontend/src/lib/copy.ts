// Build clean multi-line copy text from a selection that crosses several
// flex-based rows. Each row participating in copy must carry a
// `data-copytext` attribute with its formatted line.
//
// Returns null when the selection touches fewer than two rows so the caller
// can let the browser handle a partial / single-row copy normally.
export function selectedRowsText(container: Element, sel: Selection | null): string | null {
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null

  const rows = container.querySelectorAll<HTMLElement>('[data-copytext]')
  const lines: string[] = []
  for (const row of rows) {
    if (sel.containsNode(row, true)) {
      const text = row.dataset.copytext
      if (text) lines.push(text)
    }
  }
  if (lines.length < 2) return null
  return lines.join('\n')
}
