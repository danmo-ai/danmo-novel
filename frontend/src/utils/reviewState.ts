import type { ReviewFinding } from '@/types'

/**
 * 这两条不是"意见"，是"这一章没审完"的标记：输出撞到预算被截断，或分章
 * 审校里该章调用失败。它们是 advisory——截断的产物判不出严重度——但绝不能
 * 因此被读成"没有阻断，可以过"。后端 domain.ReviewVerdict 用的是同一套判据。
 */
const UNFINISHED_IDS = new Set(['review-truncated', 'review-incomplete'])

export type ReviewVerdict = 'clean' | 'blocking' | 'unfinished'

export function isUnfinishedFinding(f: ReviewFinding) {
  return UNFINISHED_IDS.has(f.id)
}

/**
 * 能否靠正文补丁修掉：必须落在具体某一章上。
 *
 * 角色状态账本（stale-*）、身份规划、审校覆盖标记都是整单元结论，没有章号。
 * 它们改正文修不了（stale-* 要在细纲 state_deltas 里写终值），送去「自动
 * 修复」只会在服务端被整批拒绝——一条这样的意见就能让另外几十条白选。
 * 所以它们不参与勾选，服务端 RunRevise 用的是同一条判据（chapter > 0）。
 */
export function isProseFixable(f: ReviewFinding) {
  return Number(f.chapter) > 0
}

/**
 * 一条 finding 是否算「阻断」。单一判据，供标题计数与 verdict 共用。
 *
 * 与后端 domain.ReviewVerdict 逐条对应：单轮检出的 blocking 不算（同一章两次
 * 调用给出的 blocking 完全不同，单轮即可阻断会让清单关不掉）；roundsHit 缺省
 * （旧数据、本地硬门）按原语义阻断。
 */
export function isBlockingFinding(f: ReviewFinding) {
  if (isUnfinishedFinding(f) || f.severity !== 'blocking') return false
  return !f.roundsHit || f.roundsHit >= 2
}
/**
 * blocking 优先于 unfinished：已知硬伤可执行，审校不完整只是缺口。
 *
 * 这里只决定徽章与排序，不推进任何状态（清单只有修复/拒绝两个出口，定稿在
 * 写作页单独发起）。判据必须与后端 domain.ReviewVerdict 一致，否则徽章与实际
 * 判定会互相矛盾。实际规则全部收敛在 isBlockingFinding。
 */
export function reviewVerdict(findings: ReviewFinding[] = []): ReviewVerdict {
  let unfinished = false
  for (const f of findings) {
    if (isUnfinishedFinding(f)) {
      unfinished = true
      continue
    }
    if (isBlockingFinding(f)) return 'blocking'
  }
  return unfinished ? 'unfinished' : 'clean'
}

export const VERDICT_LABEL: Record<ReviewVerdict, string> = {
  // 「可通过」是旧语义（面板上曾有"通过"按钮）。现在清单只有修复/拒绝两个出口，
  // 干净只说明"没有值得修的问题了"，不推进任何状态，所以措辞改成中性的描述。
  clean: '无待修问题',
  blocking: '有阻断',
  unfinished: '审校不完整',
}
