export type CastLike = { stem: string; name?: string; role?: string; raw?: string; tagline?: string }
export type CastRelation = { from: string; to: string; type: string; current: string; formal: boolean }

function normalizePersonKey(s: string): string {
  return (s || '')
    .replace(/^#\s*/, '')
    .replace(/[（(].*$/, '')
    .replace(/[`\s\-_/]/g, '')
    .toLowerCase()
}

export function resolveCastRef(ref: string, cards: CastLike[]): string {
  const cleaned = (ref || '').replace(/[`"'‘’]/g, '').replace(/等$/, '').trim()
  if (!cleaned) return ''
  if (cards.some((c) => c.stem === cleaned)) return cleaned
  const want = normalizePersonKey(cleaned)
  if (!want) return ''
  for (const c of cards) {
    if (normalizePersonKey(c.name || '') === want || normalizePersonKey(c.stem) === want) return c.stem
  }
  let hit = ''
  for (const c of cards) {
    const name = normalizePersonKey(c.name || '')
    if (!name) continue
    if (name.startsWith(want) || want.startsWith(name)) {
      if (hit && hit !== c.stem) return ''
      hit = c.stem
    }
  }
  return hit
}

function splitTableCells(line: string): string[] {
  return line.trim().replace(/^\||\|$/g, '').split('|').map((s) => s.trim())
}

function sectionBody(raw: string, heading: RegExp): string {
  const m = raw.match(heading)
  return m?.[1] || ''
}

function parseFormal(raw: string, from: string, cards: CastLike[]): CastRelation[] {
  const section = sectionBody(raw, /##\s*(?:关系|人物关系)[^\n]*\n([\s\S]*?)(?=\n##\s|$)/)
  const out: CastRelation[] = []
  for (const line of section.split('\n')) {
    if (!line.trim().startsWith('|') || /^\s*\|?\s*:?-+/.test(line)) continue
    const cells = splitTableCells(line)
    if (cells.length < 2 || ['对方', '角色', '人物', 'stem'].includes(cells[0])) continue
    const to = resolveCastRef(cells[0], cards)
    if (!to || to === from) continue
    out.push({ from, to, type: cells[1] || '', current: cells[2] || '', formal: true })
  }
  return out
}

function splitAnchorParts(body: string): string[] {
  return body.replace(/;/g, '；').split('；').map((s) => s.trim()).filter(Boolean)
}

function splitNamesAndDesc(part: string): { names: string[]; desc: string } {
  const open = part.search(/[（(]/)
  let head = part
  let desc = ''
  if (open >= 0) {
    const closeRel = part.slice(open + 1).search(/[）)]/)
    if (closeRel >= 0) {
      const close = open + 1 + closeRel
      head = part.slice(0, open).trim()
      desc = part.slice(open + 1, close).trim()
    }
  }
  const names = head
    .replace(/[、，/]/g, ',')
    .split(',')
    .map((n) => n.replace(/等$/, '').trim())
    .filter(Boolean)
  return { names, desc }
}

function parseAnchor(raw: string, from: string, cards: CastLike[]): CastRelation[] {
  const m = raw.match(/^[\t *>-]*\*{0,2}关系锚点\*{0,2}[：:]\s*(.+)$/m)
  if (!m?.[1]) return []
  const out: CastRelation[] = []
  for (const part of splitAnchorParts(m[1].trim())) {
    const { names, desc } = splitNamesAndDesc(part)
    const typ = desc ? desc.slice(0, 16) : '关系锚点'
    for (const name of names) {
      const to = resolveCastRef(name, cards)
      if (!to || to === from) continue
      out.push({ from, to, type: typ, current: desc, formal: false })
    }
  }
  return out
}

function normalizeRole(role?: string) {
  const r = (role || '').toLowerCase()
  if (['protagonist', 'hero', 'main'].includes(r)) return 'protagonist'
  if (['antagonist', 'volume_antagonist', 'villain'].includes(r)) return 'volume_antagonist'
  return 'recurring'
}

/** Parse formal ## 关系 tables + 关系锚点 / 与主角关系 prose. */
export function parseCastRelations(cards: CastLike[]): CastRelation[] {
  const result: CastRelation[] = []
  const seen = new Set<string>()
  const add = (r: CastRelation) => {
    if (!r.from || !r.to || r.from === r.to) return
    const key = [r.from, r.to].sort().join('|')
    if (seen.has(key)) return
    seen.add(key)
    result.push(r)
  }
  const protagonist = cards.find((c) => normalizeRole(c.role) === 'protagonist')
  for (const card of cards) {
    const raw = card.raw || ''
    for (const r of parseFormal(raw, card.stem, cards)) add(r)
    for (const r of parseAnchor(raw, card.stem, cards)) add(r)
    if (protagonist && card.stem !== protagonist.stem) {
      const text = raw.match(/^[\t *>-]*\*{0,2}(?:与主角关系|关键关系|关系钩)\*{0,2}[：:]\s*(.+)$/m)?.[1]?.trim()
        || raw.match(/(?:与主角关系|关键关系|关系钩)[：:]\s*(.+)/)?.[1]?.trim()
      if (text) add({ from: card.stem, to: protagonist.stem, type: '与主角', current: text, formal: false })
    }
  }
  return result
}

export function isProtagonist(role?: string) {
  return normalizeRole(role) === 'protagonist'
}
