/**
 * 任务页的数据模型：run 与它产出的候选做客户端 join。
 *
 * LLM 调用是分钟级异步，作者真正的问题是「什么在等我」，而不是「这个单元刚产出
 * 了什么」。任务页把候选收件箱与操作记录融合成一条时间线：待确认、进行中、
 * 已终结（含失败）。
 *
 * 纯客户端 join，不需要新端点：精简 run 列表带 candidateId，精简候选列表带
 * runId，store 已同时拉取两份数据。规则与 iOS 的 TaskItem.swift 一一对应。
 */
import type { Candidate, Run } from '@/shared/types'
import { planModeLabel, runKindLabel } from '@/shared/utils/runLabels'

export type TaskSection = 'needsAction' | 'running' | 'closed'

export interface TaskItem {
  id: string
  run?: Run
  candidate?: Candidate
  section: TaskSection
  unitId?: string
  /** 该行是「审校清单 → 修订」折叠对里的活动项，携带被折叠的清单 id。 */
  foldedReviewCandidateId?: string
}

const SECTION_ORDER: Record<TaskSection, number> = { needsAction: 0, running: 1, closed: 2 }

function sectionOf(run?: Run, candidate?: Candidate): TaskSection {
  if (candidate && candidate.status === 'open') return 'needsAction'
  if (run && (run.status === 'pending' || run.status === 'running')) return 'running'
  return 'closed'
}

function unitIdOf(run?: Run, candidate?: Candidate): string | undefined {
  return candidate?.unitId || run?.spec?.unitId || undefined
}

export function buildTaskItems(candidates: Candidate[], runs: Run[], activeRun?: Run | null): TaskItem[] {
  const candidateByRunId = new Map<string, Candidate>()
  for (const c of candidates) {
    if (c.runId && !candidateByRunId.has(c.runId)) candidateByRunId.set(c.runId, c)
  }
  const claimed = new Set<string>()
  const items: TaskItem[] = []

  // runs 为主表：每次请求一行，包含失败且没产出候选的那些。
  for (const run of runs) {
    const candidate = (run.candidateId ? candidates.find((c) => c.id === run.candidateId) : undefined)
      ?? candidateByRunId.get(run.id)
    if (candidate) claimed.add(candidate.id)
    items.push({
      id: run.id,
      run,
      candidate,
      section: sectionOf(run, candidate),
      unitId: unitIdOf(run, candidate),
    })
  }

  // 孤儿候选：run 落在 limit 窗口之外，但 open 候选仍是待办，不能丢。
  for (const candidate of candidates) {
    if (claimed.has(candidate.id)) continue
    items.push({
      id: candidate.id,
      candidate,
      section: candidate.status === 'open' ? 'needsAction' : 'closed',
      unitId: unitIdOf(undefined, candidate),
    })
  }

  // 实时 run 可能比列表更新。
  if (activeRun && !items.some((i) => i.run?.id === activeRun.id)) {
    const candidate = activeRun.candidateId ? candidates.find((c) => c.id === activeRun.candidateId) : undefined
    items.push({
      id: activeRun.id,
      run: activeRun,
      candidate,
      section: sectionOf(activeRun, candidate),
      unitId: unitIdOf(activeRun, candidate),
    })
  }

  return sortTaskItems(foldReviewPairs(items))
}

/**
 * 单元的审校清单在其修订运行期间仍是 open —— 清单正是修订的输入，后端
 * blockingRun 刻意放行这一对。视觉上仍要读成「一个单元一件事」：隐藏清单行，
 * 由修订行携带它。
 */
function foldReviewPairs(items: TaskItem[]): TaskItem[] {
  const reviseByUnit = new Map<string, TaskItem>()
  for (const item of items) {
    if (itemKind(item) !== 'revise' || item.section === 'closed' || !item.unitId) continue
    const existing = reviseByUnit.get(item.unitId)
    if (!existing || createdAtOf(item) > createdAtOf(existing)) reviseByUnit.set(item.unitId, item)
  }
  if (!reviseByUnit.size) return items

  const reviewIdByUnit = new Map<string, string>()
  for (const item of items) {
    if (itemKind(item) !== 'review' || item.section !== 'needsAction') continue
    if (!item.unitId || !reviseByUnit.has(item.unitId) || !item.candidate) continue
    reviewIdByUnit.set(item.unitId, item.candidate.id)
  }

  const out: TaskItem[] = []
  for (const item of items) {
    if (itemKind(item) === 'review' && item.unitId && reviewIdByUnit.get(item.unitId) === item.candidate?.id) {
      continue
    }
    if (
      itemKind(item) === 'revise' && item.unitId
      && reviseByUnit.get(item.unitId)?.id === item.id
      && reviewIdByUnit.has(item.unitId)
    ) {
      out.push({ ...item, foldedReviewCandidateId: reviewIdByUnit.get(item.unitId) })
      continue
    }
    out.push(item)
  }
  return out
}

/** 待确认最旧优先（等最久的决定排最前），进行中次之，已终结最新优先。 */
export function sortTaskItems(items: TaskItem[]): TaskItem[] {
  return [...items].sort((a, b) => {
    if (a.section !== b.section) return SECTION_ORDER[a.section] - SECTION_ORDER[b.section]
    const left = createdAtOf(a)
    const right = createdAtOf(b)
    return a.section === 'needsAction' ? left.localeCompare(right) : right.localeCompare(left)
  })
}

export function createdAtOf(item: TaskItem): string {
  return item.candidate?.createdAt || item.run?.createdAt || ''
}

export function itemKind(item: TaskItem): string {
  return item.candidate?.kind || item.run?.kind || ''
}

export function itemKindTitle(item: TaskItem): string {
  const kind = itemKind(item)
  if (kind === 'plan') return planModeLabel(item.candidate?.mode || item.run?.spec?.mode)
  const refine = Boolean(
    item.candidate?.baseChapters?.length
    || item.candidate?.hasBaseChapters
    || item.run?.spec?.rewriteMode === 'refine',
  )
  if (kind === 'write' && refine) return '正文 · 原文优化'
  return runKindLabel(kind, item.run?.spec?.mode)
}

export function itemTarget(item: TaskItem): string {
  if (item.unitId) return item.unitId
  if (item.candidate?.targetPath) return item.candidate.targetPath
  const volume = item.candidate?.volumeId || item.run?.spec?.volumeId
  if (volume) return volume
  return '全书'
}

/** 确认该行是否需要先取候选详情（精简列表不带正文/计划）。 */
export function needsCandidateDetail(item: TaskItem): boolean {
  const c = item.candidate
  if (!c) return false
  return Boolean(c.hasChapters || c.hasBaseChapters || c.hasPlan || c.hasReplacement || c.hasOutlines || c.hasActual)
}
