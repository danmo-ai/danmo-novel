import type { MarkdownEntity, MarkdownField, StructuredMarkdown } from '@/types'

// Presentation-only fallback for candidates. The Go parser is the sole source
// of truth for validation, saving and LLM context; this never persists data.
export function parseStructuredMarkdown(raw: string, protocol: string): StructuredMarkdown {
  const root: MarkdownEntity = { kind: 'root', id: '', fields: [], range: range() }
  const doc: StructuredMarkdown = { protocol, version: 1, title: '', root, entities: [], legacy: !raw.includes('- schema_version:') }
  let current = root
  for (const [index, line] of raw.replace(/\r\n/g, '\n').split('\n').entries()) {
    const heading = line.match(/^(#{1,6})\s+(.+?)\s*$/)
    if (heading) {
      const text = heading[2]
      if (heading[1].length === 1 || /^(全书总纲|卷纲|结构审校)/.test(text)) {
        doc.title = text
        current = root
        continue
      }
      const match = text.match(/^(故事线|分卷|单元|节拍|转折|承诺|约束|揭示|卷末交接|问题)\s*(?:·|:|：)?\s*(.*)$/)
      if (!match) continue
      const entity: MarkdownEntity = { kind: match[1] === '问题' ? 'finding' : match[1], id: match[2].trim(), fields: [], range: range(index + 1) }
      doc.entities.push(entity)
      current = entity
      continue
    }
    const field = line.match(/^\s*-\s+([a-z][a-z0-9_]*)\s*:\s*(.*)$/)
    if (field) current.fields.push({ name: field[1], value: field[2].trim(), range: range(index + 1) } as MarkdownField)
  }
  return doc
}
function range(line = 1) { return { line, column: 1, endLine: line, endColumn: 1, byteOffset: 0, byteEnd: 0 } }
