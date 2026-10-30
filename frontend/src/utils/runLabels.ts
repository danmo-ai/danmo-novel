/** Human labels for run / candidate task kinds (plan uses spec.mode). */
export const KIND_LABELS: Record<string, string> = {
  plan: '立项',
  outline: '细纲',
  write: '正文',
  review: '审校',
  revise: '修订',
  finalize: '定稿',
  rewrite_entity: '重写设定',
  local_edit: '局部编辑',
  continue: '续写',
  expand: '扩写',
  polish: '润色',
  rewrite_selection: '选区改写',
}

const PLAN_MODE_LABELS: Record<string, string> = {
  init: '立项',
  reinit: '重新立项',
  volume: '卷纲',
}

export function planModeLabel(mode?: string) {
  return PLAN_MODE_LABELS[mode || ''] || '立项'
}

export function runKindLabel(kind?: string, mode?: string) {
  if (kind === 'plan') return planModeLabel(mode)
  return KIND_LABELS[kind || ''] || kind || '任务'
}
