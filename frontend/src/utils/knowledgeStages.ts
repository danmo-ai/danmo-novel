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
