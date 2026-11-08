/**
 * Steps at which the run confirm dialog lets the author attach reference
 * knowledge. Keep in sync with `knowledge.Stages` in
 * `core/service/knowledge/knowledge.go` (the backend only accepts these IDs).
 *
 * `finalize` is deliberately absent: it runs local hard gates, no LLM call.
 * `local_edit`/`continue`/`expand`/`polish` start without a preview dialog, so
 * they have no knowledge picker either.
 */
export const KNOWLEDGE_STAGES = [
  { id: 'plan', label: '立项' },
  { id: 'volume', label: '卷纲' },
  { id: 'outline', label: '单元细纲' },
  { id: 'write', label: '单元正文' },
  { id: 'review', label: '审校' },
  { id: 'entity', label: '设定重写' },
] as const

export type KnowledgeStageId = (typeof KNOWLEDGE_STAGES)[number]['id']

export function knowledgeStageLabel(id: string) {
  return KNOWLEDGE_STAGES.find((s) => s.id === id)?.label || id
}

/** Maps a run TaskSpec onto the stage whose articles the dialog should offer. */
export function specKnowledgeStage(spec: Record<string, any>): string {
  switch (spec.kind) {
    case 'plan':
      // 立项 / 重新立项 share one tag: the same articles shape the bible.
      return spec.mode === 'volume' ? 'volume' : 'plan'
    case 'idea':
      // 创意扩写与立项是同一段作者意图：立项类文章在这里同样适用。
      return 'plan'
    case 'outline':
      return 'outline'
    case 'write':
      return 'write'
    case 'review':
      return 'review'
    case 'rewrite_entity':
      return 'entity'
    default:
      return ''
  }
}

/**
 * An article without tags applies everywhere so a new article is never
 * silently invisible; tagged articles only show at their own steps.
 */
export function knowledgeAppliesTo(stages: string[] | undefined, stage: string) {
  if (!stage || !stages?.length) return true
  return stages.includes(stage)
}

/** Short human summary of an article's tags, for list rows. */
export function knowledgeStageSummary(stages: string[] | undefined) {
  if (!stages?.length) return '未标记'
  return stages.map(knowledgeStageLabel).join(' · ')
}

/**
 * Book genres an article can be restricted to. Mirrors `GenreFamiliesList` in
 * `core/service/knowledge/knowledge.go`; an article with no genre tag is 公共
 * and is offered to every book.
 */
export const KNOWLEDGE_GENRES = [
  '玄幻',
  '仙侠',
  '都市',
  '悬疑',
  '现代言情',
  '古代言情',
  '仕途扫黑',
  '系统穿越',
] as const

/**
 * Same alias table as the backend: book metadata may carry a platform category
 * (悬疑灵异, 都市修真) or a broad word (言情, 系统), and it must still reach the
 * matching family. Matching is set intersection.
 */
const GENRE_ALIASES: Record<string, string[]> = {
  玄幻: ['玄幻'], 奇幻: ['玄幻'], 东方玄幻: ['玄幻'], 西方奇幻: ['玄幻'],
  仙侠: ['仙侠'], 修真: ['仙侠'], 武侠: ['仙侠'], 凡人修仙: ['仙侠'], 古典仙侠: ['仙侠'],
  都市: ['都市'], 现实: ['都市'], 都市日常: ['都市'], 都市高武: ['都市'], 战神赘婿: ['都市'],
  悬疑: ['悬疑'], 推理: ['悬疑'], 刑侦: ['悬疑'], 灵异: ['悬疑'], 悬疑灵异: ['悬疑'],
  言情: ['现代言情', '古代言情'],
  现代言情: ['现代言情'], 都市言情: ['现代言情'],
  古代言情: ['古代言情'], 古言: ['古代言情'], 宫斗: ['古代言情'],
  仕途扫黑: ['仕途扫黑'], 仕途: ['仕途扫黑'], 官场: ['仕途扫黑'], 扫黑: ['仕途扫黑'],
  系统穿越: ['系统穿越'], 系统: ['系统穿越'], 签到: ['系统穿越'], 快穿: ['系统穿越'], 穿越: ['系统穿越'],
}

export function genreFamilies(name?: string): string[] {
  const n = (name || '').trim()
  if (!n) return []
  const alias = GENRE_ALIASES[n]
  if (alias) return alias
  const families: string[] = []
  const add = (fams: string[]) => {
    for (const f of fams) if (!families.includes(f)) families.push(f)
  }
  for (const f of KNOWLEDGE_GENRES) if (n.includes(f)) add([f])
  for (const [key, fams] of Object.entries(GENRE_ALIASES)) {
    if (key !== n && n.includes(key)) add(fams)
  }
  const ordered = KNOWLEDGE_GENRES.filter((f) => families.includes(f))
  return ordered.length ? [...ordered] : [n]
}

/**
 * Subgenre spellings that name the same 子类, mirroring `subgenreAliases` in
 * `core/resource/novel/genre.go`. 95-genre-routing 把警察、法医、社会派都归在
 * 「刑侦探案」，书里写成 年代刑侦 / 刑侦 / 探案 都是同一子类。
 */
const SUBGENRE_ALIASES: Record<string, string> = {
  刑侦探案: '刑侦探案',
  刑侦: '刑侦探案',
  年代刑侦: '刑侦探案',
  探案: '刑侦探案',
  侦探推理: '刑侦探案',
  侦探: '刑侦探案',
  警察: '刑侦探案',
  法医: '刑侦探案',
  社会派: '刑侦探案',
}

/** Canonical subgenre name, or '' when the spelling is unknown. */
export function canonicalSubgenre(subgenre?: string): string {
  const s = (subgenre || '').trim()
  if (!s) return ''
  if (SUBGENRE_ALIASES[s]) return SUBGENRE_ALIASES[s]
  for (const [alias, canonical] of Object.entries(SUBGENRE_ALIASES)) {
    if (alias !== canonical && s.includes(alias)) return canonical
  }
  return ''
}

/** Whether a tagged subgenre and a book subgenre name the same 子类. */
function subgenreMatched(tagged: string, book: string): boolean {
  const t = canonicalSubgenre(tagged)
  const b = canonicalSubgenre(book)
  if (t && b) return t === b
  return tagged.trim().toLowerCase() === book.trim().toLowerCase()
}

/** True when the article is 公共 (offered for any book genre). */
export function isCommonKnowledge(doc: { genres?: string[]; subgenres?: string[] }) {
  return !doc.genres?.length && !doc.subgenres?.length
}

/**
 * Genre applicability, mirroring `Doc.GenreMatched`: 公共 always matches,
 * genre tags compare by family, subgenre tags are an extra restriction.
 */
export function knowledgeAppliesToGenre(
  doc: { genres?: string[]; subgenres?: string[] },
  bookGenre?: string,
  bookSubgenre?: string,
) {
  if (isCommonKnowledge(doc)) return true
  if (doc.genres?.length) {
    const book = genreFamilies(bookGenre)
    const hit = doc.genres.some((g) => genreFamilies(g).some((f) => book.includes(f)))
    if (!hit) return false
  }
  if (doc.subgenres?.length) {
    const sub = (bookSubgenre || '').trim()
    if (!sub) return false
    return doc.subgenres.some((s) => subgenreMatched(s, sub))
  }
  return true
}

/** Short human summary of an article's genre tags, for list rows. */
export function knowledgeGenreSummary(doc: { genres?: string[]; subgenres?: string[] }) {
  if (isCommonKnowledge(doc)) return '公共'
  const parts = [...(doc.genres || [])]
  if (doc.subgenres?.length) parts.push(`仅 ${doc.subgenres.join('、')}`)
  return parts.join(' · ')
}
