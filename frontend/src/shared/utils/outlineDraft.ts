/**
 * 把细纲候选（JSON 契约）渲染成合同视图所需的 YAML 形状。
 *
 * UnitContract 读的是落盘 YAML（snake_case），候选走的是 JSON（camelCase）。
 * 这里只做键名与缩进的映射，让「候选细纲」与「已接受细纲」用同一个渲染器，
 * 作者不必对着 JSON 判断欲望/阻碍/选择。规则与 iOS 的 UnitOutlineDraft.yamlLike 对应。
 */
import type { UnitInfo, UnitOutline } from '@/shared/types'

const SCALARS: [string, string][] = [
  ['unitId', 'unit_id'], ['title', 'title'], ['function', 'function'],
  ['desire', 'desire'], ['obstacle', 'obstacle'], ['choice', 'choice'],
  ['payoff', 'payoff'], ['pleasure', 'pleasure'], ['forbidden', 'forbidden'],
  ['pov', 'pov'], ['storyDay', 'story_day'], ['endDay', 'end_day'],
  ['gapDays', 'gap_days'], ['gapFromPrev', 'gap_from_prev'],
  ['flashback', 'flashback'], ['infoControl', 'info_control'],
  ['chapterChars', 'chapter_chars'], ['wordTarget', 'word_target'],
]

function text(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) return `[${value.map((v) => String(v)).join(', ')}]`
  return String(value)
}

export function outlineDraftToYaml(draft: UnitOutline): string {
  const lines: string[] = []
  for (const [json, yaml] of SCALARS) {
    const value = text(draft[json])
    if (value) lines.push(`${yaml}: ${value}`)
  }
  if (Array.isArray(draft.onStage) && draft.onStage.length) {
    lines.push(`on_stage: [${draft.onStage.join(', ')}]`)
  }
  if (draft.nextHook && typeof draft.nextHook === 'object') {
    lines.push('next_hook:')
    for (const key of Object.keys(draft.nextHook).sort()) {
      const value = text((draft.nextHook as Record<string, unknown>)[key])
      if (value) lines.push(`  ${key}: ${value}`)
    }
  }
  if (Array.isArray(draft.chapters) && draft.chapters.length) {
    lines.push('chapters:')
    for (const chapter of draft.chapters as Record<string, unknown>[]) {
      lines.push(`  - chapter: ${text(chapter.chapter)}`)
      for (const key of ['goal', 'change', 'cutHook', 'cut_hook', 'dayOffset', 'day_offset']) {
        if (chapter[key] === undefined) continue
        const yamlKey = key === 'cutHook' ? 'cut_hook' : key === 'dayOffset' ? 'day_offset' : key
        lines.push(`    ${yamlKey}: ${text(chapter[key])}`)
      }
      if (Array.isArray(chapter.scenes) && chapter.scenes.length) {
        lines.push('    scenes:')
        for (const scene of chapter.scenes as Record<string, unknown>[]) {
          let first = true
          for (const key of ['id', 'chapter', 'goal', 'conflict', 'turn', 'outcome', 'who', 'where']) {
            const value = text(scene[key])
            if (!value) continue
            lines.push(first ? `      - ${key}: ${value}` : `        ${key}: ${value}`)
            first = false
          }
          for (const key of ['mustLand', 'reveal', 'withhold']) {
            const items = scene[key]
            if (!Array.isArray(items) || !items.length) continue
            const yamlKey = key === 'mustLand' ? 'must_land' : key
            lines.push(`        ${yamlKey}: [${items.join(', ')}]`)
          }
        }
      }
    }
  }
  return lines.join('\n')
}

/** 用候选细纲合成一个 UnitInfo，交给合同渲染器；缺失字段回落已落盘单元。 */
export function outlineDraftUnit(draft: UnitOutline, existing?: UnitInfo): UnitInfo {
  const unitId = String(draft.unitId || existing?.unitId || '')
  return {
    unitId,
    volume: existing?.volume ?? 0,
    index: existing?.index ?? 0,
    status: existing?.status ?? 'proposed',
    title: String(draft.title || existing?.title || ''),
    chapterFrom: Number(draft.chapterFrom ?? existing?.chapterFrom ?? 0),
    chapterTo: Number(draft.chapterTo ?? existing?.chapterTo ?? 0),
    hasProse: existing?.hasProse ?? false,
    outlinePath: existing?.outlinePath ?? '',
    prosePath: existing?.prosePath ?? '',
    chapterTarget: Number(draft.chapterChars ?? existing?.chapterTarget ?? 0),
    outlineRaw: outlineDraftToYaml(draft),
  }
}
